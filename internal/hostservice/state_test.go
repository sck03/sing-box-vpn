package hostservice

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTunnelRetainsDomainUntilInvocationChanges(t *testing.T) {
	id, active, logs := strings.Repeat("a", 32), "active", "https://example-tunnel.trycloudflare.com"
	reads := 0
	run := func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if name == "systemctl" {
			return []byte("InvocationID=" + id + "\nActiveState=" + active), nil
		}
		reads++
		return []byte(logs), nil
	}
	first, err := ObserveTunnel(t.Context(), run, Tunnel{})
	if err != nil || first.Domain != "example-tunnel.trycloudflare.com" {
		t.Fatal(first, err)
	}
	logs = strings.Repeat("connection retry\n", 300)
	next, err := ObserveTunnel(t.Context(), run, first)
	if err != nil || next != first || reads != 1 {
		t.Fatal("log rotation invalidated a live domain", next, reads, err)
	}
	id = strings.Repeat("b", 32)
	next, err = ObserveTunnel(t.Context(), run, next)
	if err != nil || next.Domain != "" || next.Invocation != id {
		t.Fatal("restarted tunnel reused old domain", next, err)
	}
	logs = "https://new-tunnel.trycloudflare.com"
	next, err = ObserveTunnel(t.Context(), run, next)
	if err != nil || next.Domain != "new-tunnel.trycloudflare.com" {
		t.Fatal(next, err)
	}
	active = "inactive"
	next, err = ObserveTunnel(t.Context(), run, next)
	if err != nil || next != (Tunnel{}) {
		t.Fatal(next, err)
	}
}

func TestRestoreNetworkRetriesPartialFailureAndRemovedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sysctl.conf")
	if err := os.WriteFile(path, []byte("# sing-box-vpn\n"), 0600); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"net.core.default_qdisc": "fq_codel", "net.ipv4.tcp_congestion_control": "cubic"}
	calls := 0
	run := func(context.Context, string, ...string) ([]byte, error) {
		calls++
		if calls == 2 {
			return nil, errors.New("temporary sysctl failure")
		}
		return nil, nil
	}
	if err := RestoreNetwork(t.Context(), run, path, values); err == nil {
		t.Fatal("injected failure lost")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("recovery file removed before success", err)
	}
	for range 2 {
		if err := RestoreNetwork(t.Context(), run, path, values); err != nil {
			t.Fatal("retry failed", err)
		}
	}
	if calls != 6 {
		t.Fatal(calls)
	}
	if err := os.WriteFile(path, []byte("unrelated configuration"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RestoreNetwork(t.Context(), run, path, values); err == nil || calls != 6 {
		t.Fatal("unowned file modified", err)
	}
	delete(values, "net.ipv4.tcp_congestion_control")
	if err := RestoreNetwork(t.Context(), run, path, values); err == nil || calls != 6 {
		t.Fatal("invalid restore performed a write", err)
	}
}
