#!/usr/bin/env bash
set -Eeuo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
source "$ROOT/sb.sh"
for scenario in ready core-down panel-down heartbeat-failed panel-unready; do
    systemctl() {
        [[ "$*" == 'is-active --quiet sb-core' || "$*" == 'is-active --quiet sb-panel' ]] || return 99
        [[ "$scenario:$3" != core-down:sb-core && "$scenario:$3" != panel-down:sb-panel ]]
    }
    panel() {
        if [[ "$1" == wait-runtime ]]; then
            local payload
            IFS= read -r payload
            [[ "$payload" == '{"instance":"normal","started":123}' && "$scenario" != heartbeat-failed ]]
        elif [[ "$1" == wait-panel ]]; then [[ "$scenario" != panel-unready ]]; else return 99; fi
    }
    if verify_installation 123; then
        [[ "$scenario" == ready ]] || { echo "Installation accepted unhealthy scenario: $scenario" >&2; exit 1; }
    else
        [[ "$scenario" != ready ]] || { echo 'Healthy installation rejected' >&2; exit 1; }
    fi
done
echo 'Installation health: each service, fresh runtime heartbeat and panel readiness required.'
