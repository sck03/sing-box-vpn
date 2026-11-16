#!/usr/bin/env bash
# Shared systemd recovery policy; sourced by the installation entry points.
write_unit_file() {
    local target=$1 temporary
    install -d -m 0755 "$(dirname -- "$target")" || return 1
    temporary=$(mktemp "$target.XXXXXXXX") || return 1
    cat > "$temporary" || return 1
    chmod 0644 "$temporary" || return 1
    mv -f -- "$temporary" "$target"
}
write_service_recovery() {
    local root=$1 service unit_dir=${SB_SYSTEMD_DIR:-/etc/systemd/system}
    shift
    for service in "$@"; do
        [[ "$service" =~ ^[A-Za-z0-9_.@-]+$ ]] || return 1
        [[ -f "$unit_dir/$service.service" ]] || continue
        write_unit_file "$unit_dir/$service.service.d/10-sb-vpn-recovery.conf" <<EOF || return 1
[Unit]
Wants=network-online.target
After=network-online.target
StartLimitIntervalSec=300
StartLimitBurst=10
[Service]
Restart=always
RestartSec=10
TimeoutStartSec=120
ExecStartPre=+${SB_MANAGER:-/usr/local/bin/yg-vpngate} firewall --root "$root" --wait
EOF
    done
}
remove_service_recovery() {
    local service unit_dir=${SB_SYSTEMD_DIR:-/etc/systemd/system}
    for service in "$@"; do
        [[ "$service" =~ ^[A-Za-z0-9_.@-]+$ ]] || return 1
        rm -f -- "$unit_dir/$service.service.d/10-sb-vpn-recovery.conf" || return 1
        rmdir -- "$unit_dir/$service.service.d" 2>/dev/null || true
    done
}
