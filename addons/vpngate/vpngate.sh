#!/usr/bin/env bash
set -Eeuo pipefail
SB_ROOT="${SB_ROOT:-/etc/s-box}"
CONFIG="$SB_ROOT/vpngate/gateway.json"
ctl() { /usr/local/bin/yg-vpngate ctl "$1" --config "$CONFIG" "${@:2}"; }
cfg() { /usr/local/bin/yg-vpngate config --root "$SB_ROOT" "$@"; }
preflight() {
    [[ $(id -u) == 0 && $(uname -s) == Linux ]] || { echo 'VPNGate 要求 Linux root' >&2;return 1; }
    [[ -c /dev/net/tun ]] || { echo '缺少 /dev/net/tun，请在 VPS 面板启用 TUN' >&2;return 1; }
    [[ -f "$SB_ROOT/app.json" && -x /usr/local/bin/yg-vpngate ]] || { echo '先安装普通五协议节点' >&2;return 1; }
    command -v systemctl >/dev/null || return 1
}
install_gateway() {
    preflight
    local country="${1:-JP}"
    [[ "$country" =~ ^[A-Za-z]{2}$ ]] || { echo '国家代码格式错误' >&2;return 1; }
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq openvpn iproute2 iptables procps
    install -d -m 0700 "$SB_ROOT/vpngate" /var/lib/yg-vpngate /run/yg-vpngate
    cfg vpngate "$country"
    cat > /etc/systemd/system/yg-vpngate.service <<EOF
[Unit]
Description=VPNGate country-locked gateway
After=network-online.target
Wants=network-online.target
[Service]
User=root
UMask=0077
ExecStart=/usr/local/bin/yg-vpngate daemon --config $CONFIG
ExecStopPost=/usr/local/bin/yg-vpngate cleanup --config $CONFIG
Restart=on-failure
RestartSec=5
TimeoutStopSec=20
NoNewPrivileges=true
LimitNOFILE=32768
[Install]
WantedBy=multi-user.target
EOF
    cat > /etc/systemd/system/sing-box-vpngate.service <<EOF
[Unit]
Description=VPNGate five-protocol dedicated entries
After=network-online.target yg-vpngate.service
[Service]
User=root
UMask=0077
ExecStart=$SB_ROOT/sing-box run -c $SB_ROOT/vpngate/server.json
ExecReload=/bin/kill -HUP \$MAINPID
WorkingDirectory=$SB_ROOT/vpngate
Restart=on-failure
RestartSec=5
LimitNOFILE=32768
[Install]
WantedBy=multi-user.target
EOF
    /usr/local/bin/yg-vpngate firewall --root "$SB_ROOT"
    systemctl daemon-reload
    if systemctl is-active --quiet yg-vpngate;then ctl reload;else systemctl enable --now yg-vpngate >/dev/null;fi
    systemctl enable sing-box-vpngate >/dev/null
    systemctl restart sing-box-vpngate
    echo '连接与出口校验在后台进行；通过校验后才转发。菜单 9 可复制五协议 VPNGate 节点。'
    ctl status
}
uninstall_gateway() {
    [[ $(id -u) == 0 ]] || return 1
    systemctl disable --now sing-box-vpngate yg-vpngate 2>/dev/null || true
    if [[ -f "$CONFIG" && -x /usr/local/bin/yg-vpngate ]];then /usr/local/bin/yg-vpngate cleanup --config "$CONFIG";fi
    rm -f /etc/systemd/system/sing-box-vpngate.service /etc/systemd/system/yg-vpngate.service
    if [[ -f "$SB_ROOT/app.json" ]];then cfg vpngate-remove;fi
    [[ $(realpath -m -- "$SB_ROOT/vpngate") == /etc/s-box/vpngate ]] || { echo '配置清理路径验证失败' >&2;return 1; }
    rm -rf -- /etc/s-box/vpngate /var/lib/yg-vpngate /run/yg-vpngate
    if [[ -f "$SB_ROOT/app.json" ]];then /usr/local/bin/yg-vpngate firewall --root "$SB_ROOT";fi
    systemctl daemon-reload
    echo 'VPNGate 已卸载，普通节点继续使用。'
}
menu() {
    while true;do
        echo;echo 'VPNGate · 家宽候选 · 同时激活一个国家'
        echo '1. 安装/选择国家  2. 状态  3. 候选节点'
        echo '4. 同国家换 IP    5. 尽量保持/自动重连  6. 锁定当前节点'
        echo '7. 暂停出口       8. 恢复出口  9. 刷新节点源'
        echo '10. 镜像源       11. ISP/hosting 策略  12. 卸载  0. 返回'
        read -r -p '选择：' choice || return 0
        case "$choice" in
        0) return;;
        1) read -r -p '国家代码 JP/KR/US 等：' country;install_gateway "$country";;
        2) ctl status;;3) ctl nodes;;4) ctl swap;;5) ctl mode --mode sticky;;6) ctl mode --mode fixed;;7) ctl stop;;8) ctl start;;9) ctl refresh;;
        10) read -r -p '完整 API 源 URL，逗号分隔，官方源放第一项：' sources;ctl policy --sources "$sources";;
        11) read -r -p '要求已确认 ISP？true/false：' isp;read -r -p '排除已知 hosting？true/false：' hosting;ctl policy --require-isp="$isp" --exclude-hosting="$hosting";;
        12) uninstall_gateway;return;;
        esac
    done
}
case "${1:-menu}" in
    menu) menu;;install|country) install_gateway "${2:-JP}";;uninstall) uninstall_gateway;;
    start|stop|swap|status|nodes|refresh) ctl "$1";;
    *) echo 'vpngate.sh menu|install COUNTRY|status|start|stop|swap|uninstall' >&2;exit 1;;
esac
