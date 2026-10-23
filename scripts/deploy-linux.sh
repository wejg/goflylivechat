#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DEPLOY_CONFIG="${DEPLOY_CONFIG:-$SCRIPT_DIR/deploy.env}"
if [[ -f "$DEPLOY_CONFIG" ]]; then
    # 配置是本机可信的 Bash 文件，不上传到服务器或镜像。
    source "$DEPLOY_CONFIG"
fi
DEPLOY_HOST="${DEPLOY_HOST:-8.159.128.130}"
DEPLOY_PORT="${DEPLOY_PORT:-22}"
DEPLOY_USER="${DEPLOY_USER:-root}"
SSH_PASSWORD="${SSH_PASSWORD:-weijg2020@W}"
SSH_IDENTITY_FILE="${SSH_IDENTITY_FILE:-}"
REMOTE_DIR="${REMOTE_DIR:-/opt/goflylivechat}"
HTTP_PORT="${HTTP_PORT:-8081}"
PUBLIC_URL="${PUBLIC_URL:-}"

fail() { printf '错误：%s\n' "$*" >&2; exit 1; }
require_command() { command -v "$1" >/dev/null 2>&1 || fail "本机缺少命令：$1"; }
valid_port() { [[ "$1" =~ ^[1-9][0-9]{0,4}$ ]] && (( $1 <= 65535 )); }

[[ "$#" == 0 ]] || fail "用法：$0（首次运行按提示填写 SSH 信息）"
SAVE_CONFIG=0
if [[ -z "$DEPLOY_HOST" && ! -f "$DEPLOY_CONFIG" && -t 0 ]]; then
    printf '首次部署，请填写服务器 SSH 信息（回车使用默认值）。\n'
    read -r -p '服务器 IP 或 SSH 别名：' DEPLOY_HOST || fail "未完成服务器信息输入"
    read -r -p "SSH 端口 [$DEPLOY_PORT]：" answer || fail "未完成端口输入"
    DEPLOY_PORT="${answer:-$DEPLOY_PORT}"
    read -r -p "SSH 用户 [$DEPLOY_USER]：" answer || fail "未完成用户输入"
    DEPLOY_USER="${answer:-$DEPLOY_USER}"
    read -r -p 'SSH 私钥路径 [留空使用系统 SSH 登录]：' SSH_IDENTITY_FILE || fail "未完成私钥输入"
    if [[ "$SSH_IDENTITY_FILE" == '~/'* ]]; then SSH_IDENTITY_FILE="$HOME/${SSH_IDENTITY_FILE:2}"; fi
    SAVE_CONFIG=1
fi
[[ "$DEPLOY_HOST" =~ ^[A-Za-z0-9][A-Za-z0-9._:-]*$ ]] || fail "请直接在终端运行脚本并按提示填写服务器信息，或通过 DEPLOY_HOST 环境变量配置"
valid_port "$DEPLOY_PORT" || fail "DEPLOY_PORT 必须在 1–65535 之间"
valid_port "$HTTP_PORT" || fail "HTTP_PORT 必须在 1–65535 之间"
[[ "$DEPLOY_USER" =~ ^[a-z_][a-z0-9_-]*$ ]] || fail "DEPLOY_USER 格式不正确"
[[ "$REMOTE_DIR" =~ ^/[A-Za-z0-9_-][A-Za-z0-9._/-]*$ && "$REMOTE_DIR" != */../* && "$REMOTE_DIR" != */.. && "$REMOTE_DIR" != */./* && "$REMOTE_DIR" != */. ]] || fail "REMOTE_DIR 必须是安全的绝对路径，不能是根目录"
if [[ -z "$PUBLIC_URL" ]]; then
    url_host="$DEPLOY_HOST"
    [[ "$url_host" != *:* ]] || url_host="[$url_host]"
    PUBLIC_URL="http://$url_host:$HTTP_PORT"
