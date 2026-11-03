package manage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirewallRejectsUnownedChainsWithoutChangingThem(t *testing.T) {
	var calls []string
	f := firewall{execute: func(_ context.Context, binary string, args ...string) ([]byte, error) {
		calls = append(calls, binary+" "+strings.Join(args, " "))
		return nil, nil
	}}
	if err := f.sync(context.Background(), t.TempDir(), false); err == nil {
		t.Fatal("unowned firewall chain accepted")
	}
	for _, call := range calls {
		if strings.Contains(call, " -F ") || strings.Contains(call, " -D ") || strings.Contains(call, " -A ") {
			t.Fatal("changed an unowned chain", call)
		}
	}
}
func TestFirewallUsesProtocolPortsAndSeparateHoppingChain(t *testing.T) {
	root := t.TempDir()
	app := App{Inbounds: []Object{{"type": "vmess", "listen_port": 20001}, {"type": "hysteria2", "listen_port": 20002}}, Profile: Profile{Ports: map[string]int{"hysteria2": 20002}, Hopping: []string{"31000:31100"}}}
	if err := AtomicJSON(filepath.Join(root, "app.json"), app); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "firewall.owner"), []byte("sck03/sing-box-vpn\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var calls []string
	f := firewall{execute: func(_ context.Context, binary string, args ...string) ([]byte, error) {
		command := binary + " " + strings.Join(args, " ")
		calls = append(calls, command)
		if strings.Contains(command, " -C ") {
			return nil, errors.New("absent")
		}
		return nil, nil
	}}
	if err := f.sync(context.Background(), root, false); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, "\n")
	for _, required := range []string{"-p tcp --dport 20001", "-p udp --dport 20002", "SBVPN_HY2 -p udp --dport 31000:31100 -j REDIRECT --to-ports 20002"} {
		if !strings.Contains(joined, required) {
			t.Fatal("missing rule", required)
		}
	}
}
