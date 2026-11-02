package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeTunnel struct{ dead chan error }

func (f *fakeTunnel) start(context.Context, Node) error { return nil }
func (f *fakeTunnel) stop()                             {}
func (f *fakeTunnel) socket() string                    { return "" }
func (f *fakeTunnel) died() <-chan error                { return f.dead }

func TestCancelledVerificationCannotEnableOldCountry(t *testing.T) {
	var csvData strings.Builder
	writer := csv.NewWriter(&csvData)
	_ = writer.Write([]string{"HostName", "IP", "CountryShort", "Ping", "Speed", "OpenVPN_ConfigData_Base64"})
	_ = writer.Write([]string{"jp-node", "203.0.113.5", "JP", "1", "100", base64.StdEncoding.EncodeToString([]byte(safeTestProfile))})
	writer.Flush()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, csvData.String()) }))
	defer server.Close()
	cfg := defaults()
	cfg.DataDir = t.TempDir()
	cfg.RunDir = t.TempDir()
	cfg.ProxyUser = "ygvg-JP"
	cfg.ProxyPass = strings.Repeat("a", 64)
	cfg.Sources = []string{server.URL}
	cfg.Enabled = true
	m := newManager(cfg, filepath.Join(cfg.DataDir, "config.json"))
	m.factory = func(Config) tunnel { return &fakeTunnel{dead: make(chan error)} }
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	m.probeOverride = func(context.Context, Config, tunnel) (Geo, error) {
		once.Do(func() { close(entered) })
		<-release
		return Geo{Country: "JP"}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { m.run(ctx); close(done) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("verification never entered")
	}
	next := cfg
	next.Country = "KR"
	next.ProxyUser = "ygvg-KR"
	if e := m.apply(next, false); e != nil {
		t.Fatal(e)
	}
	close(release)
	ticket, _ := m.authenticate(next.ProxyUser, next.ProxyPass)
	time.Sleep(50 * time.Millisecond)
	if m.allowed(ticket) {
		t.Fatal("cancelled Japanese tunnel was enabled for Korean credentials")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("manager did not stop")
	}
}

const safeTestProfile = "client\ndev tun\nproto udp\nremote 203.0.113.5 1194\nremote-cert-tls server\n<ca>\ntest-certificate\n</ca>\n"

