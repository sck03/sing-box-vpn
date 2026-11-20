#!/usr/bin/env bash
# Runs in a transient systemd unit, independently of the panel being replaced.
set -Eeuo pipefail
stage=$1 app=$2 manager=$3 runtime=$4 root=$5 task=$6 target=$7
shift 7
[[ "$task" == 0 || ( "$task" =~ ^[1-9][0-9]*$ && "$target" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ) ]]
phase=rollback-failed outcome=failed
record_result() {
    local code=$? actual
    trap - EXIT
    if [[ "$task" != 0 ]]; then
        actual=$("$manager" --version 2>/dev/null) || actual=
        if [[ "$actual" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then actual="v$actual"; else actual=; fi
        umask 077
        printf '{"task_id":%s,"target_version":"%s","actual_version":"%s","phase":"%s","outcome":"%s"}\n' "$task" "$target" "$actual" "$phase" "$outcome" > "$root/program-upgrade.json.new"
        mv -f -- "$root/program-upgrade.json.new" "$root/program-upgrade.json"
        "$manager" panel --root "$root" complete-upgrade || true
    fi
    exit "$code"
}
trap record_result EXIT
exec 8>"$root/.service-recovery.lock"
flock -w 60 8
[[ -d "$stage/previous" && -f "$stage/manager.previous" && -f "$stage/runtime.previous" && -f "$app/.managed" ]]
if systemctl restart sb-panel && "$manager" panel --root "$root" wait-panel && sleep 5 && systemctl is-active --quiet sb-panel && [[ "$task" == 0 || "v$("$manager" --version)" == "$target" ]]; then
    phase=succeeded outcome=passed
    printf '%s\n' 'sing-box-vpn upgrade succeeded' > "$stage/.upgrade-success"
    SB_HOME="$app" "$manager" panel --root "$root" prune-upgrades || echo '旧升级留档清理失败，可稍后重试。' >&2
    echo 'Updated panel is running.'
    exit 0
fi
echo 'Updated panel failed; restoring previous program and runtime.' >&2
systemctl stop sb-panel || true
install -m 0755 "$stage/manager.previous" "$manager.new"
mv -f -- "$manager.new" "$manager"
install -m 0755 "$stage/runtime.previous" "$runtime.new"
mv -f -- "$runtime.new" "$runtime"
install -m 0755 "$stage/validator.previous" "$root/sing-box.new"
mv -f -- "$root/sing-box.new" "$root/sing-box"
mv -- "$app" "$stage/rejected"
mv -- "$stage/previous" "$app"
failed=0
for unit in "$@"; do
    started=$(date +%s%N)
    if ! systemctl restart "$unit" || ! systemctl is-active --quiet "$unit"; then failed=1; continue; fi
    case "$unit" in
        sb-core|sb-vpngate-core)
            instance=normal
            [[ "$unit" != sb-vpngate-core ]] || instance=vpngate
            if ! printf '{"instance":"%s","started":%s}\n' "$instance" "$started" | "$manager" panel --root "$root" wait-runtime; then failed=1; fi
            ;;
    esac
done
if ((failed == 0)) && "$manager" panel --root "$root" wait-panel; then phase=rolled-back; else failed=1; fi
echo "Rollback completed (restart failures: $failed); diagnostics: $stage" >&2
exit 1
