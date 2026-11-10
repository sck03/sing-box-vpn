package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/sck03/sing-box-vpn/internal/command"
)

const namespace = "ygvg0"
const hostVeth = "ygvg-host"
const peerVeth = "ygvg-peer"

// OpenVPN parses --mark with atoi(), so its CLI must receive decimal.
const packetMark = "1498826497"

type backend struct {
	cfg                  Config
	runCommand           func(context.Context, string, ...string) ([]byte, error)
	owner                ownerState
	claimed              bool
	cleanupErr           error
	ovpn, worker         *exec.Cmd
	ovpnDone, workerDone chan error
	exited               chan error
	workerSocket         string
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	deadline, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, e := command.Output(deadline, nil, name, args...)
	if e != nil {
		return out, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), e)
	}
	return out, nil
}

func preflight() error {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return errors.New("gateway requires Linux root with network namespaces")
	}
	if _, e := os.Stat("/dev/net/tun"); e != nil {
		return errors.New("/dev/net/tun is unavailable")
	}
	for _, name := range []string{"ip", "iptables", "openvpn", "sysctl", "kill"} {
		if _, e := exec.LookPath(name); e != nil {
			return fmt.Errorf("missing dependency: %s", name)
		}
	}
	return nil
}

func (b *backend) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if b.runCommand != nil {
		return b.runCommand(ctx, name, args...)
	}
	return runCommand(ctx, name, args...)
}
func (b *backend) must(ctx context.Context, name string, args ...string) error {
	_, e := b.run(ctx, name, args...)
	return e
}
func (b *backend) ns(ctx context.Context, name string, args ...string) error {
	full := append([]string{"netns", "exec", namespace, name}, args...)
	return b.must(ctx, "ip", full...)
}
func (b *backend) iptables(ctx context.Context, args ...string) error {
	return b.must(ctx, "iptables", append([]string{"-w", "5"}, args...)...)
}
func chooseSubnet(routes []byte) (string, error) {
	var entries []struct {
		Dst string `json:"dst"`
	}
	if e := json.Unmarshal(routes, &entries); e != nil {
		return "", e
	}
	prefixes := make([]netip.Prefix, 0, len(entries))
	for _, route := range entries {
		if route.Dst == "default" || route.Dst == "0.0.0.0/0" {
			continue
		}
		prefix, err := netip.ParsePrefix(route.Dst)
		if err != nil {
			// ip -j omits /32 on a host route; it still reserves that address.
			if address, err := netip.ParseAddr(route.Dst); err == nil {
				prefix = netip.PrefixFrom(address, address.BitLen())
			} else {
				return "", fmt.Errorf("invalid route destination %q", route.Dst)
			}
		}
		prefixes = append(prefixes, prefix)
	}
	for _, candidate := range []string{"10.231.254.0/30", "10.232.254.0/30", "172.29.253.0/30", "192.168.223.0/30"} {
		n := netip.MustParsePrefix(candidate)
		conflict := false
		for _, other := range prefixes {
			if n.Overlaps(other) {
				conflict = true
				break
			}
		}
		if !conflict {
			return candidate, nil
		}
	}
	return "", errors.New("no unused private /30 for the gateway")
}

func guardRules(n Node) [][]string {
	return [][]string{
		{"-P", "OUTPUT", "DROP"}, {"-P", "INPUT", "DROP"},
		{"-A", "OUTPUT", "-o", "lo", "-j", "ACCEPT"}, {"-A", "INPUT", "-i", "lo", "-j", "ACCEPT"},
		{"-A", "OUTPUT", "-o", "tun0", "-j", "ACCEPT"}, {"-A", "INPUT", "-i", "tun0", "-j", "ACCEPT"},
		{"-A", "OUTPUT", "-o", peerVeth, "-d", n.IP + "/32", "-p", n.Network, "--dport", strconv.Itoa(n.Port), "-m", "mark", "--mark", packetMark, "-j", "ACCEPT"},
		{"-A", "INPUT", "-i", peerVeth, "-s", n.IP + "/32", "-p", n.Network, "--sport", strconv.Itoa(n.Port), "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT"},
	}
}

