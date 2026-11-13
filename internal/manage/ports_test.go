package manage

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
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

func TestMultiportPersistsAndProducesScopedTUICLinks(t *testing.T) {
	root, core, app := appFixture(t)
	ctx := context.Background()
	settings, err := PrepareVPN(ctx, root, core, app, "JP")
	if err != nil {
		t.Fatal(err)
	}
	used := usedPorts(app, settings)
	normalPort, err := AvailablePort(used)
	if err != nil {
		t.Fatal(err)
	}
	vpnPort, err := AvailablePort(used)
	if err != nil {
		t.Fatal(err)
	}
	run := func(scope string, args ...string) (string, error) {
		var out bytes.Buffer
		err := RunConfig(ctx, append(append([]string{"--root", root, "--core", core}, args...), "--scope", scope), &out)
		return out.String(), err
	}
	normalServer := readBytes(t, filepath.Join(root, "sb.json"))
	vpnServer := readBytes(t, filepath.Join(root, "vpngate", "server.json"))
	// TUIC forwarding and link selection must work without launching the core.
	// Full main-port changes below still require the real core validator.
	validatedCore := core
	core = filepath.Join(root, "must-not-run-sing-box")
	unchanged := fileBundle{}
	for _, prefix := range []string{"", "vpngate/"} {
		for _, name := range exportNames {
			unchanged[prefix+name] = readBytes(t, filepath.Join(root, prefix, name))
		}
	}
	unchanged["vpngate/secrets.json"] = readBytes(t, filepath.Join(root, "vpngate", "secrets.json"))
	for _, set := range []struct {
		scope string
		port  int
	}{{normalScope, normalPort}, {vpngateScope, vpnPort}} {
		if _, err := run(set.scope, "multiport", "tuic", strconv.Itoa(set.port), strconv.Itoa(set.port)); err != nil {
			t.Fatal(err)
		}
		link, err := run(set.scope, "links", "--tuic-port", strconv.Itoa(set.port))
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := url.Parse(strings.TrimSpace(link))
		if err != nil || parsed.Scheme != "tuic" || parsed.Port() != strconv.Itoa(set.port) || strings.Contains(parsed.Fragment, "VPNGate-") != (set.scope == vpngateScope) {
			t.Fatal("wrong extra-port link", set.scope, err)
		}
		for _, invalid := range []string{"0", "-1", "65536", "1"} {
			if _, err := run(set.scope, "links", "--tuic-port", invalid); err == nil {
				t.Fatal("invalid or unconfigured TUIC link was exported", invalid)
			}
		}
	}
	if !bytes.Equal(normalServer, readBytes(t, filepath.Join(root, "sb.json"))) || !bytes.Equal(vpnServer, readBytes(t, filepath.Join(root, "vpngate", "server.json"))) {
		t.Fatal("forwarding created extra listeners or changed a running server")
	}
	for name, before := range unchanged {
		if !bytes.Equal(before, readBytes(t, filepath.Join(root, name))) {
			t.Fatal("TUIC forwarding changed unrelated configuration", name)
		}
	}
	var current App
	if _, err := ReadJSON(filepath.Join(root, "app.json"), &current); err != nil {
		t.Fatal(err)
	}
	settings, _, err = loadVPN(root)
	if err != nil || !reflect.DeepEqual(current.Profile.TUICPorts, []string{strconv.Itoa(normalPort)}) || !reflect.DeepEqual(settings.Profiles["JP"].TUICPorts, []string{strconv.Itoa(vpnPort)}) {
		t.Fatal("scoped multiports were not persisted", err)
	}
	newPort, err := AvailablePort(used)
	if err != nil {
		t.Fatal(err)
	}
	core = validatedCore
	if _, err := run(normalScope, "port", "tuic", strconv.Itoa(newPort)); err != nil {
		t.Fatal(err)
	}
	if _, err := run(normalScope, "links", "--tuic-port", strconv.Itoa(normalPort)); err != nil {
		t.Fatal("changing the main port erased the forwarding ports", err)
	}
	if _, err := run(normalScope, "multiport", "tuic"); err != nil {
		t.Fatal(err)
	}
	if _, err := run(normalScope, "links", "--tuic-port", strconv.Itoa(normalPort)); err == nil {
		t.Fatal("cleared forwarding port remained available for export")
	}
	if _, err := run(vpngateScope, "links", "--tuic-port", strconv.Itoa(vpnPort)); err != nil {
		t.Fatal("clearing ordinary forwarding changed the VPNGate ports", err)
	}
}

func TestPortConflictsDoNotCommit(t *testing.T) {
	root, core, app := appFixture(t)
	ctx := context.Background()
	for _, country := range []string{"KR", "JP"} {
		if _, err := PrepareVPN(ctx, root, core, app, country); err != nil {
			t.Fatal(err)
		}
	}
	settings, _, err := loadVPN(root)
	if err != nil {
		t.Fatal(err)
	}
	port, err := AvailablePort(usedPorts(app, settings))
	if err != nil {
		t.Fatal(err)
	}
	if err := configureSubscription(root, app, settings, port); err != nil {
		t.Fatal(err)
	}
	files := []string{"app.json", "sb.json", "vpngate/settings.json", "vpngate/server.json", "jhsub.txt", "vpngate/jhsub.txt"}
	before := map[string][]byte{}
	for _, name := range files {
		before[name] = readBytes(t, filepath.Join(root, name))
	}
	cases := [][]string{
		{"multiport", "tuic", strconv.Itoa(app.Profile.Ports["vless"])},
		{"multiport", "tuic", strconv.Itoa(settings.Profiles["JP"].Ports["tuic"])},
		{"multiport", "tuic", strconv.Itoa(settings.Profiles["KR"].Ports["tuic"])},
		{"hopping", strconv.Itoa(port)},
		{"port", "tuic", strconv.Itoa(port)},
		{"multiport", "tuic", strconv.Itoa(app.Profile.Ports["tuic"]), "--scope", vpngateScope},
	}
	reserved := usedPorts(app, settings)
	reserved[port] = true
	busyPort, err := AvailablePort(reserved)
	if err != nil {
		t.Fatal(err)
	}
	busy, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(busyPort)))
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	cases = append(cases, []string{"multiport", "tuic", fmt.Sprintf("%d:%d", max(1, busyPort-1), min(65535, busyPort+1))})
	for _, args := range cases {
		if err := RunConfig(ctx, append([]string{"--root", root, "--core", core}, args...), &bytes.Buffer{}); err == nil {
			t.Fatal("conflicting port was accepted", args)
		}
		for _, name := range files {
			if !bytes.Equal(before[name], readBytes(t, filepath.Join(root, name))) {
				t.Fatal("rejected port change committed a file", name)
			}
		}
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
	app := App{Profile: Profile{TUICPorts: []string{"20000:59998"}}}
	ranges, err := usedPortRanges(app, VPNSettings{})
	if err != nil {
		t.Fatal(err)
	}
	if err := configureSubscription(t.TempDir(), app, VPNSettings{}, 31000); err == nil {
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
