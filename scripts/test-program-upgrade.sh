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
    export RUNTIME="$SB_ROOT/managed-core" CORE="$SB_ROOT/sing-box"
    export TEST_LOG="$base/systemctl.log" TEST_SCENARIO="$scenario"
    mkdir -p "$APP_HOME" "$SB_ROOT" "$SCRIPT_ROOT/.bin/linux-amd64"
    mkdir -p "$SCRIPT_ROOT/lib"
    cp "$ROOT/lib/restart-panel.sh" "$SCRIPT_ROOT/lib/restart-panel.sh"
    touch "$APP_HOME/.managed" "$SB_ROOT/.platform-v4" "$TEST_LOG"
    printf 'old\n' > "$APP_HOME/version"
    printf '#!/bin/sh\necho old\n' > "$MANAGER"
    printf 'new\n' > "$SCRIPT_ROOT/version"
    printf '#!/bin/sh\necho new\n' > "$SCRIPT_ROOT/.bin/linux-amd64/yg-vpngate"
    cp "$MANAGER" "$RUNTIME"
    cp "$SCRIPT_ROOT/.bin/linux-amd64/yg-vpngate" "$SCRIPT_ROOT/.bin/linux-amd64/managed-core"
    chmod +x "$RUNTIME" "$SCRIPT_ROOT/.bin/linux-amd64/managed-core"
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

for scenario in panel-ok panel-fail; do
    base="$SCRATCH/$scenario"
    mkdir -p "$base/stage/previous" "$base/app" "$base/bin"
    touch "$base/app/.managed"
    printf 'old\n' > "$base/stage/previous/version"
    printf 'new\n' > "$base/app/version"
    printf '#!/bin/sh\necho old\n' > "$base/stage/manager.previous"
    cp "$base/stage/manager.previous" "$base/stage/runtime.previous"
    printf '#!/bin/sh\necho new\n' > "$base/manager"
    cp "$base/manager" "$base/runtime"
    chmod +x "$base/manager" "$base/runtime"
    cat > "$base/bin/systemctl" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$PANEL_TEST_BASE/log"
if [[ "$PANEL_TEST_SCENARIO" == panel-fail && "$1" == restart && $("$PANEL_TEST_BASE/manager") == new ]]; then exit 1; fi
MOCK
    printf '#!/bin/sh\nexit 0\n' > "$base/bin/sleep"
    chmod +x "$base/bin/systemctl" "$base/bin/sleep"
    set +e
    env PATH="$base/bin:$PATH" PANEL_TEST_BASE="$base" PANEL_TEST_SCENARIO="$scenario" \
        bash "$ROOT/lib/restart-panel.sh" "$base/stage" "$base/app" "$base/manager" "$base/runtime" sing-box sb-panel > "$base/result" 2>&1
    result=$?
    set -e
    if [[ "$scenario" == panel-fail ]]; then
        [[ "$result" == 1 && $("$base/manager") == old && $("$base/runtime") == old && $(<"$base/app/version") == old ]]
        grep -q '^restart sing-box$' "$base/log"
    else
        [[ "$result" == 0 && $("$base/manager") == new && $(<"$base/app/version") == new ]]
        ! grep -q '^restart sing-box$' "$base/log"
    fi
done
echo 'Deferred panel restart: success and failed restart rollback passed.'
