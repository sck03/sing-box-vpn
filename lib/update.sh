#!/usr/bin/env bash
# Program updates never reinstall the core, rewrite configuration or enable units.
upgrade_program() (
    set -Eeuo pipefail
    require_system
    local arch stage manager_backup switched=0 binary_switched=0 unit state
    local -a running=()
    arch=$(arch_name)
    [[ -f "$APP_HOME/.managed" && -f "$SB_ROOT/app.json" && -x "$MANAGER" ]] || {
        echo '未找到完整安装，请使用首次安装入口' >&2; return 1;
    }
    [[ "$SCRIPT_ROOT" != "$APP_HOME" && -x "$SCRIPT_ROOT/.bin/linux-$arch/yg-vpngate" ]] || {
        echo '程序升级需要解压后的完整发行包' >&2; return 1;
    }
    # Query errors must stop the update, not be mistaken for stopped services.
    for unit in yg-vpngate sb-subscription sb-panel; do
        state=$(systemctl show "$unit" --property=ActiveState --value)
        case "$state" in
            active) running+=("$unit");;
            inactive|failed) ;;
            *) echo "无法安全升级，$unit 状态为 $state" >&2; return 1;;
        esac
    done
    stage=$(mktemp -d "${APP_HOME}.update.XXXXXXXX")
    manager_backup="$stage/manager.previous"
    cp -a -- "$MANAGER" "$manager_backup"
    install -d -m 0755 "$stage/program"
    cp -a -- "$SCRIPT_ROOT/." "$stage/program/"
    printf '%s\n' 'sck03/sing-box-vpn' > "$stage/program/.managed"
    "$stage/program/.bin/linux-$arch/yg-vpngate" --version
    rollback_program() {
        local code=$?
        trap - EXIT
        if ((code != 0)); then
            if ((binary_switched)); then
                install -m 0755 "$manager_backup" "$MANAGER.new"
                mv -f -- "$MANAGER.new" "$MANAGER"
            fi
            if ((switched)); then
                if [[ -e "$APP_HOME" ]]; then mv -- "$APP_HOME" "$stage/rejected"; fi
                mv -- "$stage/previous" "$APP_HOME"
            fi
            for unit in "${running[@]}"; do systemctl restart "$unit" || true; done
            echo "程序升级失败，恢复记录保存在 $stage" >&2
        fi
        exit "$code"
    }
    trap rollback_program EXIT
    mv -- "$APP_HOME" "$stage/previous"
    switched=1
    mv -- "$stage/program" "$APP_HOME"
    install -m 0755 "$APP_HOME/.bin/linux-$arch/yg-vpngate" "$MANAGER.new"
    binary_switched=1
    mv -f -- "$MANAGER.new" "$MANAGER"
    for unit in "${running[@]}"; do
        systemctl restart "$unit"
        systemctl is-active --quiet "$unit"
    done
    trap - EXIT
    echo "程序已更新；核心、配置和服务启用状态保持。上一程序保存在 $stage"
)