func (b *backend) setup(ctx context.Context, n Node) error {
	marker := filepath.Join(b.cfg.DataDir, "namespace-owner.json")
	if err := b.recoverOwnership(); err != nil {
		return err
	}
	list, e := b.run(ctx, "ip", "netns", "list")
	if e != nil {
		return e
	}
	for _, line := range strings.Split(string(list), "\n") {
		f := strings.Fields(line)
		if len(f) > 0 && f[0] == namespace {
			return errors.New("ygvg0 already exists without this service's ownership")
		}
	}
	if _, e := b.run(ctx, "ip", "link", "show", hostVeth); e == nil {
		return errors.New("gateway veth name is already occupied")
	}
	for _, args := range [][]string{{"-S", "YGVG_FWD"}, {"-t", "nat", "-S", "YGVG_NAT"}} {
		if _, e := b.run(ctx, "iptables", args...); e == nil {
			return errors.New("gateway firewall chain already exists without ownership")
		}
	}
	routes, e := b.run(ctx, "ip", "-j", "-4", "route", "show", "table", "all")
	if e != nil {
		return e
	}
	subnet, e := chooseSubnet(routes)
	if e != nil {
		return e
	}
	forward, e := b.run(ctx, "sysctl", "-n", "net.ipv4.ip_forward")
	if e != nil {
		return e
	}
	b.owner = ownerState{Owner: ownerID, Subnet: subnet, ForwardBefore: strings.TrimSpace(string(forward))}
	if e = b.owner.validate(); e != nil {
		return e
	}
	if e = atomicJSON(marker, b.owner); e != nil {
		return e
	}
	b.claimed = true
	base := strings.TrimSuffix(subnet, "0/30")
	hostIP, peerIP := base+"1", base+"2"
	commands := [][]string{
		{"ip", "netns", "add", namespace},
		{"ip", "link", "add", hostVeth, "type", "veth", "peer", "name", peerVeth},
		{"ip", "link", "set", peerVeth, "netns", namespace},
		{"ip", "addr", "add", hostIP + "/30", "dev", hostVeth},
		{"ip", "link", "set", hostVeth, "up"},
	}
	for _, c := range commands {
		if e = b.must(ctx, c[0], c[1:]...); e != nil {
			return e
		}
	}
	for _, c := range [][]string{{"link", "set", "lo", "up"}, {"addr", "add", peerIP + "/30", "dev", peerVeth}, {"link", "set", peerVeth, "up"}, {"route", "add", "default", "via", hostIP}, {"route", "replace", n.IP + "/32", "via", hostIP, "dev", peerVeth}} {
		if e = b.ns(ctx, "ip", c...); e != nil {
			return e
		}
	}
	// Only our namespace is changed. IPv6 is disabled before any data worker starts.
	_ = b.ns(ctx, "sysctl", "-qw", "net.ipv6.conf.all.disable_ipv6=1")
	_ = b.ns(ctx, "sysctl", "-qw", "net.ipv6.conf.default.disable_ipv6=1")
	for _, rule := range guardRules(n) {
		if e = b.ns(ctx, "iptables", append([]string{"-w", "5"}, rule...)...); e != nil {
			return e
		}
	}
	// Host rules are scoped to this veth/subnet and are removed individually.
	for _, r := range [][]string{
		{"-N", "YGVG_FWD"}, {"-A", "YGVG_FWD", "-s", subnet, "-j", "ACCEPT"},
		{"-A", "YGVG_FWD", "-d", subnet, "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT"},
		{"-I", "FORWARD", "1", "-i", hostVeth, "-s", subnet, "-j", "YGVG_FWD"},
		{"-I", "FORWARD", "1", "-o", hostVeth, "-d", subnet, "-j", "YGVG_FWD"},
		{"-t", "nat", "-N", "YGVG_NAT"}, {"-t", "nat", "-A", "YGVG_NAT", "-j", "MASQUERADE"},
		{"-t", "nat", "-I", "POSTROUTING", "1", "-s", subnet, "-j", "YGVG_NAT"},
	} {
		if e = b.iptables(ctx, r...); e != nil {
			return e
		}
	}
	if e = b.must(ctx, "sysctl", "-qw", "net.ipv4.ip_forward=1"); e != nil {
		return e
	}
	return nil
}

