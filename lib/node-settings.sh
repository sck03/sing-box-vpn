#!/usr/bin/env bash
# Shared protocol forms for ordinary and VPNGate nodes. Validation stays in Go.
protocol_pick() {
    local -a labels=('VLESS Reality' 'VMess WS' 'Hysteria-2' 'TUIC-v5' 'AnyTLS')
    local -a protocols=(vless vmess hysteria2 tuic anytls)
    if [[ "${1:-}" == all ]]; then labels+=('全部协议'); protocols+=(all); fi
    ui_choose '选择要修改的协议' "${labels[@]}" || return 1
    PROTOCOL=${protocols[REPLY-1]}
}
credentials_menu() {
    local scope=${1:-normal} title='普通五协议 · UUID / 密码'
    if [[ "$scope" == vpngate ]]; then title='VPNGate 五协议 · UUID / 密码'; fi
    while ui_choose "$title" '自定义 UUID / 密码' '随机重置 UUID / 密码'; do
        ui_perform credentials_action "$REPLY" "$scope"
    done
}
credentials_action() {
    local choice=$1 scope=$2 value password=''
    protocol_pick all || return "$UI_CANCEL"
    if [[ "$choice" == 2 ]]; then
        cfg reset "$PROTOCOL" --scope "$scope"
    else
        case "$PROTOCOL" in
            all) ui_hint '统一 UUID 用于 VLESS、VMess、TUIC；下一步可另设三个密码协议的统一密码。'
                 ui_secret value '统一 UUID，回车取消' || return "$UI_CANCEL";;
            hysteria2|anytls) ui_secret value '协议密码，回车取消' || return "$UI_CANCEL";;
            tuic) ui_secret value 'TUIC UUID，回车保留现有 UUID' || return "$UI_CANCEL";;
            *) ui_secret value 'UUID，回车取消' || return "$UI_CANCEL";;
        esac
        case "$PROTOCOL" in
            tuic) ui_secret password 'TUIC 密码，回车保留现有密码' || return "$UI_CANCEL"
                  [[ -n "$value$password" ]] || return "$UI_CANCEL";;
            all) [[ -n "$value" ]] || return "$UI_CANCEL"
                 ui_secret password '统一密码，回车使用上述 UUID' || return "$UI_CANCEL"
                 password=${password:-$value};;
            *) [[ -n "$value" ]] || return "$UI_CANCEL";;
        esac
        if [[ "$PROTOCOL" == tuic || "$PROTOCOL" == all ]]; then
            printf '%s\n%s\n' "$value" "$password" | cfg credentials "$PROTOCOL" --stdin --scope "$scope"
        else
            printf '%s\n' "$value" | cfg credentials "$PROTOCOL" --stdin --scope "$scope"
        fi
    fi
    reload_scope "$scope"
    ui_hint '凭据已更新，请从菜单 9 获取新的节点和订阅。'
}
port_menu() {
    local scope=${1:-normal} title='普通五协议端口管理'
    if [[ "$scope" == vpngate ]]; then title='VPNGate 五协议端口管理'; fi
    while ui_choose "$title" '更改协议主端口' '设置 Hysteria-2 多端口 / 跳跃' \
        '清除 Hysteria-2 多端口' '设置 TUIC-v5 多端口复用' '清除 TUIC-v5 多端口' \
        '生成指定 TUIC 端口的分享链接' '查看当前端口'; do
        ui_perform port_action "$REPLY" "$scope"
    done
}
port_action() {
    local value scope=${2:-normal} protocol=hysteria2
    local -a ports=()
    case "$1" in
    1) protocol_pick || return "$UI_CANCEL"; ui_input value '新端口' || return "$UI_CANCEL"
       cfg port "$PROTOCOL" "$value" --scope "$scope";firewall;reload_scope "$scope";;
    2|3|4|5)
       if [[ "$1" == 4 || "$1" == 5 ]]; then protocol=tuic; fi
       if [[ "$1" == 2 || "$1" == 4 ]]; then
           if [[ "$protocol" == tuic ]]; then
               ui_hint '多个 UDP 端口共用一个 TUIC 入口和凭据；客户端选择其中一个端口，不自动跳跃。'
           fi
           ui_hint '端口或范围用空格分隔，例如：31000:31100 31200；本次输入替换已有设置。'
           ui_input value '端口 / 范围，回车清空' || return "$UI_CANCEL"
           read -r -a ports <<< "$value"
       fi
       cfg multiport "$protocol" "${ports[@]}" --scope "$scope";firewall
       show_status "$scope";;
    6) ui_input value 'TUIC 主端口或已设置的复用端口' || return "$UI_CANCEL"
       cfg links --scope "$scope" --tuic-port "$value";;
    7) show_status "$scope";;
    esac
}
