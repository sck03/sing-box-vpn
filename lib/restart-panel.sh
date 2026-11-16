#!/usr/bin/env bash
# Runs in a transient systemd unit, independently of the panel being replaced.
set -Eeuo pipefail
stage=$1 app=$2 manager=$3 runtime=$4
shift 4
[[ -d "$stage/previous" && -f "$stage/manager.previous" && -f "$stage/runtime.previous" && -f "$app/.managed" ]]
if systemctl restart sb-panel && sleep 5 && systemctl is-active --quiet sb-panel; then
    echo 'Updated panel is running.'
    exit 0
fi
echo 'Updated panel failed; restoring previous program and runtime.' >&2
systemctl stop sb-panel || true
install -m 0755 "$stage/manager.previous" "$manager.new"
mv -f -- "$manager.new" "$manager"
install -m 0755 "$stage/runtime.previous" "$runtime.new"
mv -f -- "$runtime.new" "$runtime"
mv -- "$app" "$stage/rejected"
mv -- "$stage/previous" "$app"
failed=0
for unit in "$@"; do
    if ! systemctl restart "$unit" || ! systemctl is-active --quiet "$unit"; then failed=1; fi
done
echo "Rollback completed (restart failures: $failed); diagnostics: $stage" >&2
exit 1