func (b *backend) start(ctx context.Context, n Node) error {
	b.exited = make(chan error, 1)
	if e := b.setup(ctx, n); e != nil {
		return e
	}
	profilePath := filepath.Join(b.cfg.DataDir, "current.ovpn")
	authPath := filepath.Join(b.cfg.DataDir, "openvpn.auth")
	if e := os.WriteFile(profilePath, []byte(n.Profile), 0600); e != nil {
		return e
	}
	if e := os.WriteFile(authPath, []byte("vpn\nvpn\n"), 0600); e != nil {
		return e
	}
	proto := "udp4"
	if n.Network == "tcp" {
		proto = "tcp-client"
	}
	args := []string{"netns", "exec", namespace, "openvpn", "--config", profilePath, "--dev", "tun0", "--dev-type", "tun", "--proto", proto, "--auth-user-pass", authPath, "--auth-nocache", "--mark", packetMark, "--redirect-gateway", "def1", "--connect-retry-max", "1", "--connect-timeout", "15", "--data-ciphers", "AES-256-GCM:AES-128-GCM:CHACHA20-POLY1305:AES-128-CBC:AES-256-CBC", "--data-ciphers-fallback", "AES-128-CBC", "--verb", "3"}
	b.ovpn = exec.CommandContext(ctx, "ip", args...)
	ready := make(chan struct{})
	output := &openVPNOutput{ready: ready}
	log, _ := os.OpenFile(filepath.Join(b.cfg.DataDir, "openvpn.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if log != nil {
		output.log = log
	}
	closeLog := func() {
		if log != nil {
			_ = log.Close()
		}
	}
	b.ovpn.Stdout = output
	b.ovpn.Stderr = b.ovpn.Stdout
	if e := b.ovpn.Start(); e != nil {
		closeLog()
		return e
	}
	b.ovpnDone = make(chan error, 1)
	go waitProcess(b.ovpn, b.ovpnDone, b.exited, closeLog)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Duration(b.cfg.ConnectTimeout) * time.Second):
		return errors.New("OpenVPN handshake timeout")
	case <-b.ovpnDone:
		return errors.New("OpenVPN exited before handshake")
	case <-ready:
	}
	executable, e := os.Executable()
	if e != nil {
		return e
	}
	b.workerSocket = filepath.Join(b.cfg.RunDir, "worker.sock")
	_ = os.Remove(b.workerSocket)
	b.worker = exec.CommandContext(ctx, "ip", "netns", "exec", namespace, executable, "worker", "--socket", b.workerSocket, "--dns", b.cfg.DNS)
	b.worker.Stdout = os.Stdout
	b.worker.Stderr = os.Stderr
	if e = b.worker.Start(); e != nil {
		return e
	}
	b.workerDone = make(chan error, 1)
	go waitProcess(b.worker, b.workerDone, b.exited, nil)
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("tunnel worker failed to start")
		case <-b.workerDone:
			return errors.New("tunnel worker exited")
		case <-b.ovpnDone:
			return errors.New("OpenVPN exited while starting the tunnel worker")
		case <-tick.C:
			c, e := net.DialTimeout("unix", b.workerSocket, 100*time.Millisecond)
			if e == nil {
				c.Close()
				return nil
			}
		}
	}
}

func waitProcess(cmd *exec.Cmd, done, exited chan error, cleanup func()) {
	e := cmd.Wait()
	if cleanup != nil {
		cleanup()
	}
	done <- e
	close(done)
	select {
	case exited <- e:
	default:
	}
}

