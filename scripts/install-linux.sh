#!/usr/bin/env bash
# 由 deploy-linux.sh 上传并调用；不是本机安装入口。
set -Eeuo pipefail
umask 077

fail() { printf '错误：%s\n' "$*" >&2; exit 1; }
[[ "$#" == 4 ]] || fail "用法：$0 prepare|deploy <目录> <访问源地址> <HTTP端口>"
PHASE="$1"
REMOTE_DIR="$2"
PUBLIC_URL="$3"
HTTP_PORT="$4"
[[ "$PHASE" == prepare || "$PHASE" == deploy ]] || fail "不支持的安装阶段"
[[ "$(id -u)" == 0 && "$(uname -s)" == Linux ]] || fail "请使用 Linux root 账号"
cd "$REMOTE_DIR"

compose() { docker compose -p goflylivechat --env-file .env -f docker-compose.yml -f compose.production.yml "$@"; }
random_secret() { od -An -N32 -tx1 /dev/urandom | tr -d ' \n'; }
db_sql() {
    compose exec -T db sh -c 'MYSQL_PWD="$MARIADB_PASSWORD" exec mariadb -u "$MARIADB_USER" "$MARIADB_DATABASE" --batch --skip-column-names'
}

if [[ "$PHASE" == prepare ]]; then
    if ! command -v docker >/dev/null 2>&1; then
        # shortcut: 自动安装只支持 Debian/Ubuntu，其他发行版需先自行安装 Docker 和 Compose。
        . /etc/os-release
        case "$ID" in ubuntu|debian) ;; *) fail "自动安装 Docker 仅支持 Debian/Ubuntu" ;; esac
        command -v cloud-init >/dev/null 2>&1 && cloud-init status --wait >/dev/null || true
        export DEBIAN_FRONTEND=noninteractive
        apt-get update
        apt-get install -y ca-certificates curl
        install -m 0755 -d /etc/apt/keyrings
        curl --fail --show-error --silent --location --retry 3 "https://download.docker.com/linux/$ID/gpg" -o /etc/apt/keyrings/docker.asc
        chmod a+r /etc/apt/keyrings/docker.asc
        cat > /etc/apt/sources.list.d/docker.sources <<EOF
Types: deb
URIs: https://download.docker.com/linux/$ID
Suites: ${UBUNTU_CODENAME:-${VERSION_CODENAME:?缺少系统版本代号}}
Components: stable
Architectures: $(dpkg --print-architecture)
Signed-By: /etc/apt/keyrings/docker.asc
EOF
        apt-get update
        apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
        systemctl enable --now docker
    fi
    docker info >/dev/null 2>&1 || fail "Docker 已安装但未运行，请先检查 Docker 服务"
    compose_version="$(docker compose version --short)" || fail "需要安装 Docker Compose 插件 >= 2.24.4"
    compose_version="${compose_version#v}"
    [[ "$(printf '%s\n' 2.24.4 "$compose_version" | sort -V | head -n 1)" == 2.24.4 ]] || fail "Docker Compose 版本过低，需要 >= 2.24.4"
    if [[ ! -f .env ]]; then
        existing_volumes="$(docker volume ls -q --filter label=com.docker.compose.project=goflylivechat)"
        [[ -z "$existing_volumes" ]] || fail "已有 GoFly 数据卷但缺少 .env，请恢复原配置，不能生成新密码"
        {
            printf 'GOFLY_DB_PASSWORD=%s\n' "$(random_secret)"
            printf 'MARIADB_ROOT_PASSWORD=%s\n' "$(random_secret)"
            printf 'GOFLY_SESSION_SECRET=%s\n' "$(random_secret)"
            printf 'GOFLY_JWT_SECRET=%s\n' "$(random_secret)"
            printf 'GOFLY_ALLOWED_ORIGINS=%s\nGOFLY_HTTP_PORT=%s\n' "$PUBLIC_URL" "$HTTP_PORT"
        } > .env.new
        mv .env.new .env
    fi
    chmod 600 .env
    for key in GOFLY_DB_PASSWORD MARIADB_ROOT_PASSWORD GOFLY_SESSION_SECRET GOFLY_JWT_SECRET; do
        [[ "$(grep -c "^$key=" .env || true)" == 1 ]] && grep -Eq "^$key=.{32,}$" .env ||
            fail "远端 .env 的 $key 必须唯一且至少 32 字符；脚本不会覆盖已有配置"
        if grep -Eq "^$key=.*(replace-with|change-me|change-root-me)" .env; then
            fail "远端 .env 的 $key 仍是示例值，请先更换"
        fi
    done
    compose config --quiet
    printf 'Docker 和生产配置已就绪，已有 .env 保持不变。\n'
    exit 0
fi

[[ -f .env ]] || fail "缺少 .env，请先运行 prepare 阶段"
trap 'printf "启动失败，保留配置和数据。容器状态：\n" >&2; compose ps >&2 || true' ERR
printf '启动数据库并等待就绪...\n'
compose up -d --no-build --wait --wait-timeout 180 db
table_count="$(printf 'SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE();\n' | db_sql)"
[[ "$table_count" =~ ^[0-9]+$ ]] || fail "无法读取数据库状态"
lock_status=0
compose run --rm --no-deps --entrypoint sh app -c \
    'if [ -f "$GOFLY_INSTALL_LOCK" ]; then exit 0; else exit 42; fi' || lock_status=$?
case "$lock_status" in
    0) (( table_count > 0 )) || fail "已有安装锁但数据库为空，请恢复数据库备份" ;;
    42)
        (( table_count == 0 )) || fail "数据库已有表但安装锁缺失；拒绝重导入 import.sql，请恢复原安装锁"
        [[ -f initial-admin-password.pending ]] || random_secret > initial-admin-password.pending
        printf '首次初始化数据库...\n'
        compose run --rm --no-deps --entrypoint /app/gofly app install
        ;;
    *) fail "检查安装锁失败（退出码 $lock_status）" ;;
esac

if [[ -f initial-admin-password.pending ]]; then
    admin_password="$(cat initial-admin-password.pending)"
    [[ "$admin_password" =~ ^[0-9a-f]{64}$ ]] || fail "初始客服密码文件格式异常"
    # 仅替换导入 SQL 中的默认摘要，用户已修改的密码不受影响；首次登录会升级为 bcrypt。
    printf "UPDATE \`user\` SET password=MD5('%s') WHERE name='agent' AND password='b33aed8f3134996703dc39f9a7c95783';\n" "$admin_password" | db_sql
    mv initial-admin-password.pending initial-admin-password.txt
fi

printf '启动客服服务并等待健康检查...\n'
compose up -d --no-build --pull never --wait --wait-timeout 180
compose ps
printf '\n部署完成。HTTP 端口：'
compose port app 8081
printf '客服后台：%s/login（访问源和端口以远端 .env 为准）\n' "$PUBLIC_URL"
if [[ -f initial-admin-password.txt ]]; then
    printf '初始客服账号：agent\n初始密码：%s\n登录后请修改密码；后续部署不会重置密码。\n' "$(cat initial-admin-password.txt)"
fi
printf '配置和数据卷属于 Compose 项目 goflylivechat，部署目录：%s\n' "$REMOTE_DIR"
