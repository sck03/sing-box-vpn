#!/usr/bin/env bash
set -Eeuo pipefail
ROOT="$1"
echo '1. 临时 Argo 隧道  2. 已配置的固定隧道  3. 关闭 Argo'
read -r -p '选择：' choice
if [[ "$choice" == 3 ]];then
    systemctl disable --now sb-argo 2>/dev/null || true
    /usr/local/bin/yg-vpngate config --root "$ROOT" argo
    exit
fi
case $(uname -m) in x86_64) arch=amd64;;aarch64|arm64) arch=arm64;;*) exit 1;;esac
/usr/local/bin/yg-vpngate asset --arch "$arch" --output "$ROOT/cloudflared"
read -r port scheme < <(/usr/local/bin/yg-vpngate config --root "$ROOT" vmess-info)
if [[ "$choice" == 2 ]];then
    read -r -p '固定域名（已在 Cloudflare 指向本机 VMess 端口）：' domain
    read -rs -p 'Tunnel token：' token;echo
    [[ "$token" =~ ^[A-Za-z0-9+/=_-]+$ ]] || { echo 'Token 格式错误' >&2;exit 1; }
    printf 'TUNNEL_TOKEN=%s\n' "$token" > "$ROOT/argo.env";chmod 0600 "$ROOT/argo.env";unset token
    /usr/local/bin/yg-vpngate config --root "$ROOT" argo "$domain"
    command="$ROOT/cloudflared tunnel --no-autoupdate run"
    post=''
else
    printf '\n' > "$ROOT/argo.env";chmod 0600 "$ROOT/argo.env"
    command="$ROOT/cloudflared tunnel --no-autoupdate --no-tls-verify --url $scheme://127.0.0.1:$port --logfile $ROOT/argo.log"
    post="ExecStartPost=/usr/local/bin/yg-vpngate config --root $ROOT argo-watch"
fi
cat > /etc/systemd/system/sb-argo.service <<EOF
[Unit]
Description=sing-box-vpn Argo entry
After=network-online.target sing-box.service
[Service]
EnvironmentFile=$ROOT/argo.env
ExecStartPre=/usr/bin/truncate -s 0 $ROOT/argo.log
ExecStart=$command
$post
Restart=on-failure
RestartSec=5
TimeoutStartSec=120
[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload;systemctl enable sb-argo >/dev/null;systemctl restart sb-argo
echo 'Argo 已启动，菜单 9 刷新节点；临时域名会随服务重启自动更新。'