func TestRejectProfileExecutionAndMismatchedRemote(t *testing.T) {
	for _, line := range []string{"script-security 2\nup /bin/sh", "plugin /tmp/code.so", "management 127.0.0.1 1111", "ca /etc/shadow", "config /tmp/other.ovpn", "remote 203.0.113.6 443", "auth-user-pass /etc/shadow"} {
		if _, _, _, e := validateProfile(safeTestProfile+line+"\n", "203.0.113.5"); e == nil {
			t.Fatalf("accepted forbidden directive %q", line)
		}
	}
	if _, port, proto, e := validateProfile(safeTestProfile, "203.0.113.5"); e != nil || port != 1194 || proto != "udp" {
		t.Fatalf("safe profile rejected: %v", e)
	}
}
func TestCSVRejectsMissingCountryAndKnownDatacenter(t *testing.T) {
	var b strings.Builder
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"HostName", "IP", "CountryShort", "Ping", "Speed", "OpenVPN_ConfigData_Base64"})
	for _, row := range [][]string{{"volunteer", "203.0.113.5", "JP"}, {"unknown", "203.0.113.5", ""}, {"wrong", "203.0.113.5", "KR"}, {"public-vpn-1", "203.0.113.5", "JP"}} {
		_ = w.Write(append(row, "10", "100", base64.StdEncoding.EncodeToString([]byte(safeTestProfile))))
	}
	w.Flush()
	nodes, e := parseNodes([]byte("\ufeff*vpn_servers*\n" + b.String()))
	if e != nil {
		t.Fatal(e)
	}
	cfg := defaults()
	cfg.Country = "JP"
	pool := candidates(cfg, nodes, Node{}, false)
	if len(pool) != 1 || pool[0].Host != "volunteer" {
		t.Fatalf("country/residential filter failed: %+v", pool)
	}
}
func TestStrictCountryFixedModeAndManualSwap(t *testing.T) {
	jp := Node{Host: "jp-one", IP: "203.0.113.5", Country: "JP", Profile: safeTestProfile}
	kr := jp
	kr.Host = "kr-one"
	kr.Country = "KR"
	cfg := defaults()
	cfg.Country = "JP"
	cfg.Mode = "fixed"
	cfg.FixedHost = "kr-one"
	if len(candidates(cfg, []Node{jp, kr}, kr, false)) != 0 {
		t.Fatal("fixed mode escaped country")
	}
	cfg.Mode = "sticky"
	cfg.FixedHost = ""
	if len(candidates(cfg, []Node{jp, kr}, jp, true)) != 0 {
		t.Fatal("swap returned old node or other country")
	}
}
func TestGeoRejectsWrongCountryHostingAndUnknownISP(t *testing.T) {
	cfg := defaults()
	for _, g := range []Geo{{Country: "KR"}, {Country: "JP", Hosting: true}} {
		if cfg.acceptGeo(g) == nil {
			t.Fatal("invalid exit accepted")
		}
	}
	cfg.RequireISP = true
	if cfg.acceptGeo(Geo{Country: "JP"}) == nil {
		t.Fatal("unknown ISP accepted in strict ISP mode")
	}
	cfg.AllowASNs = []int{42}
	if cfg.acceptGeo(Geo{Country: "JP", ASN: 7, ASNType: "isp", HostingKnown: true}) == nil {
		t.Fatal("ASN allowlist bypass")
	}
}
func TestKernelGuardHasNoUnmarkedUnderlayAllowance(t *testing.T) {
	mark, err := strconv.Atoi(packetMark)
	if err != nil || mark != 0x59564701 {
		t.Fatal("OpenVPN mark is not decimal")
	}
	n := Node{IP: "203.0.113.5", Network: "udp", Port: 1194}
	rules := guardRules(n)
	policy := false
	for _, r := range rules {
		if reflect.DeepEqual(r, []string{"-P", "OUTPUT", "DROP"}) {
			policy = true
		}
		line := strings.Join(r, " ")
		if strings.Contains(line, "-A OUTPUT") && strings.Contains(line, "-o "+peerVeth) {
			for _, required := range []string{"--mark " + packetMark, "-d " + n.IP + "/32", "--dport 1194"} {
				if !strings.Contains(line, required) {
					t.Fatalf("underlay bypass in %v", r)
				}
			}
		}
	}
	if !policy {
		t.Fatal("missing fail-closed kernel policy")
	}
}
func TestSubnetDoesNotOverlapExistingRoute(t *testing.T) {
	s, e := chooseSubnet([]byte(`[{"dst":"default"},{"dst":"10.0.0.0/8"},{"dst":"172.16.0.0/12"}]`))
	if e != nil || s != "192.168.223.0/30" {
		t.Fatalf("unsafe subnet: %s %v", s, e)
	}
}
func TestPolicyEpochInvalidatesAlreadyAuthenticatedClient(t *testing.T) {
	cfg := defaults()
	cfg.ProxyUser = "old"
	cfg.ProxyPass = strings.Repeat("a", 32)
	m := newManager(cfg, "")
	m.ready = true
	ticket, ok := m.authenticate("old", cfg.ProxyPass)
	if !ok || !m.allowed(ticket) {
		t.Fatal("initial auth failed")
	}
	m.revoke()
	m.ready = true
	m.cfg.Country = "KR"
	if m.allowed(ticket) {
		t.Fatal("old JP credentials survived a country switch")
	}
}

type testAccess struct {
	mu     sync.Mutex
	conns  map[net.Conn]bool
	socket string
	ready  bool
}

