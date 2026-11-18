#!/usr/bin/env bash
# Shared by failed installation and uninstall; never act on another program's unit.
owned_unit() {
    local file="${SB_UNIT_DIR:-/etc/systemd/system}/$1" first
    [[ -f "$file" && ! -L "$file" ]] || return 1
    IFS= read -r first < "$file" || return 1
    [[ "$first" == '# Managed by sing-box-vpn v4' ]]
}
stop_owned_units() {
    local unit
    for unit in "$@"; do
        if owned_unit "$unit"; then systemctl disable --now "$unit" || true; fi
    done
}
remove_owned_units() {
    local unit
    for unit in "$@"; do
        if owned_unit "$unit"; then rm -f -- "${SB_UNIT_DIR:-/etc/systemd/system}/$unit"; fi
    done
}