func stopProcess(cmd *exec.Cmd, done chan error) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	select {
	case <-done:
		return
	default:
	}
	_ = cmd.Process.Signal(os.Interrupt)
	select {
	case <-done:
		return
	case <-time.After(2 * time.Second):
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	}
}
func (b *backend) stop() {
	stopProcess(b.worker, b.workerDone)
	stopProcess(b.ovpn, b.ovpnDone)
	b.worker = nil
	b.ovpn = nil
	b.cleanupErr = nil
	if !b.claimed {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	orphanErr := b.killOrphans(ctx)
	// Keep the namespace name available for retry if its processes cannot be
	// enumerated or stopped. Removing the veth still blocks their underlay.
	if orphanErr == nil {
		_, _ = b.run(ctx, "ip", "netns", "del", namespace)
	}
	_, _ = b.run(ctx, "ip", "link", "del", hostVeth)
	s := b.owner.Subnet
	for _, args := range [][]string{
		{"-D", "FORWARD", "-i", hostVeth, "-s", s, "-j", "YGVG_FWD"}, {"-D", "FORWARD", "-o", hostVeth, "-d", s, "-j", "YGVG_FWD"},
		{"-F", "YGVG_FWD"}, {"-X", "YGVG_FWD"}, {"-t", "nat", "-D", "POSTROUTING", "-s", s, "-j", "YGVG_NAT"},
		{"-t", "nat", "-F", "YGVG_NAT"}, {"-t", "nat", "-X", "YGVG_NAT"},
	} {
		_, _ = b.run(ctx, "iptables", append([]string{"-w", "5"}, args...)...)
	}
	names, err := b.run(ctx, "ip", "netns", "list")
	if err != nil {
		b.cleanupErr = errors.Join(orphanErr, err)
		return
	}
	links, err := b.run(ctx, "ip", "-j", "link", "show")
	if err != nil {
		b.cleanupErr = errors.Join(orphanErr, err)
		return
	}
	var interfaces []struct{ Ifname string }
	if err := json.Unmarshal(links, &interfaces); err != nil {
		b.cleanupErr = err
		return
	}
	filter, filterErr := b.run(ctx, "iptables", "-w", "5", "-S")
	nat, natErr := b.run(ctx, "iptables", "-w", "5", "-t", "nat", "-S")
	if err := errors.Join(orphanErr, filterErr, natErr); err != nil {
		b.cleanupErr = err
		return
	}
	remaining := namespaceListed(names) || strings.Contains(string(filter), "YGVG_FWD") || strings.Contains(string(nat), "YGVG_NAT")
	for _, link := range interfaces {
		remaining = remaining || link.Ifname == hostVeth
	}
	if remaining {
		b.cleanupErr = errors.New("gateway cleanup incomplete; ownership retained for retry")
		return
	}
	if b.owner.ForwardBefore == "0" {
		if strings.TrimSpace(string(names)) == "" && !strings.Contains(string(nat), "MASQUERADE") {
			if _, err := b.run(ctx, "sysctl", "-qw", "net.ipv4.ip_forward=0"); err != nil {
				b.cleanupErr = err
				return
			}
		}
	}
	if err := os.Remove(filepath.Join(b.cfg.RunDir, "worker.sock")); err != nil && !os.IsNotExist(err) {
		b.cleanupErr = err
		return
	}
	if err := os.Remove(filepath.Join(b.cfg.DataDir, "namespace-owner.json")); err != nil && !os.IsNotExist(err) {
		b.cleanupErr = err
		return
	}
	b.claimed = false
}

func namespaceListed(data []byte) bool {
	for line := range strings.SplitSeq(string(data), "\n") {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == namespace {
			return true
		}
	}
	return false
}

// SIGKILL of the daemon bypasses stopProcess. Only processes still attached to
// our owned namespace may be killed; unlinking netns alone would leave them alive.
func (b *backend) killOrphans(ctx context.Context) error {
	names, err := b.run(ctx, "ip", "netns", "list")
	if err != nil || !namespaceListed(names) {
		return err
	}
	data, err := b.run(ctx, "ip", "netns", "pids", namespace)
	if err != nil {
		return err
	}
	pids := strings.Fields(string(data))
	if len(pids) == 0 {
		return nil
	}
	for _, pid := range pids {
		n, err := strconv.Atoi(pid)
		if err != nil || n <= 1 || n >= 1<<31 || n == os.Getpid() {
			return errors.New("invalid process ID in owned namespace")
		}
	}
	_, _ = b.run(ctx, "kill", append([]string{"-KILL", "--"}, pids...)...)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		data, err := b.run(ctx, "ip", "netns", "pids", namespace)
		if err != nil {
			return err
		}
		if len(strings.Fields(string(data))) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("owned namespace processes did not stop; cleanup can be retried")
		case <-tick.C:
		}
	}
}