func (a *testAccess) authenticate(u, p string) (ticket, bool) {
	return ticket{epoch: 1, country: "JP"}, u == "user" && p == "password"
}
func (a *testAccess) allowed(t ticket) bool { return a.ready && t.epoch == 1 }
func (a *testAccess) open(ctx context.Context, t ticket, n, d string) (net.Conn, string, error) {
	if !a.allowed(t) {
		return nil, "", fmt.Errorf("not ready")
	}
	c, s, e := dialWorker(ctx, a.socket, n, d)
	if e == nil {
		a.track(c)
	}
	return c, s, e
}
func (a *testAccess) track(c net.Conn) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.conns[c] = true
	return true
}
func (a *testAccess) untrack(c net.Conn) { a.mu.Lock(); delete(a.conns, c); a.mu.Unlock() }
func startProxyFixture(t *testing.T) (string, *testAccess, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	socket := filepath.Join(t.TempDir(), "w.sock")
	w := worker{socket: socket, dialOverride: func(ctx context.Context, n, a string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, n, a)
	}}
	go w.serve(ctx)
	deadline := time.Now().Add(3 * time.Second)
	for {
		c, e := net.Dial("unix", socket)
		if e == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("worker did not bind")
		}
		time.Sleep(10 * time.Millisecond)
	}
	ln, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		cancel()
		t.Fatal(e)
	}
	access := &testAccess{socket: socket, ready: true, conns: map[net.Conn]bool{}}
	go serveProxy(ctx, ln, access)
	t.Cleanup(func() {
		cancel()
		access.mu.Lock()
		for c := range access.conns {
			c.Close()
		}
		access.mu.Unlock()
	})
	return ln.Addr().String(), access, cancel
}
func authProxy(t *testing.T, address string, pass string) net.Conn {
	t.Helper()
	c, e := net.Dial("tcp", address)
	if e != nil {
		t.Fatal(e)
	}
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = c.Write([]byte{5, 1, 2})
	var h [2]byte
	if _, e = io.ReadFull(c, h[:]); e != nil || h != [2]byte{5, 2} {
		t.Fatalf("auth method: %v %v", h, e)
	}
	b := append([]byte{1, 4}, []byte("user")...)
	b = append(b, byte(len(pass)))
	b = append(b, []byte(pass)...)
	_, _ = c.Write(b)
	if _, e = io.ReadFull(c, h[:]); e != nil {
		t.Fatal(e)
	}
	if h[1] != 0 {
		c.Close()
		return nil
	}
	return c
}
func requestAddress(command byte, address string) []byte {
	host, port, _ := net.SplitHostPort(address)
	p := 0
	fmt.Sscanf(port, "%d", &p)
	b := append([]byte{5, command, 0, 1}, net.ParseIP(host).To4()...)
	return binary.BigEndian.AppendUint16(b, uint16(p))
}
func TestProxyAuthAndTCPRelay(t *testing.T) {
	proxy, _, _ := startProxyFixture(t)
	if c := authProxy(t, proxy, "wrong"); c != nil {
		t.Fatal("wrong password accepted")
	}
	echo, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer echo.Close()
	go func() {
		c, _ := echo.Accept()
		if c != nil {
			defer c.Close()
			_, _ = io.Copy(c, c)
		}
	}()
	c := authProxy(t, proxy, "password")
	defer c.Close()
	_, _ = c.Write(requestAddress(1, echo.Addr().String()))
	var h [10]byte
	if _, e = io.ReadFull(c, h[:]); e != nil || h[1] != 0 {
		t.Fatal("connect failed", e, h)
	}
	_, _ = c.Write([]byte("through-tunnel"))
	b := make([]byte, 14)
	if _, e = io.ReadFull(c, b); e != nil || string(b) != "through-tunnel" {
		t.Fatal("relay failed", e, string(b))
	}
}
func TestProxyUDPRelayAndFragmentRejection(t *testing.T) {
	proxy, _, _ := startProxyFixture(t)
	echo, e := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if e != nil {
		t.Fatal(e)
	}
	defer echo.Close()
	go func() {
		b := make([]byte, 1024)
		for {
			n, a, e := echo.ReadFromUDP(b)
			if e != nil {
				return
			}
			_, _ = echo.WriteToUDP(b[:n], a)
		}
	}()
	c := authProxy(t, proxy, "password")
	defer c.Close()
	_, _ = c.Write(append([]byte{5, 3, 0, 4}, make([]byte, 18)...))
	var h [10]byte
	if _, e = io.ReadFull(c, h[:]); e != nil || h[1] != 0 {
		t.Fatal("association failed", e)
	}
	relayAddr := &net.UDPAddr{IP: net.IP(h[4:8]), Port: int(binary.BigEndian.Uint16(h[8:10]))}
	u, e := net.DialUDP("udp4", nil, relayAddr)
	if e != nil {
		t.Fatal(e)
	}
	defer u.Close()
	req := requestAddress(1, echo.LocalAddr().String())
	packet := append([]byte{0, 0, 0, req[3]}, req[4:]...)
	packet = append(packet, []byte("udp-through-tunnel")...)
	_ = u.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = u.Write(packet)
	b := make([]byte, 1024)
	n, e := u.Read(b)
	if e != nil || string(b[10:n]) != "udp-through-tunnel" {
		t.Fatal("UDP relay failed", e)
	}
	packet[2] = 1
	_, _ = u.Write(packet)
	_ = u.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if _, e = u.Read(b); e == nil {
		t.Fatal("fragmented packet forwarded")
	}
}
