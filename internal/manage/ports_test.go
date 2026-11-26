package manage

import (
	"net"

	"testing"
)

func TestPortRangeNormalization(t *testing.T) {
	ranges, err := parsePortRanges([]string{"31005", "31000:31003", "31003:31004", "031005", "65535"})
	if err != nil || len(ranges) != 2 || ranges[0].String() != "31000:31005" || ranges[1].String() != "65535" {
		t.Fatal("overlapping ranges were not merged", ranges, err)
	}
	for _, value := range []string{"0", "65536", "2:1", "1:65536", "1:2:3", "-1", "1-2", "1 2", ""} {
		if _, err := parsePortRanges([]string{value}); err == nil {
			t.Fatal("invalid range accepted", value)
		}
	}
	full, err := parsePortRanges([]string{"1:65535"})
	if err != nil || len(full) != 1 || !portInRanges(65535, full) || portInRanges(0, full) {
		t.Fatal("full range was expanded or its boundaries changed")
	}
}

func TestPortProbeRejectsLoopbackAndIPv6Listeners(t *testing.T) {
	for _, probe := range []struct{ network, address string }{{"tcp4", "127.0.0.1:0"}, {"tcp6", "[::1]:0"}} {
		t.Run(probe.network, func(t *testing.T) {
			listener, err := net.Listen(probe.network, probe.address)
			if err != nil && probe.network == "tcp6" {
				t.Skip("IPv6 is unavailable", err)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			if err := probePort(listener.Addr().(*net.TCPAddr).Port); err == nil {
				t.Fatal("port allocator missed an active listener", probe.network)
			}
		})
	}
}

func TestAllocationAndSubscriptionRespectForwardRanges(t *testing.T) {
	app := App{Nodes: []ManagedNode{{ID: "tuic", Protocol: "tuic", Port: 60000, UDPPorts: []string{"20000:59998"}}}}
	ranges, err := usedPortRanges(app, VPNSettings{})
	if err != nil {
		t.Fatal(err)
	}
	if err := (PlatformConfig{Schema: PlatformSchema, App: app, VPN: VPNSettings{ProxyPort: 7928}, Listener: subscriptionSettings{Enabled: true, Listen: "127.0.0.1", Port: 31000}}).Validate(); err == nil {
		t.Fatal("subscription took a forwarding port")
	}
	used := map[int]bool{}
	for port := 20000; port < 59999; port++ {
		if portInRanges(port, ranges) != true {
			t.Fatal("a port inside the forwarding range was not reserved")
		}
	}
	// There are no available choices when both the whole random allocation
	// range and its last port are reserved; the map remains compact.
	used[59999] = true
	if _, err := AvailablePort(used, ranges...); err == nil || len(used) != 1 {
		t.Fatal("allocator expanded or ignored forwarding ranges")
	}
}

func TestAvailablePortFindsOnlyUnreservedPort(t *testing.T) {
	port, err := AvailablePort(map[int]bool{})
	if err != nil {
		t.Fatal(err)
	}
	ranges := []portRange{{20000, port - 1}, {port + 1, 59999}}
	for attempt := 0; attempt < 3; attempt++ {
		used := map[int]bool{}
		got, err := AvailablePort(used, ranges...)
		if err != nil || got != port || len(used) != 1 || !used[port] {
			t.Fatalf("only available port %d was missed: port=%d, used=%v, err=%v", port, got, used, err)
		}
	}
}

func BenchmarkPortRangeNormalization(b *testing.B) {
	values := []string{"1:65535", "31000:32000", "65535"}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := parsePortRanges(values); err != nil {
			b.Fatal(err)
		}
	}
}
