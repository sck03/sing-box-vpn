#!/usr/bin/env bash
# All validation and persistence are provided by the Go subscription service.
subs() { "$MANAGER" subscriptions --root "$SB_ROOT" --core "$CORE" "$@"; }
subscription_menu() {
    ui_submenu '订阅与客户端管理' subscription_action \
        '启用 / 修改监听与公网地址' '停用订阅' '查看默认订阅地址' \
        '重置默认订阅令牌' '查看来源 / 集合 / 模板配置' '预览配置文件' \
        '应用配置文件' '立即刷新远端来源 / 模板 / 规则' '创建独立分享' \
        '撤销独立分享' '查看刷新记录和流量信息' '导出加密备份' \
        '预览 / 恢复加密备份' '查看回收站' '归档对象' '恢复归档对象' '查看客户端能力' \
        '打开本机 Web 管理（按 Ctrl+C 退出）' '按需检测节点连接' '分发到已配置目的地'
}
subscription_action() {
    local port listen public file id outputs expires password kind count
    case "$1" in
    1) ui_input port '订阅端口，回车保留或自动分配' || return "$UI_CANCEL"
       ui_input listen '监听 IP，回车保留或使用 ::' || return "$UI_CANCEL"
       ui_input public '公网基础地址，例如 https://sub.example.com/path；回车保留，- 清除' || return "$UI_CANCEL"
       write_subscription_service || return
       cfg subscription-init --port "${port:-0}" --listen "$listen" --public-base-url "$public" --apply-services || return
       subscription_urls normal; subscription_urls vpngate;;
    2) cfg subscription-stop --apply-services;;
    3) subscription_urls normal; subscription_urls vpngate;;
    4) cfg subscription-reset || return; subscription_urls normal; subscription_urls vpngate;;
    5) subs show;;
    6|7) ui_input file '订阅配置 JSON 文件路径' || return "$UI_CANCEL"
         if [[ "$1" == 6 ]]; then subs preview --file "$file"; else subs apply --file "$file"; fi;;
    8) subs refresh;;
    9) ui_input id '分享 ID（字母、数字、短横线或下划线）' || return "$UI_CANCEL"
       ui_input outputs '输出 ID，逗号分隔，例如 normal-sing-box,normal-provider' || return "$UI_CANCEL"
       ui_input expires '有效时长，例如 720h；回车不限' || return "$UI_CANCEL"
       ui_input count '下载次数上限；回车不限' || return "$UI_CANCEL"
       subs grant --id "$id" --outputs "$outputs" --expires "${expires:-0}" --max-downloads "${count:-0}";;
    10) ui_input id '要撤销的分享 ID' || return "$UI_CANCEL"; subs revoke --id "$id";;
    11) subs logs;;
    12|13) ui_input file '加密备份文件路径' || return "$UI_CANCEL"
           ui_secret password '备份口令，至少 12 字节' || return "$UI_CANCEL"
           if [[ "$1" == 12 ]]; then printf '%s' "$password" | subs backup --file "$file"
           else
               printf '%s' "$password" | subs restore-backup --file "$file" || return
               if ui_confirm '按以上预览恢复配置'; then printf '%s' "$password" | subs restore-backup --file "$file" --apply; fi
           fi;;
    14) subs trash;;
    15) ui_input kind '对象类型：source / collection / template / output / rules' || return "$UI_CANCEL"
        ui_input id '对象 ID；有依赖引用时会拒绝归档' || return "$UI_CANCEL"; subs archive --kind "$kind" --id "$id";;
    16) ui_input id '回收站条目 ID' || return "$UI_CANCEL"; subs restore --id "$id";;
    17) subs clients;;
    18) subs serve;;
    19) ui_input id '要检测的输出 ID，例如 normal-sing-box' || return "$UI_CANCEL"; subs check --id "$id";;
    20) ui_input id '分发目的地 ID（先在 deliveries 中配置）' || return "$UI_CANCEL"; subs deliver --id "$id";;
    esac
}
