package manage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type firewallExit int

func (e firewallExit) Error() string { return fmt.Sprintf("firewall exit %d", int(e)) }
func (e firewallExit) ExitCode() int { return int(e) }

func TestFirewallQueryDistinguishesMissingRuleFromFailure(t *testing.T) {
	for _, code := range []firewallExit{1, 2, 3, 4} {
		f := firewall{execute: func(context.Context, string, ...string) ([]byte, error) { return nil, code }}
		found, err := f.command(context.Background(), false, "iptables", "-S", inputChain)
		if found || (code == 1 && err != nil) || (code != 1 && err == nil) {
			t.Fatalf("exit %d: found=%v err=%v", code, found, err)
		}
	}
}

func TestFirewallNFTMissingChainQuery(t *testing.T) {
	for _, args := range [][]string{{"-t", "filter", "-S", inputChain}, {"-C", "INPUT", "-j", inputChain}} {
		f := firewall{execute: func(context.Context, string, ...string) ([]byte, error) {
			return []byte("iptables v1.8.10 (nf_tables): Chain 'SBVPN_INPUT' does not exist\n"), firewallExit(2)
		}}
		if found, err := f.command(context.Background(), false, "iptables", args...); found || err != nil {
			t.Fatal("nf_tables missing chain was treated as an invalid query", found, err)
		}
		if _, err := f.command(context.Background(), true, "iptables", args...); err == nil {
			t.Fatal("required operation ignored a missing chain")
		}
	}
	f := firewall{execute: func(context.Context, string, ...string) ([]byte, error) {
		return []byte("iptables v1.8.10 (nf_tables): Chain 'OTHER_CHAIN' does not exist\n"), firewallExit(2)
	}}
	if _, err := f.command(context.Background(), false, "iptables", "-S", inputChain); err == nil {
		t.Fatal("query ignored an unrelated missing-chain error")
	}
}

func TestFirewallFailedCleanupRetainsOwnership(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "firewall.owner")
	if err := os.WriteFile(marker, []byte("sck03/sing-box-vpn\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f := firewall{execute: func(context.Context, string, ...string) ([]byte, error) { return nil, firewallExit(4) }}
	if err := f.sync(context.Background(), root, true); err == nil {
		t.Fatal("failed firewall query reported successful cleanup")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("query failure discarded firewall ownership", err)
	}
}

func TestFirewallIPv6QueryFailureIsReported(t *testing.T) {
	root := t.TempDir()
	if err := AtomicJSON(filepath.Join(root, "app.json"), App{Inbounds: []Object{{"type": "vmess", "listen_port": 20001}}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "firewall.owner"), []byte("sck03/sing-box-vpn\n"), 0600); err != nil {
		t.Fatal(err)
	}
	changes := 0
	f := firewall{execute: func(_ context.Context, binary string, args ...string) ([]byte, error) {
		if binary == "ip6tables" {
			return nil, firewallExit(4)
		}
		if strings.Contains(strings.Join(args, " "), "-S "+legacyUDPChain) {
			return nil, firewallExit(1)
		}
		return nil, nil
	}, restore: func(context.Context, string, string) ([]byte, error) { changes++; return nil, nil }}
	if err := f.sync(context.Background(), root, false); err == nil {
		t.Fatal("IPv6 query failure was silently treated as unsupported IPv6")
	}
	if changes != 0 {
		t.Fatal("IPv4 rules changed before IPv6 preflight completed")
	}
}

func TestFirewallQueryReportsCancellation(t *testing.T) {
	f := firewall{execute: func(context.Context, string, ...string) ([]byte, error) { return nil, context.Canceled }}
	_, err := f.command(context.Background(), false, "iptables", "-S", inputChain)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("firewall query hid cancellation", err)
	}
}

func BenchmarkFirewallForwardingRules(b *testing.B) {
	profile := func(base int, hopping, tuic []string) Profile {
		p := Profile{Ports: map[string]int{}, Hopping: hopping, TUICPorts: tuic}
		for i, kind := range Protocols {
			p.Ports[kind] = base + i
		}
		return p
	}
	app := App{Profile: profile(20000, []string{"21000:21002"}, []string{"21100:21102"})}
	app.Inbounds = MakeInbounds(app.Profile, "")
	settings := VPNSettings{ActiveCountry: "JP", Profiles: map[string]Profile{
		"JP": profile(22000, []string{"23000:23002"}, []string{"23100:23102"}),
	}}
	for i := 0; i < 100; i++ {
		country := fmt.Sprintf("%c%c", 'A'+i/26, 'A'+i%26)
		settings.Profiles[country] = profile(40000+10*i, []string{fmt.Sprint(51000 + 2*i)}, []string{fmt.Sprint(52000 + 2*i)})
	}
	gateway := Object{"inbounds": MakeInbounds(settings.Profiles["JP"], "vg-")}
	b.ReportAllocs()
	for b.Loop() {
		input, udp, err := firewallRules(app, settings, gateway, subscriptionSettings{})
		if err != nil || input == "" || udp == "" {
			b.Fatal("forwarding generation failed", err)
		}
	}
}
