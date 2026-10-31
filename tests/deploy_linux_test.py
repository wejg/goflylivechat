"""Offline deployment checks; SSH and Docker boundaries never reach a server."""
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]


class DeployLinuxTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.work = Path(self.temp.name)
        self.remote = self.work / "remote"
        self.remote.mkdir()
        self.project = self.work / "project"
        (self.project / "scripts").mkdir(parents=True)
        for name in ("scripts/deploy-linux.sh", "scripts/install-linux.sh",
                     "docker-compose.yml", "compose.production.yml"):
            shutil.copyfile(ROOT / name, self.project / name)
        self.config = "\n".join(f"{key}={'a' * 64}" for key in (
            "GOFLY_DB_PASSWORD", "MARIADB_ROOT_PASSWORD",
            "GOFLY_SESSION_SECRET", "GOFLY_JWT_SECRET")) + "\nGOFLY_APP_PUSH_ENABLED=true\n"
        (self.project / ".env").write_text(self.config)
        self.bin = self.work / "bin"
        self.bin.mkdir()
        self.env = dict(os.environ, PATH=f"{self.bin}:{os.environ['PATH']}",
                        FAKE_STATE=str(self.work), DEPLOY_CONFIG=str(self.work / "absent"))
        for key in ("DEPLOY_HOST", "DEPLOY_PORT", "DEPLOY_USER", "SSH_PASSWORD",
                    "SSH_IDENTITY_FILE", "REMOTE_DIR", "PUBLIC_URL", "HTTP_PORT"):
            self.env.pop(key, None)
        self.env["REMOTE_DIR"] = str(self.remote)
        self.stub("id", "#!/bin/sh\necho 0\n")
        self.stub("uname", "#!/bin/sh\necho Linux\n")
        self.stub("sshpass", '#!/bin/sh\nshift\nexec "$@"\n')
        self.stub("ssh", "#!/bin/sh\necho unexpected-ssh >&2\nexit 99\n")
        self.stub("docker", '''#!/usr/bin/env python3
import os, pathlib, sys
p = pathlib.Path(os.environ['FAKE_STATE'])
a = sys.argv[1:]
with (p / 'calls').open('a') as f:
    f.write(' '.join(a) + '\\n')
if a[:2] == ['volume', 'ls']:
    if (p / 'volume').exists(): print('goflylivechat_db_data')
elif a[:2] == ['network', 'inspect']:
    sys.exit(0 if (p / 'network').exists() else 1)
elif a[:2] == ['network', 'create']:
    if os.environ.get('FAIL_NETWORK') == '1': sys.exit(1)
    (p / 'network').touch()
elif a[:2] == ['compose', 'version']:
    print('2.39.1')
elif 'compose' in a:
    if 'exec' in a:
        sql = sys.stdin.read()
        with (p / 'sql').open('a') as f: f.write(sql)
        if 'COUNT(*)' in sql: print('1' if (p / 'tables').exists() else '0')
    elif 'run' in a and 'install' in a:
        (p / 'lock').touch()
        (p / 'tables').touch()
    elif 'run' in a:
        sys.exit(0 if (p / 'lock').exists() else 42)
    elif 'up' in a and os.environ.get('FAIL_UP') == '1':
        sys.exit(1)
    elif 'config' in a and os.environ.get('FAIL_CONFIG') == '1':
        sys.exit(1)
''')

    def stub(self, name, source):
        path = self.bin / name
        path.write_text(source)
        path.chmod(0o755)

    def run_remote(self, phase="prepare", **env):
        return subprocess.run(["bash", str(ROOT / "scripts/install-linux.sh"), phase,
                               str(self.remote), "http://example.com:8081", "8081"],
                              env=self.env | env, capture_output=True, text=True)

    def test_creates_private_secrets_and_preserves_them_on_rerun(self):
        result = self.run_remote()
        self.assertEqual(result.returncode, 0, result.stderr)
        config = self.remote / ".env"
        original = config.read_text()
        self.assertEqual(config.stat().st_mode & 0o777, 0o600)
        values = dict(line.split("=", 1) for line in original.splitlines())
        for key in ("GOFLY_DB_PASSWORD", "MARIADB_ROOT_PASSWORD",
                    "GOFLY_SESSION_SECRET", "GOFLY_JWT_SECRET"):
            self.assertRegex(values[key], r"^[0-9a-f]{64}$")
        self.assertEqual(len({values[k] for k in values if k.endswith(("PASSWORD", "SECRET"))}), 4)
        result = self.run_remote()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(config.read_text(), original)

    def test_missing_config_with_existing_volume_is_rejected(self):
        (self.work / "volume").touch()
        result = self.run_remote()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.remote / ".env").exists())

    def test_shared_network_is_created_once_and_reused(self):
        for _ in range(2):
            result = self.run_remote()
            self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue((self.work / "network").exists())
        self.assertEqual((self.work / "calls").read_text().count("network create gofly-app\n"), 1)

    def test_shared_network_failure_stops_prepare(self):
        result = self.run_remote(FAIL_NETWORK="1")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.work / "network").exists())

    def test_uploaded_config_is_validated_and_replaces_existing_with_backup(self):
        self.assertEqual(self.run_remote().returncode, 0)
        original = (self.remote / ".env").read_text()
        (self.remote / ".env.upload").write_text(self.config)
        result = self.run_remote()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.remote / ".env").read_text(), self.config)
        self.assertEqual((self.remote / ".env.backup").read_text(), original)
        for name in (".env", ".env.backup"):
            self.assertEqual((self.remote / name).stat().st_mode & 0o777, 0o600)
        self.assertFalse((self.remote / ".env.upload").exists())

    def test_invalid_upload_leaves_existing_config_untouched(self):
        self.assertEqual(self.run_remote().returncode, 0)
        original = (self.remote / ".env").read_text()
        for config, env in (("GOFLY_DB_PASSWORD=change-me\n", {}),
                            (self.config, {"FAIL_CONFIG": "1"})):
            with self.subTest(env=env):
                (self.remote / ".env.upload").write_text(config)
                result = self.run_remote(**env)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual((self.remote / ".env").read_text(), original)
                self.assertFalse((self.remote / ".env.backup").exists())

    def test_existing_tables_without_install_lock_are_never_reimported(self):
        self.assertEqual(self.run_remote().returncode, 0)
        (self.work / "tables").touch()
        result = self.run_remote("deploy")
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.work / "lock").exists())
        self.assertNotIn(" install\n", (self.work / "calls").read_text())

    def test_fresh_install_and_rerun_keep_admin_password(self):
        self.assertEqual(self.run_remote().returncode, 0)
        result = self.run_remote("deploy")
        self.assertEqual(result.returncode, 0, result.stderr)
        credential = self.remote / "initial-admin-password.txt"
        password = credential.read_text()
        self.assertRegex(password.strip(), r"^[0-9a-f]{64}$")
        self.assertEqual(credential.stat().st_mode & 0o777, 0o600)
        sql_before = (self.work / "sql").read_text()
        result = self.run_remote("deploy")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(credential.read_text(), password)
        self.assertEqual((self.work / "sql").read_text().count("UPDATE"), sql_before.count("UPDATE"))

    def test_health_failure_is_not_reported_as_success(self):
        self.assertEqual(self.run_remote().returncode, 0)
        result = self.run_remote("deploy", FAIL_UP="1")
        self.assertNotEqual(result.returncode, 0)

    def test_invalid_ssh_input_fails_before_connecting(self):
        for config in ({"DEPLOY_HOST": "-oProxyCommand=evil"},
                       {"DEPLOY_HOST": "example.com", "DEPLOY_PORT": "70000"},
                       {"DEPLOY_HOST": "example.com", "REMOTE_DIR": "/"},
                       {"DEPLOY_HOST": "example.com", "REMOTE_DIR": "///"},
                       {"DEPLOY_HOST": "example.com", "PUBLIC_URL": "http://example.com:99999"},
                       {"DEPLOY_HOST": "example.com", "PUBLIC_URL": "http://example.com;evil"}):
            with self.subTest(config=config):
                result = subprocess.run(["bash", str(self.project / "scripts/deploy-linux.sh")],
                                        env=self.env | config, capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertNotIn("unexpected-ssh", result.stderr)

    def test_valid_host_reaches_ssh_without_rejecting_default_url(self):
        result = subprocess.run(["bash", str(self.project / "scripts/deploy-linux.sh")],
                                env=self.env | {"DEPLOY_HOST": "192.0.2.1"},
                                capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unexpected-ssh", result.stderr)

    def test_browser_origin_is_normalized_before_remote_prepare(self):
        self.allow_deployment()
        for config, expected in (({"HTTP_PORT": "80"}, "http://example.com"),
                                 ({"PUBLIC_URL": "https://EXAMPLE.com:443/"}, "https://example.com")):
            with self.subTest(config=config):
                calls = self.work / "ssh-calls"
                calls.unlink(missing_ok=True)
                result = subprocess.run(["bash", str(self.project / "scripts/deploy-linux.sh")],
                                        env=self.env | {"DEPLOY_HOST": "example.com"} | config,
                                        capture_output=True, text=True)
                self.assertEqual(result.returncode, 0, result.stderr)
                prepare = [shlex.split(line) for line in calls.read_text().splitlines()
                           if 'install-linux.sh prepare ' in line]
                self.assertEqual(len(prepare), 1)
                self.assertEqual(prepare[0][4], expected)

    def allow_deployment(self):
        self.stub("ssh", '''#!/usr/bin/env bash
command="${@: -1}"
printf '%s\\n' "$command" >> "$FAKE_STATE/ssh-calls"
case "$command" in
  *uname*) printf x86_64 ;;
  *'docker load'*) cat >/dev/null ;;
  *tar*|*cat*) bash -c "$command" ;;
esac
''')

    def test_local_env_is_uploaded_before_prepare(self):
        self.allow_deployment()
        result = subprocess.run(["bash", str(self.project / "scripts/deploy-linux.sh")],
                                env=self.env | {"REMOTE_DIR": str(self.remote)},
                                capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        uploaded = self.remote / ".env.upload"
        self.assertEqual(uploaded.read_text(), self.config)
        self.assertEqual(uploaded.stat().st_mode & 0o777, 0o600)
        calls = (self.work / "ssh-calls").read_text()
        self.assertLess(calls.index(".env.upload"), calls.index("install-linux.sh prepare"))

    def test_missing_local_env_fails_before_connecting(self):
        (self.project / ".env").unlink()
        result = subprocess.run(["bash", str(self.project / "scripts/deploy-linux.sh")],
                                env=self.env, capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn("unexpected-ssh", result.stderr)

    def test_saved_ssh_config_is_reused_without_changes(self):
        self.allow_deployment()
        key = self.work / "ssh 'key"
        key.touch()
        config = Path(self.env["DEPLOY_CONFIG"])
        config.write_text(f"DEPLOY_HOST=192.0.2.1\nSSH_IDENTITY_FILE={shlex.quote(str(key))}\n")
        config.chmod(0o600)
        original = config.read_text()
        result = subprocess.run(["bash", str(self.project / "scripts/deploy-linux.sh")],
                                env=self.env, capture_output=True, text=True, timeout=15)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(config.read_text(), original)


if __name__ == "__main__":
    unittest.main()
