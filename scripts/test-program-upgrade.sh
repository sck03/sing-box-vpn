#!/usr/bin/env bash
set -Eeuo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
source "$ROOT/lib/update.sh"
SCRATCH=$(mktemp -d)
trap 'rm -rf -- "$SCRATCH"' EXIT
require_system() { :; }
arch_name() { echo amd64; }
for scenario in stopped active fail; do
    base="$SCRATCH/$scenario"
    export APP_HOME="$base/app" SB_ROOT="$base/config" MANAGER="$base/manager" SCRIPT_ROOT="$base/release"
    export TEST_LOG="$base/systemctl.log" TEST_SCENARIO="$scenario"
    mkdir -p "$APP_HOME" "$SB_ROOT" "$SCRIPT_ROOT/.bin/linux-amd64"
    touch "$APP_HOME/.managed" "$SB_ROOT/app.json" "$TEST_LOG"
    printf 'old\n' > "$APP_HOME/version"
    printf '#!/bin/sh\necho old\n' > "$MANAGER"
    printf 'new\n' > "$SCRIPT_ROOT/version"
    printf '#!/bin/sh\necho new\n' > "$SCRIPT_ROOT/.bin/linux-amd64/yg-vpngate"
    chmod +x "$MANAGER" "$SCRIPT_ROOT/.bin/linux-amd64/yg-vpngate"
    systemctl() {
        printf '%s\n' "$*" >> "$TEST_LOG"
        case "$1" in
            show) if [[ "$2" == sb-subscription && "$TEST_SCENARIO" != stopped ]]; then echo active; else echo inactive; fi;;
            restart) if [[ "$TEST_SCENARIO" == fail && $("$MANAGER") == new ]]; then return 1; fi;;
            is-active) return 0;;
            *) echo 'unexpected systemctl operation' >&2; return 1;;
        esac
    }
    if [[ "$scenario" == fail ]]; then
        set +e
        (set -e; upgrade_program) > "$base/result" 2>&1
        result=$?
        set -e
        [[ "$result" != 0 && $("$MANAGER") == old && $(<"$APP_HOME/version") == old ]]
    else
        upgrade_program > "$base/result"
        [[ $("$MANAGER") == new && $(<"$APP_HOME/version") == new ]]
    fi
    if [[ "$scenario" == stopped ]]; then
        ! grep -q '^restart ' "$TEST_LOG"
    fi
    ! grep -Eq '^(enable|disable|start)|^restart (sing-box|yg-vpngate)' "$TEST_LOG"
done
echo 'Program upgrade: stopped, active and rollback scenarios passed.'
