package manage

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/command"
)

func TestSystemServicesActiveDoesNotHideTimeout(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte("#!/bin/sh\nexec /bin/sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := (systemServices{}).active(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("service query timeout treated as an inactive service: %v", err)
	}
}

func TestSystemServicesActiveRejectsQueryFailures(t *testing.T) {
	for _, tc := range []struct {
		name, script string
		code         int
		overflow     bool
	}{
		{"bus", "echo 'Failed to connect to bus' >&2\nexit 1", 1, false},
		{"permission", "echo 'Access denied' >&2\nexit 2", 2, false},
		{"unexpected", "exit 5", 5, false},
		{"inactive-with-diagnostic", "echo 'query failed' >&2\nexit 3", 3, false},
		{"overflow", "/usr/bin/head -c 1048577 /dev/zero\nexit 3", 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := t.TempDir()
			if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte("#!/bin/sh\n"+tc.script+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin)
			active, err := (systemServices{}).active(t.Context())
			var exit *exec.ExitError
			if active != nil || !errors.As(err, &exit) || exit.ExitCode() != tc.code {
				t.Fatalf("query failure treated as inactivity: active=%v err=%v", active, err)
			}
			if tc.overflow && !errors.Is(err, command.ErrOutputLimit) {
				t.Fatal("output limit error lost")
			}
			if !strings.Contains(err.Error(), "sing-box") {
				t.Fatal("query error omitted the service name")
			}
		})
	}
}

func TestSystemServicesActiveRecognizesUnitStates(t *testing.T) {
	for _, code := range []string{"3", "4"} {
		t.Run(code, func(t *testing.T) {
			bin := t.TempDir()
			script := "#!/bin/sh\ncase \"$3\" in\nsing-box) exit 0;;\nsing-box-vpngate) exit " + code + ";;\nesac\nexit 1\n"
			if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin)
			active, err := (systemServices{}).active(t.Context())
			if err != nil || !reflect.DeepEqual(active, []string{"sing-box"}) {
				t.Fatalf("incorrect restart plan: %v, %v", active, err)
			}
		})
	}
}

func TestCoreQueryFailurePreservesBinaries(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte("#!/bin/sh\necho 'Failed to connect to bus' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	root, candidate, updater, _ := updaterFixture(t)
	updater.services = systemServices{}
	previous := filepath.Join(root, "sing-box.previous")
	if err := os.WriteFile(previous, []byte("saved previous"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := updater.activate(t.Context(), root, candidate, true); err == nil || !strings.Contains(err.Error(), "Failed to connect to bus") {
		t.Fatalf("activation did not preserve the query failure: %v", err)
	}
	for path, want := range map[string]string{filepath.Join(root, "sing-box"): "old", previous: "saved previous", candidate: "new"} {
		if got := string(readBytes(t, path)); got != want {
			t.Fatalf("query failure altered %s: %q", filepath.Base(path), got)
		}
	}
	staged, err := filepath.Glob(filepath.Join(root, ".core-activate-*"))
	if err != nil || len(staged) != 0 {
		t.Fatalf("query failure retained staging files: %v, %v", staged, err)
	}
}
