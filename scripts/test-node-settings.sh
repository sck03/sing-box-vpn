#!/usr/bin/env bash
# Exercise the actual interactive forms with a fake manager; no system changes.
set -Eeuo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
source ./sb.sh
test_dir=$(mktemp -d)
trap 'rm -rf -- "$test_dir"' EXIT
test_uuid=01234567-89ab-4cde-8f01-23456789abcd
test_password='  密码 @:/?#%  '
cfg() {
    printf '%s\n' "$@" > "$test_dir/args"
    if [[ "$1" == credentials ]]; then cat > "$test_dir/secret"; fi
    return "${CFG_STATUS:-0}"
}
reload_scope() { printf '%s\n' "$1" > "$test_dir/reload"; }
firewall() { printf applied > "$test_dir/firewall"; }
show_status() { printf '%s\n' "$1" > "$test_dir/status"; }
reset_calls() { rm -f -- "$test_dir/args" "$test_dir/secret" "$test_dir/reload" "$test_dir/firewall"; }
check_credentials() {
    cmp "$test_dir/args" <(printf '%s\n' credentials "$1" --stdin --scope "$2")
    cmp "$test_dir/secret" <(printf '%s' "$3")
    cmp "$test_dir/reload" <(printf '%s\n' "$2")
}
for scope in normal vpngate; do
    index=0
    for protocol in vless vmess hysteria2 tuic anytls all; do
        index=$((index + 1))
        value=$test_uuid
        if [[ "$protocol" == hysteria2 || "$protocol" == anytls ]]; then value=$test_password; fi
        input=$(printf '%s\n%s' "$index" "$value")
        expected="$value"$'\n'
        if [[ "$protocol" == tuic || "$protocol" == all ]]; then
            input+=$'\n'"$test_password"
            expected+="$test_password"$'\n'
        fi
        reset_calls
        ui_run credentials_action 1 "$scope" <<< "$input" > "$test_dir/output"
        [[ "$UI_RESULT" == 0 ]]
        check_credentials "$protocol" "$scope" "$expected"
    done
    for input in $'4\n\n'"$test_password" $'4\n'"$test_uuid"$'\n'; do
        reset_calls
        ui_run credentials_action 1 "$scope" <<< "$input" > "$test_dir/output"
        [[ "$UI_RESULT" == 0 ]]
        check_credentials tuic "$scope" "${input#*$'\n'}"$'\n'
    done
    for input in $'1\n' $'3\n' $'4\n\n' $'6\n' $'4\n0' $'0'; do
        reset_calls
        ui_run credentials_action 1 "$scope" <<< "$input" > "$test_dir/output"
        [[ "$UI_RESULT" == "$UI_CANCEL" && ! -e "$test_dir/args" && ! -e "$test_dir/reload" ]]
    done
    reset_calls
    ui_run credentials_action 2 "$scope" <<< 6 > "$test_dir/output"
    cmp "$test_dir/args" <(printf '%s\n' reset all --scope "$scope")
    [[ ! -e "$test_dir/secret" && -e "$test_dir/reload" ]]

    reset_calls
    port_menu "$scope" <<< $'4\n31000:31100 31200\n\n0' > "$test_dir/output"
    cmp "$test_dir/args" <(printf '%s\n' multiport tuic 31000:31100 31200 --scope "$scope")
    [[ -e "$test_dir/firewall" && ! -e "$test_dir/reload" ]]
    reset_calls
    ui_run port_action 5 "$scope" > "$test_dir/output"
    cmp "$test_dir/args" <(printf '%s\n' multiport tuic --scope "$scope")
done

# Navigate from the ordinary settings menu to the real credential form.
reset_calls
settings_menu <<< $'2\n1\n4\n\n'"$test_password"$'\n\n0\n0' > "$test_dir/output"
check_credentials tuic normal $'\n'"$test_password"$'\n'

# A rejected config must stop the action before restarting or applying firewall.
CFG_STATUS=37
reset_calls
ui_run credentials_action 1 normal <<< $'1\ninvalid-uuid' > "$test_dir/output"
[[ "$UI_RESULT" == 37 && ! -e "$test_dir/reload" ]]
reset_calls
ui_run port_action 4 normal <<< 'invalid-range' > "$test_dir/output"
[[ "$UI_RESULT" == 37 && ! -e "$test_dir/firewall" ]]
printf 'Node settings menu checks passed.\n'
