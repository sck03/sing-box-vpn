//go:build linux

package manage

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLinuxMultiportFirewall(t *testing.T) {
	if os.Getenv("RUN_FIREWALL_TESTS") != "1" {
		t.Skip("set RUN_FIREWALL_TESTS=1 under Linux root")
	}
	if os.Geteuid() != 0 {
		t.Fatal("namespace firewall acceptance requires root")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	run := func(args ...string) []byte {
		t.Helper()
		output, err := exec.CommandContext(ctx, "ip", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("ip %v: %v: %s", args, err, output)
		}
		return output
	}
	id := "sb-fw-" + strconv.Itoa(os.Getpid()) + "-" + randomUUID()[:8]
	serverNS, clientNS := id+"-s", id+"-c"
	for _, name := range []string{serverNS, clientNS} {
		run("netns", "add", name)
		t.Cleanup(func() { _ = exec.Command("ip", "netns", "del", name).Run() })
		run("-n", name, "link", "set", "lo", "up")
	}
	run("-n", serverNS, "link", "add", "fw-server", "type", "veth", "peer", "name", "fw-client", "netns", clientNS)
	for _, pair := range []struct{ namespace, link, ipv4, ipv6 string }{{serverNS, "fw-server", "192.0.2.1/30", "fd00:532b::1/64"}, {clientNS, "fw-client", "192.0.2.2/30", "fd00:532b::2/64"}} {
		run("-n", pair.namespace, "addr", "add", pair.ipv4, "dev", pair.link)
		run("-n", pair.namespace, "-6", "addr", "add", pair.ipv6, "dev", pair.link, "nodad")
		run("-n", pair.namespace, "link", "set", pair.link, "up")
	}
	execute := func(c context.Context, binary string, args ...string) ([]byte, error) {
		return exec.CommandContext(c, "ip", append([]string{"netns", "exec", serverNS, binary}, args...)...).CombinedOutput()
	}
	for _, binary := range []string{"iptables", "ip6tables"} {
		for _, args := range [][]string{{"-P", "INPUT", "DROP"}, {"-N", "FOREIGN_FILTER"}, {"-A", "INPUT", "-j", "FOREIGN_FILTER"}, {"-t", "nat", "-N", "FOREIGN_NAT"}, {"-t", "nat", "-A", "PREROUTING", "-j", "FOREIGN_NAT"}} {
			if output, err := execute(ctx, binary, args...); err != nil {
				t.Fatalf("seed %s: %v: %s", binary, err, output)
			}
		}
		if binary == "ip6tables" {
			if output, err := execute(ctx, binary, "-A", "INPUT", "-p", "ipv6-icmp", "-j", "ACCEPT"); err != nil {
				t.Fatalf("allow IPv6 neighbor discovery: %v: %s", err, output)
			}
		}
	}
	profile := func(base int, hopping, tuic []string) Profile {
		p := Profile{Ports: map[string]int{}, Hopping: hopping, TUICPorts: tuic}
		for i, kind := range Protocols {
			p.Ports[kind] = base + i
		}
		return p
	}
	app := App{Profile: profile(20000, []string{"31000:31002"}, []string{"31100", "31110:31112"}), SocksPort: 40000}
	app.Inbounds = MakeInbounds(app.Profile, "")
	settings := VPNSettings{ActiveCountry: "JP", ProxyPort: 7928, Profiles: map[string]Profile{
		"JP": profile(22000, []string{"32000:32002"}, []string{"32100"}),
		"KR": profile(24000, []string{"34000"}, []string{"34100"}),
	}}
	root := t.TempDir()
	write := func() {
		t.Helper()
		gateway := Object{"inbounds": MakeInbounds(settings.Profiles[settings.ActiveCountry], "vg-")}
		for _, file := range []struct {
			name  string
			value any
		}{{"app.json", app}, {"vpngate/settings.json", settings}, {"vpngate/server.json", gateway}} {
			if err := AtomicJSON(filepath.Join(root, file.name), file.value); err != nil {
				t.Fatal(err)
			}
		}
	}
	write()
	if err := os.WriteFile(filepath.Join(root, "firewall.owner"), []byte("sck03/sing-box-vpn\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f := firewall{execute: execute, restore: func(c context.Context, binary, rules string) ([]byte, error) {
		command := exec.CommandContext(c, "ip", "netns", "exec", serverNS, binary, "-w", "5", "--noflush")
		command.Stdin = strings.NewReader(rules)
		return command.CombinedOutput()
	}}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	server := exec.CommandContext(ctx, "ip", "netns", "exec", serverNS, binary, "-test.run=^TestFirewallNamespaceHelper$")
	server.Env = append(os.Environ(), "SBVPN_FIREWALL_HELPER=server")
	server.Stderr = os.Stderr
	stdout, err := server.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Process.Kill(); _ = server.Wait() })
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if line != "READY\n" {
			t.Fatal("UDP responder did not start", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("UDP responder startup timed out")
	}
	probeNumber := 0
	probe := func(port, target int) {
		t.Helper()
		for _, address := range []string{"192.0.2.1", "fd00:532b::1"} {
			probeNumber++
			command := exec.CommandContext(ctx, "ip", "netns", "exec", clientNS, binary, "-test.run=^TestFirewallNamespaceHelper$")
			command.Env = append(os.Environ(), "SBVPN_FIREWALL_HELPER=probe", "SBVPN_FIREWALL_ADDRESS="+net.JoinHostPort(address, strconv.Itoa(port)), "SBVPN_FIREWALL_TARGET="+strconv.Itoa(target), "SBVPN_FIREWALL_SOURCE="+strconv.Itoa(50000+probeNumber))
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("forward %s:%d -> %d: %v: %s", address, port, target, err, output)
			}
		}
	}
	if err := f.sync(ctx, root, false); err != nil {
		t.Fatal(err)
	}
	for _, ports := range [][2]int{{31000, 20002}, {31002, 20002}, {31100, 20003}, {31112, 20003}, {32002, 22002}, {32100, 22003}, {34100, 0}, {33333, 0}} {
		probe(ports[0], ports[1])
	}
	app.Profile.TUICPorts = nil
	jp := settings.Profiles["JP"]
	jp.TUICPorts = nil
	settings.Profiles["JP"] = jp
	write()
	if err := f.sync(ctx, root, false); err != nil {
		t.Fatal(err)
	}
	probe(31100, 0)
	probe(32100, 0)
	probe(31001, 20002)
	settings.ActiveCountry = "KR"
	write()
	if err := f.sync(ctx, root, false); err != nil {
		t.Fatal(err)
	}
	probe(34100, 24003)
	probe(32000, 0)
	if err := f.sync(ctx, root, true); err != nil {
		t.Fatal(err)
	}
	for _, binary := range []string{"iptables", "ip6tables"} {
		for _, chain := range []struct{ table, name string }{{"filter", inputChain}, {"nat", udpChain}} {
			if _, err := execute(ctx, binary, "-t", chain.table, "-S", chain.name); err == nil {
				t.Fatal("owned chain remained after removal", chain.name)
			}
		}
		for _, chain := range []struct{ table, name string }{{"filter", "FOREIGN_FILTER"}, {"nat", "FOREIGN_NAT"}} {
			if output, err := execute(ctx, binary, "-t", chain.table, "-S", chain.name); err != nil {
				t.Fatalf("unrelated firewall chain was removed: %v: %s", err, output)
			}
		}
	}
}

