#!/usr/bin/env bash
set -Eeuo pipefail
ARGO_LIB="$(cd -- "$(dirname -- "$(readlink -f -- "${BASH_SOURCE[0]}")")" && pwd)"
source "$ARGO_LIB/ui.sh"
source "$ARGO_LIB/services.sh"
ROOT="${1:-${SB_ROOT:-/etc/s-box}}"
argo_action() {
local choice=$1 arch port scheme domain token command post
if [[ "$choice" == 3 ]];then
    systemctl disable --now sb-argo 2>/dev/null || true
    /usr/local/bin/yg-vpngate config --root "$ROOT" argo
    ui_line "$UI_GREEN" ' Argo 已关闭。'
    return
fi
if [[ "$choice" == 2 ]];then
    ui_input domain '固定域名，已在 Cloudflare 指向本机 VMess 端口' || return "$UI_CANCEL"
    ui_secret token 'Tunnel token' || return "$UI_CANCEL"
    [[ "$token" =~ ^[A-Za-z0-9+/=_-]+$ ]] || { echo 'Token 格式错误' >&2;return 1; }
fi
case $(uname -m) in x86_64) arch=amd64;;aarch64|arm64) arch=arm64;;*) return 1;;esac
/usr/local/bin/yg-vpngate asset --arch "$arch" --output "$ROOT/cloudflared"
read -r port scheme < <(/usr/local/bin/yg-vpngate config --root "$ROOT" vmess-info)
if [[ "$choice" == 2 ]];then
    printf 'TUNNEL_TOKEN=%s\n' "$token" > "$ROOT/argo.env";chmod 0600 "$ROOT/argo.env";unset token
    /usr/local/bin/yg-vpngate config --root "$ROOT" argo "$domain"
    command="$ROOT/cloudflared tunnel --no-autoupdate run"
    post=''
else
    printf '\n' > "$ROOT/argo.env";chmod 0600 "$ROOT/argo.env"
    command="$ROOT/cloudflared tunnel --no-autoupdate --no-tls-verify --url $scheme://127.0.0.1:$port --logfile $ROOT/argo.log"
    post="ExecStartPost=/usr/local/bin/yg-vpngate config --root $ROOT argo-watch"
fi
write_service_unit sb-argo 'sing-box-vpn Argo entry' "$ROOT" sing-box.service <<EOF
EnvironmentFile=$ROOT/argo.env
ExecStartPre=/usr/bin/truncate -s 0 $ROOT/argo.log
ExecStart=$command
$post
EOF
systemctl daemon-reload;systemctl enable sb-argo >/dev/null;systemctl restart sb-argo
ui_line "$UI_GREEN" ' Argo 已启动，菜单 9 刷新节点。'
ui_hint '临时域名会随服务重启自动更新。'
}
argo_menu() {
    ui_submenu 'VMess · Argo 隧道管理' argo_action \
        '启用临时 Argo 隧道' '启用已配置的固定隧道' '关闭 Argo 隧道'
}
if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then return 0; fi
argo_menu
