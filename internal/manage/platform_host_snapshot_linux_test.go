package manage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestServiceSnapshotOneProcess(t *testing.T) {
	bin := t.TempDir()
	program := `#!/bin/sh
set -eu
echo show >> "$SB_CALLS"
[ "$1" = show ] && [ "$2" = --all ]
shift 3
for unit do
  printf 'Id=%s.service\nLoadState=loaded\nActiveState=active\nUnitFileState=enabled\n\n' "$unit"
done
`
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte(program), 0700); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(bin, "calls")
	t.Setenv("SB_CALLS", calls)
	t.Setenv("PATH", bin)
	active, enabled, err := serviceSnapshot(t.Context(), true)
	if err != nil || len(active) != len(platformServices) || len(enabled) != len(active) {
		t.Fatal(active, enabled, err)
	}
	data, err := os.ReadFile(calls)
	if err != nil || string(data) != "show\n" {
		t.Fatal(string(data), err)
	}
}
