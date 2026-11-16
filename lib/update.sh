#!/usr/bin/env bash
# Swap verified v4 manager/runtime together, preserve stopped and disabled units.
upgrade_program() (
    set -Eeuo pipefail
    require_system
    local arch stage unit state switched=0 binaries=0
    local -a running=()
    arch=$(arch_name)
    [[ -f "$APP_HOME/.managed" && -f "$SB_ROOT/.platform-v4" && -x "$MANAGER" && -x "$RUNTIME" ]] || {
        echo '升级要求完整 v4 安装，不支持旧版本迁移' >&2; return 1;
    }
    exec 9>"$SB_ROOT/.upgrade.lock"
    flock -n 9 || { echo '另一个升级任务正在运行' >&2; return 1; }
    [[ $(systemctl show sb-panel-upgrade-restart.timer --property=ActiveState --value) != active ]] || {
        echo '上一升级仍在等待面板重启' >&2; return 1;
    }
    [[ "$SCRIPT_ROOT" != "$APP_HOME" && -x "$SCRIPT_ROOT/.bin/linux-$arch/yg-vpngate" && -x "$SCRIPT_ROOT/.bin/linux-$arch/managed-core" ]] || {
        echo '升级需要解压后的完整 v4 发行包' >&2; return 1;
    }
    for unit in yg-vpngate sing-box sing-box-vpngate sb-subscription sb-argo sb-panel; do
        state=$(systemctl show "$unit" --property=ActiveState --value)
        case "$state" in active) running+=("$unit");; inactive|failed) ;; *) echo "$unit 正在切换状态" >&2; return 1;; esac
    done
    stage=$(mktemp -d "${APP_HOME}.update.XXXXXXXX")
    cp -a -- "$MANAGER" "$stage/manager.previous"
    cp -a -- "$RUNTIME" "$stage/runtime.previous"
    install -d -m 0755 "$stage/program"
    cp -a -- "$SCRIPT_ROOT/." "$stage/program/"
    printf '%s\n' 'sck03/sing-box-vpn v4' > "$stage/program/.managed"
    local candidate="$stage/program/.bin/linux-$arch"
    "$candidate/yg-vpngate" --version
    "$candidate/managed-core" --version
    "$candidate/yg-vpngate" panel --root "$SB_ROOT" --core "$CORE" --runtime "$candidate/managed-core" validate
    rollback_program() {
        local code=$?
        trap - EXIT
        if ((code != 0)); then
            if ((binaries)); then
                install -m 0755 "$stage/manager.previous" "$MANAGER.new"
                mv -f -- "$MANAGER.new" "$MANAGER"
                install -m 0755 "$stage/runtime.previous" "$RUNTIME.new"
                mv -f -- "$RUNTIME.new" "$RUNTIME"
            fi
            if ((switched)); then
                if [[ -e "$APP_HOME" ]]; then mv -- "$APP_HOME" "$stage/rejected"; fi
                mv -- "$stage/previous" "$APP_HOME"
            fi
            for unit in "${running[@]}"; do
                if [[ "$unit" == sb-panel && "${SB_DEFER_PANEL_RESTART:-0}" == 1 ]]; then continue; fi
                systemctl restart "$unit" || true
            done
            echo "升级失败，旧二进制已恢复；诊断目录：$stage" >&2
        fi
        exit "$code"
    }
    trap rollback_program EXIT
    # Preserve the helper outside the directory that it may roll back.
    install -m 0755 "$SCRIPT_ROOT/lib/restart-panel.sh" "$stage/restart-panel.sh"
    mv -- "$APP_HOME" "$stage/previous"
    switched=1
    mv -- "$stage/program" "$APP_HOME"
    binaries=1
    install -m 0755 "$APP_HOME/.bin/linux-$arch/yg-vpngate" "$MANAGER.new"
    mv -f -- "$MANAGER.new" "$MANAGER"
    install -m 0755 "$APP_HOME/.bin/linux-$arch/managed-core" "$RUNTIME.new"
    mv -f -- "$RUNTIME.new" "$RUNTIME"
    for unit in "${running[@]}"; do
        if [[ "$unit" == sb-panel && "${SB_DEFER_PANEL_RESTART:-0}" == 1 ]]; then continue; fi
        systemctl restart "$unit"
        systemctl is-active --quiet "$unit"
    done
    install -m 0755 "$stage/runtime.previous" "$RUNTIME.previous"
    if [[ "${SB_DEFER_PANEL_RESTART:-0}" == 1 && " ${running[*]} " == *' sb-panel '* ]]; then
        systemd-run --unit=sb-panel-upgrade-restart --on-active=10s --collect /bin/bash "$stage/restart-panel.sh" "$stage" "$APP_HOME" "$MANAGER" "$RUNTIME" "${running[@]}"
    fi
    trap - EXIT
    echo "程序和受管核心已更新；上一发行包保存在 $stage，账户账本和服务启用状态保留。"
)
