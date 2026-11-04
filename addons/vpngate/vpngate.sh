#!/usr/bin/env bash
set -Eeuo pipefail
ADDON_ROOT="$(cd -- "$(dirname -- "$(readlink -f -- "${BASH_SOURCE[0]}")")/../.." && pwd)"
source "$ADDON_ROOT/lib/ui.sh"
source "$ADDON_ROOT/lib/services.sh"
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
    write_gateway_services
    /usr/local/bin/yg-vpngate firewall --root "$SB_ROOT"
    systemctl daemon-reload
    systemctl enable yg-vpngate sing-box-vpngate >/dev/null
    if systemctl is-active --quiet yg-vpngate;then ctl reload;else systemctl start yg-vpngate;fi
    systemctl restart sing-box-vpngate
    echo '连接与出口校验在后台进行；通过校验后才转发。菜单 9 可复制五协议 VPNGate 节点。'
    ctl status
}
uninstall_gateway() {
    [[ $(id -u) == 0 ]] || return 1
    [[ $(realpath -m -- "$SB_ROOT/vpngate") == /etc/s-box/vpngate ]] || { echo '配置清理路径验证失败' >&2;return 1; }
    systemctl disable --now sing-box-vpngate yg-vpngate 2>/dev/null || true
    if [[ -f "$CONFIG" && -x /usr/local/bin/yg-vpngate ]];then /usr/local/bin/yg-vpngate cleanup --config "$CONFIG";fi
    remove_service_recovery sing-box-vpngate yg-vpngate
    rm -f /etc/systemd/system/sing-box-vpngate.service /etc/systemd/system/yg-vpngate.service
    if [[ "${1:-}" != --all && -f "$SB_ROOT/app.json" ]];then cfg vpngate-remove;fi
    rm -rf -- /etc/s-box/vpngate /var/lib/yg-vpngate /run/yg-vpngate
    if [[ "${1:-}" != --all && -f "$SB_ROOT/app.json" ]];then /usr/local/bin/yg-vpngate firewall --root "$SB_ROOT";fi
    systemctl daemon-reload
    if [[ "${1:-}" == --all ]]; then echo 'VPNGate 组件已清理。'; else echo 'VPNGate 已卸载，普通节点继续使用。'; fi
}
gateway_menu_render() {
    ui_header 'VPNGate · 临时家宽候选管理'
    ui_hint '同时激活一个国家，五协议共用该国家出口。'
    ui_rule
    ui_option 1 '安装 VPNGate / 选择国家'
    ui_option 2 '查看连接与出口状态'
    ui_option 3 '查看候选节点'
    ui_rule
    ui_option 4 '更换同国家出口 IP'
    ui_option 5 '尽量保持当前节点 / 故障自动重连'
    ui_option 6 '锁定当前节点'
    ui_option 7 '暂停 VPNGate 出口'
    ui_option 8 '恢复 VPNGate 出口'
    ui_option 9 '刷新节点源'
    ui_rule
    ui_option 10 '配置节点镜像源'
    ui_option 11 '设置 ISP / hosting 筛选策略'
    ui_option 12 '卸载 VPNGate（保留普通节点）'
    ui_rule
    ui_option 13 '查看 VPNGate 五协议节点 / 订阅'
    ui_option 14 '修改 VPNGate 端口 / 凭据 / 证书'
    ui_rule
    ui_option 0 '返回上一级'
    ui_gateway_status "${SB_MANAGER:-/usr/local/bin/yg-vpngate}" "$CONFIG"
    ui_rule "$UI_RED" '~'
}
gateway_action() {
    local country sources isp hosting
    case "$1" in
        1) ui_input country '国家代码，例如 JP / KR / US' || return "$UI_CANCEL";install_gateway "$country";;
        2) ui_gateway_status "${SB_MANAGER:-/usr/local/bin/yg-vpngate}" "$CONFIG";;
        3) ctl nodes;;4) ctl swap;;5) ctl mode --mode sticky;;6) ctl mode --mode fixed;;7) ctl stop;;8) ctl start;;9) ctl refresh;;
        10) ui_hint '多个 API 源 URL 用逗号分隔，官方源放第一项。'
            ui_input sources '完整 API 源 URL' || return "$UI_CANCEL";ctl policy --sources "$sources";;
        11) ui_input isp '要求已确认 ISP？true / false' || return "$UI_CANCEL"
            ui_input hosting '排除已知 hosting？true / false' || return "$UI_CANCEL"
            ctl policy --require-isp="$isp" --exclude-hosting="$hosting";;
    esac
}
menu() {
    while true;do
        gateway_menu_render
        ui_read_choice || return 0
        case "$REPLY" in
            0) return;;
            12) if ui_confirm '卸载 VPNGate，保留普通节点'; then
                    ui_run uninstall_gateway
                    if ((UI_RESULT == 0)); then return; fi
                    ui_pause
                fi
                continue;;
            13) ui_open_menu sb vpngate-share;continue;;
            14) ui_open_menu sb vpngate-settings;continue;;
            1|2|3|4|5|6|7|8|9|10|11) ui_perform gateway_action "$REPLY";;
            *) ui_invalid;ui_pause;;
        esac
    done
}
if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then return 0; fi
case "${1:-menu}" in
    menu) menu;;install|country) install_gateway "${2:-JP}";;uninstall) uninstall_gateway "${2:-}";;
    start|stop|swap|status|nodes|refresh) ctl "$1";;
    *) echo 'vpngate.sh menu|install COUNTRY|status|start|stop|swap|uninstall' >&2;exit 1;;
esac
