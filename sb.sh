#!/usr/bin/env bash
# GPL-3.0. Modernized management derived from sing-box-yg; see NOTICE.md.
set -Eeuo pipefail
export PYTHONUTF8=1
SCRIPT_ROOT="$(cd -- "$(dirname -- "$(readlink -f -- "${BASH_SOURCE[0]}")")" && pwd)"
APP_HOME="${SB_HOME:-/opt/sing-box-vpn}"
SB_ROOT="${SB_ROOT:-/etc/s-box}"
CORE="$SB_ROOT/sing-box"
LIB="$SCRIPT_ROOT/lib"
cfg() { python3 "$LIB/config.py" --root "$SB_ROOT" --core "$CORE" "$@"; }
pause() { read -r -p '按回车继续…' _ || true; }
require_root() { [[ $(id -u) == 0 ]] || { echo '需要 root 权限' >&2; return 1; }; }
require_system() {
    require_root
    [[ $(uname -s) == Linux ]] || { echo '服务安装要求 Linux' >&2; return 1; }
    source /etc/os-release
    [[ "$ID" == debian || "$ID" == ubuntu ]] || { echo '当前版本支持 Ubuntu/Debian + systemd' >&2; return 1; }
    command -v systemctl >/dev/null || { echo '需要 systemd' >&2; return 1; }
}
firewall() { python3 "$LIB/firewall.py" --root "$SB_ROOT" "$@"; }
reload_main() { cfg export; firewall; systemctl restart sing-box; }
arch_name() { case $(uname -m) in x86_64) echo amd64;; aarch64|arm64) echo arm64;; *) echo '仅支持 amd64/arm64' >&2; return 1;; esac; }
install_main() {
    require_system
    local install_arch
    install_arch=$(arch_name)
    if [[ -d "$APP_HOME" && ! -f "$APP_HOME/.managed" ]]; then echo "$APP_HOME 已存在且无项目标记，停止安装" >&2; return 1; fi
    if [[ -d "$SB_ROOT" && ! -f "$SB_ROOT/sb.json" && ! -f "$SB_ROOT/app.json" ]] && [[ -n $(find "$SB_ROOT" -mindepth 1 -maxdepth 1 -print -quit) ]]; then echo '配置目录已有非本项目文件，停止安装' >&2;return 1;fi
    if [[ ! -f "$SCRIPT_ROOT/.bin/linux-$install_arch/yg-vpngate" && ! -f "$APP_HOME/.bin/linux-$install_arch/yg-vpngate" ]];then
        echo '请使用已验证的正式安装包；源码开发请先运行 scripts/build_release.py' >&2;return 1
    fi
    apt-get update -qq
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq ca-certificates curl openssl python3 iproute2 iptables procps qrencode tar
    if [[ -d "$APP_HOME" && ! -f "$APP_HOME/.managed" ]]; then echo "$APP_HOME 已存在且无项目标记，停止安装" >&2; return 1; fi
    install -d -m 0755 "$APP_HOME" "$SB_ROOT"
    if [[ "$SCRIPT_ROOT" != "$APP_HOME" ]]; then
        cp -a "$SCRIPT_ROOT/lib" "$SCRIPT_ROOT/addons" "$SCRIPT_ROOT/versions.json" "$SCRIPT_ROOT/LICENSE" "$SCRIPT_ROOT/NOTICE.md" "$SCRIPT_ROOT/sb.sh" "$SCRIPT_ROOT/install.sh" "$APP_HOME/"
        if [[ -d "$SCRIPT_ROOT/.bin" ]]; then cp -a "$SCRIPT_ROOT/.bin" "$APP_HOME/"; fi
    fi
    printf '%s\n' 'sck03/sing-box-vpn' > "$APP_HOME/.managed"
    LIB="$APP_HOME/lib"
    local arch candidate
    arch=$(arch_name)
    candidate="$SB_ROOT/sing-box.candidate"
    python3 "$LIB/core.py" --root "$SB_ROOT" prepare --output "$candidate"
    python3 "$LIB/config.py" --root "$SB_ROOT" --core "$candidate" init
    python3 "$LIB/core.py" --root "$SB_ROOT" activate "$candidate" --no-restart
    rm -f -- "$candidate"
    if [[ -f "$APP_HOME/.bin/linux-$arch/yg-vpngate" ]]; then
        install -m 0755 "$APP_HOME/.bin/linux-$arch/yg-vpngate" /usr/local/bin/yg-vpngate.new
        mv -f /usr/local/bin/yg-vpngate.new /usr/local/bin/yg-vpngate
    else
        echo '发行包缺少静态网关二进制，请使用 install.sh 安装正式发行包' >&2; return 1
    fi
    chmod 0600 "$SB_ROOT"/*.json "$SB_ROOT"/*.key 2>/dev/null || true
    cat > /etc/systemd/system/sing-box.service <<EOF
[Unit]
Description=sing-box-vpn ordinary five-protocol service
After=network-online.target
Wants=network-online.target
[Service]
ExecStart=$CORE run -c $SB_ROOT/sb.json
ExecReload=/bin/kill -HUP \$MAINPID
WorkingDirectory=$SB_ROOT
Restart=on-failure
RestartSec=5
LimitNOFILE=65536
[Install]
WantedBy=multi-user.target
EOF
    ln -sfn "$APP_HOME/sb.sh" /usr/bin/sb
    firewall
    systemctl daemon-reload
    systemctl enable --now sing-box >/dev/null
    systemctl restart sing-box
    if [[ -f "$SB_ROOT/vpngate/gateway.json" ]]; then systemctl restart yg-vpngate sing-box-vpngate; fi
    # Old templates are removed only after successful generation and migration.
    rm -f -- "$SB_ROOT/sb10.json" "$SB_ROOT/sb11.json"
    echo '五协议普通节点已安装，输入 sb，菜单 17 添加 VPNGate。'
    cfg show
}
subscription_menu() {
    echo '1. 启用/重置本地订阅  2. 停止订阅  3. 查看订阅地址'
    read -r -p '选择：' choice
    case "$choice" in
    1) local port; read -r -p '订阅端口（回车随机）：' port
       python3 - "$SB_ROOT" "$port" <<'PY'
import sys,secrets,socket,json
from pathlib import Path
root=Path(sys.argv[1]);port=int(sys.argv[2]) if sys.argv[2] else 0
s=socket.socket();s.bind(('0.0.0.0',port));port=s.getsockname()[1];s.close()
token=secrets.token_urlsafe(32)
p=root/'subscription.json';p.write_text(json.dumps({'enabled':True,'port':port,'token':token,'listen':'0.0.0.0'}));p.chmod(0o600)
PY
       cat > /etc/systemd/system/sb-subscription.service <<EOF
[Unit]
Description=Token protected sing-box-vpn subscriptions
After=network-online.target
[Service]
ExecStart=/usr/bin/python3 $APP_HOME/lib/subscription.py --root $SB_ROOT
WorkingDirectory=$SB_ROOT
Restart=on-failure
ProtectSystem=strict
ReadOnlyPaths=$SB_ROOT
NoNewPrivileges=true
[Install]
WantedBy=multi-user.target
EOF
       cfg export; firewall; systemctl daemon-reload; systemctl enable --now sb-subscription >/dev/null; systemctl restart sb-subscription; subscription_urls;;
    2) systemctl disable --now sb-subscription 2>/dev/null || true
       python3 - "$SB_ROOT/subscription.json" <<'PY'
import json,sys,pathlib
p=pathlib.Path(sys.argv[1])
if p.exists():
 s=json.loads(p.read_text());s['enabled']=False;p.write_text(json.dumps(s));p.chmod(0o600)
PY
       firewall;;
    3) subscription_urls;;
    esac
}
subscription_urls() {
    python3 - "$SB_ROOT" <<'PY'
import json,sys,pathlib
r=pathlib.Path(sys.argv[1]);p=r/'subscription.json'
if not p.exists():print('未配置本地订阅');raise SystemExit
s=json.loads(p.read_text());a=json.loads((r/'app.json').read_text())['address']
for name in ('sbox.json','clmi.yaml','jhsub.txt'):print(f'http://{a}:{s["port"]}/{s["token"]}/{name}')
PY
}
protocol_pick() { echo 'vless / vmess / hysteria2 / tuic / anytls'; read -r -p '协议：' PROTOCOL; }
core_menu() {
    echo '1. 当前/最新稳定版  2. 更新最新稳定版  3. 指定稳定版本  4. 回滚上一核心  0. 返回'
    read -r -p '选择：' choice
    case "$choice" in
    1) python3 "$LIB/core.py" --root "$SB_ROOT" check;;
    2) python3 "$LIB/core.py" --root "$SB_ROOT" update;;
    3) read -r -p '稳定版版本号（1.14.0 或更新）：' version;python3 "$LIB/core.py" --root "$SB_ROOT" update --version "$version";;
    4) python3 "$LIB/core.py" --root "$SB_ROOT" rollback;;
    esac
}
settings_menu() {
    echo '1. Reality SNI  2. 重置凭据  3. Argo 隧道  4. VMess WS 路径'
    echo '5. Telegram 配置  6. WARP endpoint  7. GitLab 私有订阅  8. 本地订阅  9. VMess CDN 地址'
    read -r -p '选择：' choice
    case "$choice" in
    1) read -r -p 'Reality 域名：' value; cfg sni "$value"; reload_main;;
    2) protocol_pick; cfg reset "$PROTOCOL"; reload_main;;
    3) "$APP_HOME/lib/argo.sh" "$SB_ROOT" "$APP_HOME";;
    4) read -r -p 'WS 路径：' value; cfg ws "$value"; reload_main;;
    5) python3 "$LIB/remote.py" --root "$SB_ROOT" telegram-config;;
    6) read -r -p '新版 WireGuard endpoint JSON 文件：' value; cfg warp "$value"; reload_main;;
    7) python3 "$LIB/remote.py" --root "$SB_ROOT" gitlab-config; cfg export; python3 "$LIB/remote.py" --root "$SB_ROOT" gitlab-push;;
    8) subscription_menu;;
    9) read -r -p 'CDN 地址（空值清除）：' value; cfg cdn "$value"; cfg export;;
    esac
}
port_menu() {
    echo '1. 更改主端口  2. 普通 Hy2 端口跳跃  3. 清除端口跳跃';read -r -p '选择：' choice
    case "$choice" in
    1) protocol_pick;read -r -p '新端口：' value;cfg port "$PROTOCOL" "$value";reload_main;;
    2) read -r -p '端口/范围，空格分隔，例如 31000:31100 31200：' value;read -r -a ports <<< "$value";cfg hopping "${ports[@]}";reload_main;;
    3) cfg hopping;reload_main;;
    esac
}
route_menu() { echo 'socks / warp / direct4 / direct6';read -r -p '通道：' channel;read -r -p '后缀域名，空格分隔；回车清空：' value;read -r -a domains <<< "$value";cfg route "$channel" "${domains[@]}";reload_main; }
certificate_menu() {
    echo '1. 导入 PEM 证书  2. Certbot 申请证书';read -r -p '选择：' choice
    case "$choice" in
    1) protocol_pick;read -r -p '证书绝对路径：' cert;read -r -p '私钥绝对路径：' key;read -r -p '证书域名：' domain;cfg tls "$PROTOCOL" "$cert" "$key" "$domain";reload_main;;
    2) read -r -p '已解析到此 VPS 的域名：' domain;read -r -p '邮箱：' email
       [[ "$domain" =~ ^[A-Za-z0-9.-]+$ && "$email" == *@* ]] || return 1
       apt-get install -y -qq certbot
       certbot certonly --standalone --non-interactive --agree-tos --email "$email" -d "$domain"
       cfg tls all "/etc/letsencrypt/live/$domain/fullchain.pem" "/etc/letsencrypt/live/$domain/privkey.pem" "$domain";reload_main
       install -d /etc/letsencrypt/renewal-hooks/deploy
       printf '#!/bin/sh\nsystemctl restart sing-box\nsb export\n' > /etc/letsencrypt/renewal-hooks/deploy/sing-box-vpn.sh
       chmod 0755 /etc/letsencrypt/renewal-hooks/deploy/sing-box-vpn.sh;;
    esac
}
share_menu() {
    echo '1. URI/二维码/聚合节点  2. 客户端配置与订阅  3. 推送到已配置 Telegram';read -r -p '选择：' choice
    cfg export
    case "$choice" in
    1) cfg links; if command -v qrencode >/dev/null;then while IFS= read -r link;do qrencode -t ANSIUTF8 "$link";done < "$SB_ROOT/jhsub.txt";fi;;
    2) echo "Sing-box: $SB_ROOT/sbox.json";echo "Mihomo: $SB_ROOT/clmi.yaml";subscription_urls;;
    3) python3 "$LIB/remote.py" --root "$SB_ROOT" telegram-push;;
    esac
}
uninstall_main() {
    echo '将停止并删除本项目服务与节点配置。';read -r -p '输入 DELETE 确认卸载：' answer;[[ "$answer" == DELETE ]] || return 0
    "$APP_HOME/addons/vpngate/vpngate.sh" uninstall || return 1
    systemctl disable --now sing-box sb-subscription sb-argo 2>/dev/null || true
    firewall --remove
    rm -f /etc/systemd/system/sing-box.service /etc/systemd/system/sb-subscription.service /etc/systemd/system/sb-argo.service /usr/bin/sb /usr/local/bin/yg-vpngate
    [[ "$SB_ROOT" == /etc/s-box && -f "$SB_ROOT/app.json" ]] || { echo '配置目录不符合安全清理条件' >&2;return 1; }
    rm -rf -- "$SB_ROOT"
    [[ "$APP_HOME" == /opt/sing-box-vpn && -f "$APP_HOME/.managed" ]] && rm -rf -- "$APP_HOME"
    systemctl daemon-reload
}
menu() {
    require_system
    while true;do
        echo;echo 'sing-box-vpn · 稳定核心可在线更新'
        echo ' 1. 安装/迁移五协议普通节点   2. 卸载'
        echo ' 3. 变更配置                4. 端口/Hy2 跳跃'
        echo ' 5. 域名分流                6. 启停服务'
        echo ' 7. 更新项目发行版          8. 更新/指定/回滚稳定核心'
        echo ' 9. 刷新并查看节点         10. 运行日志'
        echo '11. BBR + FQ              12. 管理证书'
        echo '13. 查看 WARP 分流配置     14. 配置已有本机 SOCKS5'
        echo '15. 更换入口地址           16. 使用说明'
        echo '17. VPNGate 临时家宽候选    0. 退出'
        if [[ -f "$SB_ROOT/app.json" ]];then cfg show;fi
        read -r -p '选择：' choice || return 0
        case "$choice" in
        0) return;;1) install_main;;2) uninstall_main;return;;3) settings_menu;;4) port_menu;;5) route_menu;;
        6) read -r -p 'start / stop / restart：' action;[[ "$action" =~ ^(start|stop|restart)$ ]] && systemctl "$action" sing-box;;
        7) curl -fLsS https://raw.githubusercontent.com/sck03/sing-box-vpn/main/install.sh -o /tmp/sb-vpn-update.sh;bash /tmp/sb-vpn-update.sh;rm -f /tmp/sb-vpn-update.sh;;
        8) core_menu;;9) share_menu;;10) journalctl -u sing-box -u yg-vpngate -u sing-box-vpngate -n 100 --no-pager;;
        11) modprobe tcp_bbr;printf 'net.core.default_qdisc=fq\nnet.ipv4.tcp_congestion_control=bbr\n' > /etc/sysctl.d/99-sing-box-vpn-bbr.conf;sysctl -p /etc/sysctl.d/99-sing-box-vpn-bbr.conf;;
        12) certificate_menu;;13) cfg show;;14) read -r -p '已有 SOCKS5 本机端口：' value;cfg socks "$value";reload_main;;
        15) read -r -p '新 VPS IP/域名：' value;cfg address "$value";cfg export;;
        16) echo 'https://github.com/sck03/sing-box-vpn · 原节点与 VPNGate 节点各自独立；临时组手动选择。';;
        17) "$APP_HOME/addons/vpngate/vpngate.sh" menu;;esac
        pause
    done
}
case "${1:-menu}" in
    install) install_main;;export) cfg export;;status) cfg show;;links) cfg links;;
    core-check) python3 "$LIB/core.py" --root "$SB_ROOT" check;;
    core-update) require_system;if [[ -n "${2:-}" ]];then python3 "$LIB/core.py" --root "$SB_ROOT" update --version "$2";else python3 "$LIB/core.py" --root "$SB_ROOT" update;fi;;
    core-rollback) require_system;python3 "$LIB/core.py" --root "$SB_ROOT" rollback;;
    menu) menu;;--help|help) echo 'sb [install|export|status|links|core-check|core-update [VERSION]|core-rollback]；不带参数打开菜单';;
    *) echo '未知命令' >&2;exit 1;;
esac
