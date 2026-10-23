"""Offline deployment checks; SSH and Docker boundaries never reach a server."""
import os
from pathlib import Path
import pty
import shlex
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
        self.bin = self.work / "bin"
        self.bin.mkdir()
        self.env = dict(os.environ, PATH=f"{self.bin}:{os.environ['PATH']}",
                        FAKE_STATE=str(self.work), DEPLOY_CONFIG=str(self.work / "absent"))
        for key in ("DEPLOY_HOST", "DEPLOY_PORT", "DEPLOY_USER", "SSH_PASSWORD",
                    "SSH_IDENTITY_FILE", "REMOTE_DIR", "PUBLIC_URL", "HTTP_PORT"):
            self.env.pop(key, None)
        self.stub("id", "#!/bin/sh\necho 0\n")
        self.stub("uname", "#!/bin/sh\necho Linux\n")
        self.stub("ssh", "#!/bin/sh\necho unexpected-ssh >&2\nexit 99\n")
        self.stub("docker", '''#!/usr/bin/env python3
import os, pathlib, sys
p = pathlib.Path(os.environ['FAKE_STATE'])
a = sys.argv[1:]
with (p / 'calls').open('a') as f:
    f.write(' '.join(a) + '\\n')
if a[:2] == ['volume', 'ls']:
    if (p / 'volume').exists(): print('goflylivechat_db_data')
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
        for config in ({}, {"DEPLOY_HOST": "-oProxyCommand=evil"},
                       {"DEPLOY_HOST": "example.com", "DEPLOY_PORT": "70000"},
                       {"DEPLOY_HOST": "example.com", "REMOTE_DIR": "/"},
                       {"DEPLOY_HOST": "example.com", "REMOTE_DIR": "///"},
                       {"DEPLOY_HOST": "example.com", "PUBLIC_URL": "http://example.com:99999"},
                       {"DEPLOY_HOST": "example.com", "PUBLIC_URL": "http://example.com;evil"}):
            with self.subTest(config=config):
                result = subprocess.run(["bash", str(ROOT / "scripts/deploy-linux.sh")],
                                        env=self.env | config, capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertNotIn("unexpected-ssh", result.stderr)

    def test_valid_host_reaches_ssh_without_rejecting_default_url(self):
        result = subprocess.run(["bash", str(ROOT / "scripts/deploy-linux.sh")],
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
                result = subprocess.run(["bash", str(ROOT / "scripts/deploy-linux.sh")],
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
  *tar*|*load*) cat >/dev/null ;;
esac
''')

    def test_first_run_prompts_and_saves_private_config_for_next_run(self):
        self.allow_deployment()
        key = self.work / "ssh 'key"
        key.touch()
        master, slave = pty.openpty()
        try:
            os.write(master, f"192.0.2.1\n\n\n{key}\n".encode())
            result = subprocess.run(["bash", str(ROOT / "scripts/deploy-linux.sh")],
                                    env=self.env, stdin=slave, capture_output=True, text=True,
                                    timeout=15)
        finally:
            os.close(master)
            os.close(slave)
        self.assertEqual(result.returncode, 0, result.stderr)
        config = Path(self.env["DEPLOY_CONFIG"])
        self.assertEqual(config.stat().st_mode & 0o777, 0o600)
        self.assertNotIn("SSH_PASSWORD=", config.read_text())
        original = config.read_text()
        result = subprocess.run(["bash", str(ROOT / "scripts/deploy-linux.sh")],
                                env=self.env, capture_output=True, text=True, timeout=15)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(config.read_text(), original)


if __name__ == "__main__":
    unittest.main()
