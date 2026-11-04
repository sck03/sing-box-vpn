#!/usr/bin/env bash
# Shared terminal layout for the main menu and its add-ons.
UI_RESET='' UI_GREEN='' UI_RED='' UI_YELLOW='' UI_CYAN='' UI_WHITE=''
if [[ -t 1 && ${TERM:-dumb} != dumb && -z ${NO_COLOR+x} ]]; then
    UI_RESET=$'\033[0m' UI_GREEN=$'\033[1;32m' UI_RED=$'\033[1;31m'
    UI_YELLOW=$'\033[1;33m' UI_CYAN=$'\033[1;36m' UI_WHITE=$'\033[1;37m'
fi
UI_WIDTH=72
# Reserved result for cancelling an input form without a pause or error.
UI_CANCEL=200
ui_line() { printf '%s%s%s\n' "$1" "$2" "$UI_RESET"; }
ui_rule() {
    local line
    printf -v line '%*s' "$UI_WIDTH" ''
    line=${line// /${2:--}}
    ui_line "${1:-$UI_WHITE}" "$line"
}
ui_header() {
    local columns=${COLUMNS:-80}
    if [[ -t 1 && ${TERM:-dumb} != dumb ]]; then
        columns=$(tput cols 2>/dev/null) || columns=80
        printf '\033[H\033[2J'
    fi
    [[ "$columns" =~ ^[0-9]+$ ]] || columns=80
    UI_WIDTH=$((columns > 72 ? 72 : columns))
    printf '\n'
    ui_rule "$UI_RED" '~'
    ui_line "$UI_CYAN" " $1"
    ui_rule "$UI_RED" '~'
}
ui_option() { printf '%s%2s. %s%s\n' "$UI_GREEN" "$1" "$2" "$UI_RESET"; }
ui_hint() { ui_line "$UI_CYAN" "    $1"; }
ui_field() { printf ' %s：%s%s%s\n' "$1" "${3:-$UI_CYAN}" "$2" "$UI_RESET"; }
ui_invalid() { ui_line "$UI_YELLOW" ' 输入无效，请输入菜单中显示的数字。'; }
ui_pause() { printf '\n'; read -r -p "${UI_YELLOW}按回车继续…${UI_RESET}" _ || true; }
ui_read_choice() { printf '\n'; read -r -p "${UI_YELLOW}请输入选择：${UI_RESET}" REPLY; }
ui_back() { ui_rule; ui_option 0 '返回上一级'; ui_rule "$UI_RED" '~'; }

# REPLY holds a validated selection; zero and EOF return to the caller.
ui_choose() {
    local title=$1 index=1 label
    shift
    ui_header "$title"
    for label in "$@"; do ui_option "$index" "$label"; index=$((index + 1)); done
    ui_back
    while ui_read_choice; do
        [[ "$REPLY" == 0 ]] && return 1
        for ((index=1; index <= $#; index++)); do
            [[ "$REPLY" == "$index" ]] && return 0
        done
        ui_invalid
    done
    return 1
}
ui_input() { read -r -p "$2（0 返回）：" "$1" && [[ ${!1} != 0 ]]; }
ui_secret() {
    IFS= read -rs -p "$2（隐藏，0 返回）：" "$1" || { printf '\n'; return 1; }
    printf '\n'
    [[ ${!1} != 0 ]]
}
ui_confirm() {
    local answer
    while true; do
        read -r -p "${UI_YELLOW}$1 [y/N]：${UI_RESET}" answer || return 1
        case "${answer,,}" in
            y|yes) return 0;;
            ''|n|no|0) return 1;;
            *) ui_line "$UI_YELLOW" ' 请输入 y 确认，或按回车取消。';;
        esac
    done
}

# Keep fail-fast behavior inside an action, but return to the menu on failure.
# A subshell called in an `if` would disable errexit inside shell functions.
ui_run() {
    set +e
    ( set -e; "$@" )
    UI_RESULT=$?
    set -e
    if ((UI_RESULT != 0 && UI_RESULT != UI_CANCEL)); then
        ui_line "$UI_RED" " 操作未完成（退出码 $UI_RESULT），请查看上方提示。"
    fi
    return 0
}
ui_perform() {
    ui_run "$@"
    if ((UI_RESULT != UI_CANCEL)); then ui_pause; fi
}
ui_open_menu() {
    ui_run "$@"
    if ((UI_RESULT != 0 && UI_RESULT != UI_CANCEL)); then ui_pause; fi
}
ui_submenu() {
    local title=$1 handler=$2
    shift 2
    while ui_choose "$title" "$@"; do
        ui_perform "$handler" "$REPLY"
    done
}
ui_service() {
    local state
    state=$(systemctl is-active "$2" 2>/dev/null) || true
    case "$state" in
        active) ui_field "$1" '运行中' "$UI_GREEN";;
        activating|reloading) ui_field "$1" '启动 / 重载中' "$UI_YELLOW";;
        failed) ui_field "$1" '运行异常（菜单 10 查看日志）' "$UI_RED";;
        inactive|deactivating) ui_field "$1" '已停止' "$UI_YELLOW";;
        *) ui_field "$1" '未安装 / 状态不可用' "$UI_YELLOW";;
    esac
}
ui_gateway_status() {
    local manager=$1 config=$2 status line
    ui_rule "$UI_RED" '~'
    ui_line "$UI_WHITE" ' VPNGate 出口状态（菜单 17）：'
    if [[ ! -f "$config" ]]; then
        ui_field 'VPNGate' '未安装 / 尚未选择国家' "$UI_YELLOW"
        return 0
    fi
    ui_service '网关进程' yg-vpngate
    ui_service '五协议入口' sing-box-vpngate
    if status=$("$manager" ctl status --config "$config" --format text --timeout 2s); then
        while IFS= read -r line; do
            case "$line" in
                *'已连接（出口已验证）'*) ui_line "$UI_GREEN" "$line";;
                *'连接状态：'*) ui_line "$UI_YELLOW" "$line";;
                *) ui_line "$UI_CYAN" "$line";;
            esac
        done <<< "$status"
    else
        ui_line "$UI_YELLOW" ' VPNGate 状态读取失败，请在菜单 17 检查配置。'
    fi
}
