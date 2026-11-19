#!/usr/bin/env bash
set -Eeuo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
source "$ROOT/lib/ownership.sh"
SCRATCH=$(mktemp -d)
trap 'rm -rf -- "$SCRATCH"' EXIT
export SB_UNIT_DIR="$SCRATCH/units"
mkdir -p "$SB_UNIT_DIR"
printf '%s\n' '# Another program' > "$SB_UNIT_DIR/sb-core.service"
printf '%s\n' '# Managed by sing-box-vpn v4' > "$SB_UNIT_DIR/sb-panel.service"
printf '%s\n' '# Managed by sing-box-vpn v4' > "$SCRATCH/external.service"
ln -s "$SCRATCH/external.service" "$SB_UNIT_DIR/sb-recovery.timer"
systemctl() { printf '%s\n' "$*" >> "$SCRATCH/calls"; }
stop_owned_units sb-core.service sb-panel.service sb-recovery.timer missing.service
[[ $(<"$SCRATCH/calls") == 'disable --now sb-panel.service' ]]
remove_owned_units sb-core.service sb-panel.service sb-recovery.timer missing.service
[[ -f "$SB_UNIT_DIR/sb-core.service" && -L "$SB_UNIT_DIR/sb-recovery.timer" && ! -e "$SB_UNIT_DIR/sb-panel.service" ]]
echo 'Installation cleanup preserves foreign and symlinked units.'
