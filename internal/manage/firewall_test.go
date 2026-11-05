package manage

import (
	"context"
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
	var calls, batches []string
	f := firewall{execute: func(_ context.Context, binary string, args ...string) ([]byte, error) {
		command := binary + " " + strings.Join(args, " ")
		calls = append(calls, command)
		if strings.Contains(command, " -C ") || strings.Contains(command, "-S "+legacyUDPChain) {
			return nil, firewallExit(1)
		}
		return nil, nil
	}, restore: func(_ context.Context, binary, rules string) ([]byte, error) {
		batches = append(batches, binary+"\n"+rules)
		return nil, nil
	}}
	if err := f.sync(context.Background(), root, false); err != nil {
		t.Fatal(err)
	}
	if len(batches) != 2 {
		t.Fatal("did not update both address families in batches")
	}
	joined := strings.Join(batches, "\n")
	for _, required := range []string{"-p tcp --dport 20001", "-p udp --dport 20002", "--dst-type LOCAL --dport 31000:31100 -j REDIRECT --to-ports 20002"} {
		if !strings.Contains(joined, required) {
			t.Fatal("missing rule", required)
		}
	}
	for _, command := range calls {
		if strings.Contains(command, " -F ") || strings.Contains(command, " -A ") || strings.Contains(command, " -D ") {
			t.Fatal("a live project chain was changed outside a batch", command)
		}
	}
}

func TestFirewallForwardsOnlyTheActiveCountry(t *testing.T) {
	profile := func(base int, ports []string) Profile {
		p := Profile{Ports: map[string]int{}, TUICPorts: ports}
		for i, kind := range Protocols {
			p.Ports[kind] = base + i
		}
		return p
	}
	app := App{Profile: profile(20000, []string{"31000:31002", "31001", "31010"})}
	app.Inbounds = MakeInbounds(app.Profile, "")
	settings := VPNSettings{ActiveCountry: "JP", Profiles: map[string]Profile{"JP": profile(22000, []string{"32000"}), "KR": profile(24000, []string{"33000"})}}
	gateway := Object{"inbounds": MakeInbounds(settings.Profiles["JP"], "vg-JP-")}
	_, udp, err := firewallRules(app, settings, gateway, subscriptionSettings{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(udp, "--dport 31000:31002") != 1 || !strings.Contains(udp, "--dport 32000 -j REDIRECT --to-ports 22003") || strings.Contains(udp, "33000") {
		t.Fatal("duplicate ranges or inactive country rules were installed", udp)
	}
}

func TestInvalidFirewallConfigDoesNotChangeAnyRule(t *testing.T) {
	root := t.TempDir()
	if err := AtomicJSON(filepath.Join(root, "app.json"), App{Inbounds: []Object{{"type": "tuic", "listen_port": 0}}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "firewall.owner"), []byte("sck03/sing-box-vpn\n"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	f := firewall{execute: func(context.Context, string, ...string) ([]byte, error) { calls++; return nil, nil },
		restore: func(context.Context, string, string) ([]byte, error) { calls++; return nil, nil }}
	if err := f.sync(context.Background(), root, false); err == nil || calls != 0 {
		t.Fatal("invalid configuration touched firewall state", err, calls)
	}
}

func TestFirewallRejectsStaleForwardingTarget(t *testing.T) {
	app := App{Profile: Profile{Ports: map[string]int{"tuic": 20003}, TUICPorts: []string{"31000"}},
		Inbounds: []Object{{"type": "tuic", "listen_port": 20004}}}
	if _, _, err := firewallRules(app, VPNSettings{}, nil, subscriptionSettings{}); err == nil {
		t.Fatal("forwarding targeted a port without its configured listener")
	}
}

func TestFirewallRemovalDoesNotReadDamagedApplication(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "firewall.owner"), []byte("sck03/sing-box-vpn\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.json"), []byte("{damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	f := firewall{execute: func(context.Context, string, ...string) ([]byte, error) { return nil, firewallExit(1) }}
	if err := f.sync(context.Background(), root, true); err != nil {
		t.Fatal("damaged application blocked cleanup of owned firewall rules", err)
	}
}