fi
PUBLIC_URL="${PUBLIC_URL%/}"
PUBLIC_URL="$(printf '%s' "$PUBLIC_URL" | tr '[:upper:]' '[:lower:]')"
public_url_pattern='^https?://(\[[0-9A-Fa-f:]+\]|[A-Za-z0-9][A-Za-z0-9._-]*)(:[1-9][0-9]{0,4})?$'
[[ "$PUBLIC_URL" =~ $public_url_pattern ]] || fail "PUBLIC_URL 只能包含协议、主机和可选端口，例如 http://example.com:8081"
public_port="${BASH_REMATCH[2]#:}"
[[ -z "$public_port" ]] || valid_port "$public_port" || fail "PUBLIC_URL 中的端口必须在 1–65535 之间"
# 浏览器 Origin 会省略协议默认端口，必须与服务端的精确来源校验一致。
if [[ "$PUBLIC_URL" == http://* && "$public_port" == 80 || "$PUBLIC_URL" == https://* && "$public_port" == 443 ]]; then
    PUBLIC_URL="${PUBLIC_URL%:*}"
fi
for command_name in ssh docker tar gzip; do require_command "$command_name"; done
docker info >/dev/null 2>&1 || fail "请先启动本机 Docker（Mac 可使用 Docker Desktop）"
docker buildx version >/dev/null 2>&1 || fail "本机需要 Docker Buildx"

SSH_OPTIONS=(-p "$DEPLOY_PORT" -o StrictHostKeyChecking=accept-new
    -o ConnectTimeout=15 -o ServerAliveInterval=15 -o ServerAliveCountMax=4)
if [[ -n "$SSH_IDENTITY_FILE" ]]; then
    [[ -f "$SSH_IDENTITY_FILE" ]] || fail "SSH 私钥不存在：$SSH_IDENTITY_FILE"
    SSH_OPTIONS+=(-i "$SSH_IDENTITY_FILE" -o IdentitiesOnly=yes)
fi
if [[ -n "$SSH_PASSWORD" ]]; then
    require_command sshpass
    SSH_OPTIONS+=(-o PreferredAuthentications=password -o PubkeyAuthentication=no)
fi
SSH_CONTROL_DIR="$(mktemp -d /tmp/gofly-ssh.XXXXXX)"
SSH_OPTIONS+=(-o ControlMaster=auto -o ControlPersist=300 -o "ControlPath=$SSH_CONTROL_DIR/socket")
cleanup() {
    ssh "${SSH_OPTIONS[@]}" -O exit "$DEPLOY_USER@$DEPLOY_HOST" >/dev/null 2>&1 || true
    rm -rf -- "$SSH_CONTROL_DIR"
}
trap cleanup EXIT
trap 'printf "部署失败（第 %s 行），未删除远端数据；修复错误后可重新执行。\n" "$LINENO" >&2' ERR
ssh_run() {
    if [[ -n "$SSH_PASSWORD" ]]; then
        SSHPASS="$SSH_PASSWORD" sshpass -e ssh "${SSH_OPTIONS[@]}" "$DEPLOY_USER@$DEPLOY_HOST" "$@"
    else
        ssh "${SSH_OPTIONS[@]}" "$DEPLOY_USER@$DEPLOY_HOST" "$@"
    fi
}
remote_install() {
    local remote_command
    printf -v remote_command 'bash %q %q %q %q %q' "$REMOTE_DIR/scripts/install-linux.sh" "$1" "$REMOTE_DIR" "$PUBLIC_URL" "$HTTP_PORT"
    ssh_run "$remote_command"
}

printf '检查 SSH 和服务器架构...\n'
remote_arch="$(ssh_run 'test "$(uname -s)" = Linux && test "$(id -u)" = 0 && uname -m')" || fail "需要可登录的 Linux root SSH 账号"
case "$remote_arch" in
    x86_64|amd64) platform=linux/amd64 ;;
    aarch64|arm64) platform=linux/arm64 ;;
    *) fail "不支持服务器架构：$remote_arch" ;;
esac
if [[ "$SAVE_CONFIG" == 1 ]]; then
    (
        umask 077
        set -o noclobber
        for config_key in DEPLOY_HOST DEPLOY_PORT DEPLOY_USER SSH_IDENTITY_FILE REMOTE_DIR HTTP_PORT PUBLIC_URL; do
            printf '%s=%q\n' "$config_key" "${!config_key}"
        done > "$DEPLOY_CONFIG"
    )
    printf 'SSH 配置已自动保存到 %s（权限 600，不保存登录密码）。\n' "$DEPLOY_CONFIG"
fi

printf '上传部署文件...\n'
COPYFILE_DISABLE=1 tar --exclude='._*' --exclude='.DS_Store' -C "$PROJECT_ROOT" -czf - \
    docker-compose.yml compose.production.yml scripts/install-linux.sh |
    ssh_run "umask 077; mkdir -p '$REMOTE_DIR' && tar -xzf - -C '$REMOTE_DIR'"
remote_install prepare

printf '本机构建 %s 镜像...\n' "$platform"
docker buildx build --platform "$platform" --load --tag goflylivechat:deploy "$PROJECT_ROOT"
printf '上传并载入镜像...\n'
docker save goflylivechat:deploy | gzip | ssh_run 'bash -o pipefail -c "gzip -d | docker load"'
remote_install deploy
