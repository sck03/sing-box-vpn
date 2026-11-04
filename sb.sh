#!/usr/bin/env bash
# GPL-3.0. Modernized management derived from sing-box-yg; see NOTICE.md.
set -Eeuo pipefail
SCRIPT_ROOT="$(cd -- "$(dirname -- "$(readlink -f -- "${BASH_SOURCE[0]}")")" && pwd)"
APP_HOME="${SB_HOME:-/opt/sing-box-vpn}"
SB_ROOT="${SB_ROOT:-/etc/s-box}"
CORE="$SB_ROOT/sing-box"
MANAGER="${SB_MANAGER:-/usr/local/bin/yg-vpngate}"
source "$SCRIPT_ROOT/lib/ui.sh"
cfg() { "$MANAGER" config --root "$SB_ROOT" --core "$CORE" "$@"; }
require_root() { [[ $(id -u) == 0 ]] || { echo '需要 root 权限' >&2; return 1; }; }
require_system() {
    require_root
    [[ $(uname -s) == Linux ]] || { echo '服务安装要求 Linux' >&2; return 1; }
    source /etc/os-release
    [[ "$ID" == debian || "$ID" == ubuntu ]] || { echo '当前版本支持 Ubuntu/Debian + systemd' >&2; return 1; }
    command -v systemctl >/dev/null || { echo '需要 systemd' >&2; return 1; }
}
firewall() { "$MANAGER" firewall --root "$SB_ROOT" "$@"; }
reload_main() { systemctl restart sing-box; }
reload_vpngate() { systemctl try-restart sing-box-vpngate; }
arch_name() { case $(uname -m) in x86_64) echo amd64;; aarch64|arm64) echo arm64;; *) echo '仅支持 amd64/arm64' >&2; return 1;; esac; }
install_main() {
    require_system
    local install_arch
    install_arch=$(arch_name)
    if [[ -d "$APP_HOME" && ! -f "$APP_HOME/.managed" ]]; then echo "$APP_HOME 已存在且无项目标记，停止安装" >&2; return 1; fi
    if [[ -d "$SB_ROOT" && ! -f "$SB_ROOT/sb.json" && ! -f "$SB_ROOT/app.json" ]] && [[ -n $(find "$SB_ROOT" -mindepth 1 -maxdepth 1 -print -quit) ]]; then echo '配置目录已有非本项目文件，停止安装' >&2;return 1;fi
    if [[ ! -f "$SCRIPT_ROOT/.bin/linux-$install_arch/yg-vpngate" ]];then
        echo '请使用已验证的正式安装包；源码开发请先运行 go run ./cmd/release 并解压生成的安装包' >&2;return 1
    fi
    apt-get update -qq
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq ca-certificates curl iproute2 iptables procps qrencode tar
    if [[ -d "$APP_HOME" && ! -f "$APP_HOME/.managed" ]]; then echo "$APP_HOME 已存在且无项目标记，停止安装" >&2; return 1; fi
    install -d -m 0755 "$APP_HOME" "$SB_ROOT"
    if [[ "$SCRIPT_ROOT" != "$APP_HOME" ]]; then
        cp -a "$SCRIPT_ROOT/lib" "$SCRIPT_ROOT/addons" "$SCRIPT_ROOT/internal" "$SCRIPT_ROOT/cmd" "$SCRIPT_ROOT/go.mod" "$SCRIPT_ROOT/manifest.go" "$SCRIPT_ROOT/versions.json" "$SCRIPT_ROOT/LICENSE" "$SCRIPT_ROOT/NOTICE.md" "$SCRIPT_ROOT/sb.sh" "$SCRIPT_ROOT/install.sh" "$APP_HOME/"
        if [[ -d "$SCRIPT_ROOT/.bin" ]]; then cp -a "$SCRIPT_ROOT/.bin" "$APP_HOME/"; fi
    fi
    printf '%s\n' 'sck03/sing-box-vpn' > "$APP_HOME/.managed"
    local arch candidate
    arch="$install_arch"
    if [[ -f "$APP_HOME/.bin/linux-$arch/yg-vpngate" ]]; then
        install -m 0755 "$APP_HOME/.bin/linux-$arch/yg-vpngate" /usr/local/bin/yg-vpngate.new
        mv -f /usr/local/bin/yg-vpngate.new /usr/local/bin/yg-vpngate
    else
        echo '发行包缺少静态网关二进制，请使用 install.sh 安装正式发行包' >&2; return 1
    fi
    MANAGER=/usr/local/bin/yg-vpngate
    candidate="$SB_ROOT/sing-box.candidate"
    ui_line "$UI_CYAN" ' 正在下载并校验 Sing-box 稳定核心…'
    "$MANAGER" core --root "$SB_ROOT" prepare --output "$candidate" >/dev/null
    "$MANAGER" config --root "$SB_ROOT" --core "$candidate" init
    "$MANAGER" core --root "$SB_ROOT" activate "$candidate" --no-restart >/dev/null
    rm -f -- "$candidate"
    rm -f -- "$APP_HOME/lib/"{assets,config,core,firewall,remote,render,store,subscription}.py
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
    if [[ -f "$SB_ROOT/subscription.json" ]]; then write_subscription_service; fi
    firewall
    systemctl daemon-reload
    systemctl enable sing-box >/dev/null
    systemctl restart sing-box
    if systemctl is-enabled --quiet sb-subscription 2>/dev/null; then systemctl restart sb-subscription; fi
    if [[ -f "$SB_ROOT/vpngate/gateway.json" ]]; then systemctl restart yg-vpngate sing-box-vpngate; fi
    # Old templates are removed only after successful generation and migration.
    rm -f -- "$SB_ROOT/sb10.json" "$SB_ROOT/sb11.json"
    ui_line "$UI_GREEN" ' 五协议普通节点已安装完成。'
    show_status
    ui_hint '输入 sb 打开管理菜单；菜单 9 查看节点，菜单 17 添加 VPNGate。'
}
write_subscription_service() {
    cat > /etc/systemd/system/sb-subscription.service <<EOF
[Unit]
Description=Token protected sing-box-vpn subscriptions
After=network-online.target
[Service]
ExecStart=/usr/local/bin/yg-vpngate subscription --root $SB_ROOT
WorkingDirectory=$SB_ROOT
Restart=on-failure
RestartSec=5
ProtectSystem=strict
ReadOnlyPaths=$SB_ROOT
NoNewPrivileges=true
[Install]
WantedBy=multi-user.target
EOF
}
subscription_menu() {
    ui_submenu '本地订阅管理' subscription_action \
        '启用 / 重置本地订阅' '停止本地订阅' '查看订阅地址'
}
subscription_action() {
    local port
    case "$1" in
    1) ui_input port '订阅端口，回车随机' || return "$UI_CANCEL"
       cfg subscription-init --port "${port:-0}"
       write_subscription_service
       cfg export; firewall; systemctl daemon-reload; systemctl enable sb-subscription >/dev/null; systemctl restart sb-subscription
       echo '普通五协议订阅：'; subscription_urls normal
       if [[ -f "$SB_ROOT/vpngate/sbox.json" ]]; then echo 'VPNGate 五协议订阅：'; subscription_urls vpngate; fi;;
    2) systemctl disable --now sb-subscription 2>/dev/null || true
       cfg subscription-stop
       firewall;;
    3) echo '普通五协议订阅：'; subscription_urls normal
       if [[ -f "$SB_ROOT/vpngate/sbox.json" ]]; then echo 'VPNGate 五协议订阅：'; subscription_urls vpngate; fi;;
    esac
}
subscription_urls() { cfg subscription-urls --scope "${1:-normal}"; }
telegram_config() {
    local token chat_id
    ui_secret token 'Bot token' || return "$UI_CANCEL"
    ui_input chat_id '你自己的 chat_id' || return "$UI_CANCEL"
    printf '%s' "$token" | "$MANAGER" remote --root "$SB_ROOT" telegram-config --chat-id "$chat_id"
}
gitlab_config() {
    local token project branch
    ui_input project 'GitLab 私有项目 ID 或 group/project' || return "$UI_CANCEL"
    ui_input branch '分支，回车使用 main' || return "$UI_CANCEL"
    ui_secret token '访问令牌' || return "$UI_CANCEL"
    printf '%s' "$token" | "$MANAGER" remote --root "$SB_ROOT" gitlab-config --project "$project" --branch "${branch:-main}"
    echo '配置已保存；在菜单 9 分别选择要推送的普通或 VPNGate 订阅。'
}
protocol_pick() {
    local -a labels=('VLESS Reality' 'VMess WS' 'Hysteria-2' 'TUIC-v5' 'AnyTLS')
    local -a protocols=(vless vmess hysteria2 tuic anytls)
    if [[ "${1:-}" == all ]]; then labels+=('全部协议'); protocols+=(all); fi
    ui_choose '选择要修改的协议' "${labels[@]}" || return 1
    PROTOCOL=${protocols[REPLY-1]}
}
core_menu() {
    ui_submenu 'Sing-box 稳定版核心管理' core_action \
        '查询当前版本 / 最新稳定版' '更新到最新稳定版' \
        '安装指定稳定版本' '回滚到上一核心'
}
core_action() {
    local version
    case "$1" in
    1) "$MANAGER" core --root "$SB_ROOT" check --format text;;
    2) "$MANAGER" core --root "$SB_ROOT" update --format text;;
    3) ui_input version '稳定版版本号，1.14.0 或更新' || return "$UI_CANCEL"
       "$MANAGER" core --root "$SB_ROOT" update --version "$version" --format text;;
    4) "$MANAGER" core --root "$SB_ROOT" rollback;;
    esac
}
settings_menu() {
    while ui_choose '变更普通五协议配置' \
        '设置 Reality 域名（SNI）' '重置 UUID / 密码' '管理 Argo 隧道' \
        '修改 VMess WS 路径' '配置 Telegram 通知' '配置 WARP endpoint' \
        '配置 GitLab 私有订阅' '管理本地订阅' '设置 VMess CDN 地址'; do
        case "$REPLY" in
            3) ui_open_menu "$APP_HOME/lib/argo.sh" "$SB_ROOT";;
            8) subscription_menu;;
            *) ui_perform settings_action "$REPLY";;
        esac
    done
}
settings_action() {
    local value
    case "$1" in
    1) ui_input value 'Reality 域名' || return "$UI_CANCEL"; cfg sni "$value"; reload_main;;
    2) protocol_pick all || return "$UI_CANCEL"; cfg reset "$PROTOCOL"; reload_main;;
    4) ui_input value 'WS 路径' || return "$UI_CANCEL"; cfg ws "$value"; reload_main;;
    5) telegram_config;;
    6) ui_input value '新版 WireGuard endpoint JSON 文件' || return "$UI_CANCEL"; cfg warp "$value"; reload_main;;
    7) gitlab_config;;
    9) ui_input value 'CDN 地址，回车清除' || return "$UI_CANCEL"; cfg cdn "$value";;
    esac
}
port_menu() {
    ui_submenu '普通五协议端口管理' port_action \
        '更改协议主端口' '添加 Hysteria-2 端口跳跃' '清除 Hysteria-2 端口跳跃'
}
port_action() {
    local value
    local -a ports
    case "$1" in
    1) protocol_pick || return "$UI_CANCEL"; ui_input value '新端口' || return "$UI_CANCEL"
       cfg port "$PROTOCOL" "$value";firewall;reload_main;;
    2) ui_hint '端口或范围用空格分隔，例如：31000:31100 31200'
       ui_input value '端口 / 范围' || return "$UI_CANCEL"
       read -r -a ports <<< "$value";cfg hopping "${ports[@]}";firewall;reload_main;;
    3) cfg hopping;firewall;reload_main;;
    esac
}
route_menu() {
    ui_submenu '普通节点域名分流' route_action \
        '设置 SOCKS5 分流域名' '设置 WARP 分流域名' \
        '设置 IPv4 直连域名' '设置 IPv6 直连域名' '查看当前分流'
}
route_action() {
    local value channel
    local -a channels=(socks warp direct4 direct6) domains
    if [[ "$1" == 5 ]]; then show_status; return; fi
    channel=${channels[$1-1]}
    ui_input value '后缀域名，空格分隔；回车清空' || return "$UI_CANCEL"
    read -r -a domains <<< "$value"
    cfg route "$channel" "${domains[@]}"; reload_main
}
certificate_menu() {
    ui_submenu '普通节点 TLS 证书管理' certificate_action \
        '导入 PEM 证书 / 私钥' '使用 Certbot 申请域名证书'
}
certificate_action() {
    local cert key domain email
    local -a protocols=(vmess hysteria2 tuic anytls all)
    case "$1" in
    1) ui_choose '选择证书协议（VLESS 使用 Reality）' \
           'VMess WS' 'Hysteria-2' 'TUIC-v5' 'AnyTLS' '全部 TLS 协议' || return "$UI_CANCEL"
       PROTOCOL=${protocols[REPLY-1]}
       ui_input cert '证书绝对路径' || return "$UI_CANCEL"; ui_input key '私钥绝对路径' || return "$UI_CANCEL"
       ui_input domain '证书域名' || return "$UI_CANCEL"
       cfg tls "$PROTOCOL" "$cert" "$key" "$domain";reload_main;;
    2) ui_input domain '已解析到此 VPS 的域名' || return "$UI_CANCEL"
       ui_input email '邮箱' || return "$UI_CANCEL"
       [[ "$domain" =~ ^[A-Za-z0-9.-]+$ && "$email" == *@* ]] || return 1
       apt-get install -y -qq certbot
       certbot certonly --standalone --non-interactive --agree-tos --email "$email" -d "$domain"
       cfg tls all "/etc/letsencrypt/live/$domain/fullchain.pem" "/etc/letsencrypt/live/$domain/privkey.pem" "$domain";reload_main
       install -d /etc/letsencrypt/renewal-hooks/deploy
       printf '#!/bin/sh\nsystemctl try-restart sing-box sing-box-vpngate\nsb export\n' > /etc/letsencrypt/renewal-hooks/deploy/sing-box-vpn.sh
       chmod 0755 /etc/letsencrypt/renewal-hooks/deploy/sing-box-vpn.sh;;
    esac
}
vpngate_settings_menu() {
    ui_submenu 'VPNGate 五协议配置' vpngate_settings_action \
        '更改五协议主端口' '重置 UUID / 密码' '设置 Reality 域名（SNI）' \
        '修改 VMess WS 路径' '统一导入 TLS 证书（VLESS 保持 Reality）' '查看当前节点配置'
}
vpngate_settings_action() {
    local value cert key domain
    case "$1" in
    1) protocol_pick || return "$UI_CANCEL"; ui_input value '新端口' || return "$UI_CANCEL"
       cfg port "$PROTOCOL" "$value" --scope vpngate;firewall;reload_vpngate;;
    2) protocol_pick all || return "$UI_CANCEL";cfg reset "$PROTOCOL" --scope vpngate;reload_vpngate;;
    3) ui_input value 'Reality 域名' || return "$UI_CANCEL";cfg sni "$value" --scope vpngate;reload_vpngate;;
    4) ui_input value 'WS 路径' || return "$UI_CANCEL";cfg ws "$value" --scope vpngate;reload_vpngate;;
    5) ui_input cert '证书绝对路径' || return "$UI_CANCEL"; ui_input key '私钥绝对路径' || return "$UI_CANCEL"
       ui_input domain '证书域名' || return "$UI_CANCEL"
       cfg tls all "$cert" "$key" "$domain" --scope vpngate;reload_vpngate;;
    6) show_status vpngate;;
    esac
}
share_menu() {
    if [[ -n "${1:-}" ]]; then
        share_scope_menu "$1"
        return
    fi
    while ui_choose '刷新并查看节点 / 订阅' \
        '普通五协议（VPS 出口 / 已配置分流）' 'VPNGate 五协议（当前国家出口）'; do
        case "$REPLY" in 1) share_scope_menu normal;;2) share_scope_menu vpngate;;esac
    done
}
share_scope_menu() {
    local scope=$1 title='普通五协议 · 节点与订阅'
    if [[ "$scope" == vpngate ]]; then title='VPNGate 五协议 · 节点与订阅'; fi
    while ui_choose "$title" '查看 URI / 二维码 / 聚合节点' '生成客户端配置 / 查看订阅地址' \
        '推送到已配置的 Telegram' '推送到私有 GitLab'; do
        ui_perform share_action "$scope" "$REPLY"
    done
}
share_action() {
    local scope=$1 choice=$2 export_dir="$SB_ROOT" link
    if [[ "$scope" == vpngate ]]; then export_dir="$SB_ROOT/vpngate"; fi
    case "$choice" in
    1) cfg links --scope "$scope"; if command -v qrencode >/dev/null;then while IFS= read -r link;do qrencode -t ANSIUTF8 "$link";done < "$export_dir/jhsub.txt";fi;;
    2) cfg export --scope "$scope";echo "Sing-box: $export_dir/sbox.json";echo "Mihomo: $export_dir/clmi.yaml";subscription_urls "$scope";;
    3) cfg export --scope "$scope";"$MANAGER" remote --root "$SB_ROOT" telegram-push --scope "$scope";;
    4) cfg export --scope "$scope";"$MANAGER" remote --root "$SB_ROOT" gitlab-push --scope "$scope";;
    esac
}
uninstall_main() {
    [[ "$SB_ROOT" == /etc/s-box && -f "$SB_ROOT/app.json" ]] || { echo '配置目录不符合安全清理条件' >&2;return 1; }
    [[ "$APP_HOME" == /opt/sing-box-vpn && -f "$APP_HOME/.managed" ]] || { echo '程序目录不符合安全清理条件' >&2;return 1; }
    "$APP_HOME/addons/vpngate/vpngate.sh" uninstall --all || return 1
    systemctl disable --now sing-box sb-subscription sb-argo 2>/dev/null || true
    firewall --remove
    rm -f /etc/systemd/system/sing-box.service /etc/systemd/system/sb-subscription.service /etc/systemd/system/sb-argo.service /usr/bin/sb /usr/local/bin/yg-vpngate
    rm -f /etc/letsencrypt/renewal-hooks/deploy/sing-box-vpn.sh /etc/sysctl.d/99-sing-box-vpn-bbr.conf
    rm -rf -- "$SB_ROOT"
    rm -rf -- "$APP_HOME"
    systemctl daemon-reload
    ui_line "$UI_GREEN" ' Sing-box 与 VPNGate 已卸载。'
}
show_status() {
    local status line
    if status=$(cfg show --scope "${1:-normal}" --format text); then
        while IFS= read -r line; do
            case "$line" in
                *'五协议节点：'|*'域名分流：') ui_rule; ui_line "$UI_WHITE" "$line";;
                *'端口：'*) ui_line "$UI_YELLOW" "$line";;
                *) ui_line "$UI_CYAN" "$line";;
            esac
        done <<< "$status"
    else
        ui_line "$UI_YELLOW" ' 节点信息暂时无法读取，请检查上方提示。'
    fi
}
main_status() {
    local release='未安装' congestion
    if [[ -x "$MANAGER" ]]; then release=$("$MANAGER" --version 2>/dev/null) || release='未知'; fi
    ui_field '当前脚本版本' "$release（菜单 7 更新）"
    ui_rule
    ui_line "$UI_WHITE" ' VPS 与服务状态：'
    ui_field '系统' "${PRETTY_NAME:-Linux}"
    ui_field '内核 / 架构' "$(uname -r) / $(uname -m)"
    congestion=$(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null) || congestion='未知'
    ui_field 'TCP 拥塞控制' "$congestion"
    ui_service 'Sing-box 状态' sing-box
    ui_rule "$UI_RED" '~'
    if [[ -f "$SB_ROOT/app.json" ]]; then
        show_status
    else
        ui_line "$UI_YELLOW" ' 尚未安装普通五协议节点，请选择 1 安装。'
    fi
    ui_gateway_status "$MANAGER" "$SB_ROOT/vpngate/gateway.json"
    ui_rule "$UI_RED" '~'
}
service_menu() {
    ui_submenu 'Sing-box 服务管理' service_action '启动 Sing-box' '停止 Sing-box' '重启 Sing-box' '查看服务状态'
}
service_action() {
    local -a actions=(start stop restart)
    if [[ "$1" != 4 ]]; then systemctl "${actions[$1-1]}" sing-box; fi
    ui_service 'Sing-box 状态' sing-box
}
socks_menu() { ui_submenu '本机 SOCKS5 分流入口' socks_action '配置已有的本机 SOCKS5 端口'; }
socks_action() {
    local value
    ui_input value '已有 SOCKS5 本机端口' || return "$UI_CANCEL"
    cfg socks "$value"; reload_main
}
address_menu() { ui_submenu '节点入口地址管理' address_action '更换 VPS IP / 域名并刷新节点'; }
address_action() {
    local value
    ui_input value '新 VPS IP / 域名' || return "$UI_CANCEL"
    cfg address "$value"
}
main_menu_render() {
    ui_header 'Sing-box-vpn · 五协议共存管理'
    ui_option 1 '一键安装 / 迁移 Sing-box 五协议'
    ui_option 2 '删除卸载 Sing-box'
    ui_rule
    ui_option 3 '变更配置（TLS / UUID / Argo / 订阅）'
    ui_option 4 '更改主端口 / Hysteria-2 端口跳跃'
    ui_option 5 '设置普通节点域名分流'
    ui_option 6 '启动 / 停止 / 重启 Sing-box'
    ui_option 7 '更新 Sing-box-vpn 脚本'
    ui_option 8 '更新 / 指定 / 回滚 Sing-box 稳定核心'
    ui_rule
    ui_option 9 '刷新并查看节点 / 订阅 / 推送'
    ui_option 10 '查看 Sing-box / VPNGate 运行日志'
    ui_option 11 '一键开启 BBR + FQ 加速'
    ui_option 12 '管理 TLS 证书 / Certbot 申请证书'
    ui_option 13 '查看 WARP 与域名分流配置'
    ui_option 14 '配置已有本机 SOCKS5 代理'
    ui_option 15 '更换节点入口 IP / 域名'
    ui_rule
    ui_option 16 'Sing-box-vpn 使用说明'
    ui_option 17 'VPNGate 临时家宽候选管理'
    ui_rule
    ui_option 0 '退出脚本'
    ui_rule "$UI_RED" '~'
    main_status
}
main_action() {
    case "$1" in
        1) install_main;;
        7) curl -fLsS https://raw.githubusercontent.com/sck03/sing-box-vpn/main/install.sh -o /tmp/sb-vpn-update.sh
           bash /tmp/sb-vpn-update.sh; rm -f /tmp/sb-vpn-update.sh;;
        10) journalctl -u sing-box -u yg-vpngate -u sing-box-vpngate -n 100 --no-pager;;
        11) modprobe tcp_bbr
            printf 'net.core.default_qdisc=fq\nnet.ipv4.tcp_congestion_control=bbr\n' > /etc/sysctl.d/99-sing-box-vpn-bbr.conf
            sysctl -p /etc/sysctl.d/99-sing-box-vpn-bbr.conf;;
        13) show_status;;
        16) ui_header 'Sing-box-vpn 使用说明'
            ui_line "$UI_WHITE" ' 项目：https://github.com/sck03/sing-box-vpn'
            ui_hint '菜单 9：分别查看普通节点与 VPNGate 节点。'
            ui_hint '菜单 3 → 8：配置本地订阅。'
            ui_hint '菜单 17：选择 VPNGate 国家，管理临时出口。'
            ui_hint '各级子菜单输入 0 返回；填写参数时也可输入 0 取消。'
            ui_hint '普通节点与 VPNGate 使用独立的端口和凭据。';;
    esac
}
menu() {
    require_system
    while true;do
        main_menu_render
        ui_read_choice || return 0
        case "$REPLY" in
            0) return;;
            2) ui_header '删除卸载 Sing-box'
               ui_line "$UI_YELLOW" ' 将停止服务，并删除本项目的普通 / VPNGate 节点配置。'
               if ui_confirm '确认卸载'; then
                   ui_run uninstall_main
                   if ((UI_RESULT == 0)); then return; fi
                   ui_pause
               fi
               continue;;
            3) settings_menu;continue;;4) port_menu;continue;;5) route_menu;continue;;
            6) service_menu;continue;;8) core_menu;continue;;9) share_menu;continue;;
            12) certificate_menu;continue;;14) socks_menu;continue;;15) address_menu;continue;;
            17) ui_open_menu "$APP_HOME/addons/vpngate/vpngate.sh" menu;continue;;
            1|7|10|11|13|16) ui_run main_action "$REPLY";;
            *) ui_invalid;;
        esac
        ui_pause
    done
}
if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then return 0; fi
case "${1:-menu}" in
    install) install_main;;export) cfg export --scope "${2:-normal}";;status) cfg show --scope "${2:-normal}";;links) cfg links --scope "${2:-normal}";;
    vpngate-share) share_menu vpngate;;
    vpngate-settings) vpngate_settings_menu;;
    core-check) "$MANAGER" core --root "$SB_ROOT" check;;
    core-update) require_system;if [[ -n "${2:-}" ]];then "$MANAGER" core --root "$SB_ROOT" update --version "$2";else "$MANAGER" core --root "$SB_ROOT" update;fi;;
    core-rollback) require_system;"$MANAGER" core --root "$SB_ROOT" rollback;;
    menu) menu;;--help|help) echo 'sb [install|export [normal|vpngate]|status|links [normal|vpngate]|core-check|core-update [VERSION]|core-rollback]；不带参数打开菜单';;
    *) echo '未知命令' >&2;exit 1;;
esac