func TestLinuxUDPRangeConflict(t *testing.T) {
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	port := conn.LocalAddr().(*net.UDPAddr).Port
	if err := checkSystemPorts([]portRange{{max(1, port-1), min(65535, port+1)}}, 0); err == nil {
		t.Fatal("kernel socket inventory missed a UDP socket inside a range")
	}
}

func TestFirewallNamespaceHelper(t *testing.T) {
	switch os.Getenv("SBVPN_FIREWALL_HELPER") {
	case "server":
		for _, port := range []int{20002, 20003, 22002, 22003, 24002, 24003} {
			conn, err := net.ListenPacket("udp", ":"+strconv.Itoa(port))
			if err != nil {
				t.Fatal(err)
			}
			go func() {
				defer conn.Close()
				buffer := make([]byte, 1024)
				for {
					n, peer, err := conn.ReadFrom(buffer)
					if err != nil {
						return
					}
					_, _ = conn.WriteTo([]byte(strconv.Itoa(port)+":"+string(buffer[:n])), peer)
				}
			}()
		}
		fmt.Println("READY")
		select {}
	case "probe":
		address := os.Getenv("SBVPN_FIREWALL_ADDRESS")
		remote, err := net.ResolveUDPAddr("udp", address)
		if err != nil {
			t.Fatal(err)
		}
		port, _ := strconv.Atoi(os.Getenv("SBVPN_FIREWALL_SOURCE"))
		conn, err := net.DialUDP("udp", &net.UDPAddr{Port: port}, remote)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(400 * time.Millisecond))
		if _, err := conn.Write([]byte("forward-check")); err != nil {
			t.Fatal(err)
		}
		buffer := make([]byte, 1024)
		n, err := conn.Read(buffer)
		target := os.Getenv("SBVPN_FIREWALL_TARGET")
		if target == "0" {
			if err == nil {
				t.Fatal("inactive or removed forwarding still accepted traffic", string(buffer[:n]))
			}
		} else if err != nil || string(buffer[:n]) != target+":forward-check" {
			t.Fatal("UDP reached the wrong port", err, string(buffer[:n]))
		}
	default:
		t.Skip("helper only runs inside the isolated firewall test")
	}
}
