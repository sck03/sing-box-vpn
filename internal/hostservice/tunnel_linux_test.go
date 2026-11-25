//go:build linux

package hostservice

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/servicecatalog"
)

// A disposable transient unit exercises real journald namespace isolation,
// without Cloudflare access or replacing the installed tunnel service.
func TestLinuxTunnelJournalNamespace(t *testing.T) {
	if os.Getenv("RUN_SERVICE_TESTS") != "1" {
		t.Skip("set RUN_SERVICE_TESTS=1 on disposable Linux systemd host")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	unit := "sb-vpn-tunnel-observation-test"
	if out, err := exec.CommandContext(ctx, "systemd-run", "--collect", "--description=Isolated tunnel journal acceptance", "--unit="+unit, "--property=LogNamespace="+servicecatalog.Journal, "--property=Type=exec", "/bin/sh", "-c", "echo https://isolated-journal.trycloudflare.com; exec sleep 60").CombinedOutput(); err != nil {
		t.Fatalf("start transient unit: %v %s", err, out)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_ = exec.CommandContext(cleanup, "systemctl", "stop", unit).Run()
	}()
	command := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "systemctl" {
			args = append([]string(nil), args...)
			for i, arg := range args {
				if arg == servicecatalog.Argo {
					args[i] = unit
				}
			}
		}
		return exec.CommandContext(ctx, name, args...).CombinedOutput()
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		observed, err := ObserveTunnel(ctx, command, Tunnel{})
		if err == nil && observed.Domain == "isolated-journal.trycloudflare.com" {
			out, err := exec.CommandContext(ctx, "journalctl", "_SYSTEMD_INVOCATION_ID="+observed.Invocation, "--no-pager", "-o", "cat").CombinedOutput()
			if err != nil || strings.Contains(string(out), observed.Domain) {
				t.Fatalf("namespace leaked into default journal: %v %s", err, out)
			}
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("namespaced tunnel domain not observed: %v %v", ctx.Err(), err)
		case <-ticker.C:
		}
	}
}
