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
StartLimitIntervalSec=0
[Service]
Restart=always
RestartSec=3
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
write_service_unit() {
    local service=$1 description=$2 root=$3 after=${4:-} wants=${5:-} unit_dir=${SB_SYSTEMD_DIR:-/etc/systemd/system}
    [[ "$service" =~ ^[A-Za-z0-9_.@-]+$ ]] || return 1
    {
        printf '[Unit]\nDescription=%s\nAfter=network-online.target %s\nWants=network-online.target %s\n[Service]\nUMask=0077\n' "$description" "$after" "$wants"
        cat
        printf '[Install]\nWantedBy=multi-user.target\n'
    } | write_unit_file "$unit_dir/$service.service" || return 1
    write_service_recovery "$root" "$service"
}
write_main_service() {
    write_service_unit sing-box 'sing-box-vpn ordinary five-protocol service' "$SB_ROOT" <<EOF
ExecStart="$CORE" run -c "$SB_ROOT/sb.json"
ExecReload=/bin/kill -HUP \$MAINPID
WorkingDirectory=$SB_ROOT
LimitNOFILE=65536
EOF
}
write_subscription_service() {
    write_service_unit sb-subscription 'Token protected sing-box-vpn subscriptions' "$SB_ROOT" 'sing-box.service sing-box-vpngate.service' <<EOF
ExecStart=/usr/local/bin/yg-vpngate subscription --root "$SB_ROOT"
WorkingDirectory=$SB_ROOT
ProtectSystem=strict
ReadOnlyPaths=$SB_ROOT
NoNewPrivileges=true
EOF
}
write_gateway_services() {
    write_service_unit yg-vpngate 'VPNGate country-locked gateway' "$SB_ROOT" <<EOF || return 1
User=root
ExecStart=/usr/local/bin/yg-vpngate daemon --config "$SB_ROOT/vpngate/gateway.json"
ExecStopPost=/usr/local/bin/yg-vpngate cleanup --config "$SB_ROOT/vpngate/gateway.json"
TimeoutStopSec=20
NoNewPrivileges=true
LimitNOFILE=32768
EOF
    write_service_unit sing-box-vpngate 'VPNGate five-protocol dedicated entries' "$SB_ROOT" yg-vpngate.service yg-vpngate.service <<EOF
User=root
ExecStart="$SB_ROOT/sing-box" run -c "$SB_ROOT/vpngate/server.json"
ExecReload=/bin/kill -HUP \$MAINPID
WorkingDirectory=$SB_ROOT/vpngate
LimitNOFILE=32768
EOF
}
