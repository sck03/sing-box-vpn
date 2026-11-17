package hostservice

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestRecoveryPreservesStoppedAndUnownedUnits(t *testing.T) {
	var actions [][]string
	run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if args[0] == "show" {
			state, path := "failed", "/etc/systemd/system/"+args[1]+".service"
			if args[1] == "stopped" {
				state = "inactive"
			}
			if args[1] == "unowned" {
				path = "/usr/lib/systemd/system/unowned.service"
			}
			return []byte(fmt.Sprintf("LoadState=loaded\nActiveState=%s\nFragmentPath=%s\n", state, path)), nil
		}
		actions = append(actions, args)
		return nil, nil
	}
	read := func(path string) ([]byte, error) {
		if strings.HasSuffix(path, "foreign.service") {
			return []byte("# another program"), nil
		}
		return []byte("# Managed by sing-box-vpn v4\n[Unit]\n"), nil
	}
	if err := Recover(t.Context(), run, read, []string{"stopped", "unowned", "foreign", "failed"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actions, [][]string{{"reset-failed", "failed"}, {"start", "--no-block", "failed"}}) {
		t.Fatal(actions)
	}
}
