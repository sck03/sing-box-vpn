package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestProxyTCPHalfClosePreservesResponse(t *testing.T) {
	proxy, _, _ := startProxyFixture(t)
	server, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	response := strings.Repeat("response-after-request-eof", 4096)
	go func() {
		c, err := server.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		request, err := io.ReadAll(c)
		if err == nil && string(request) == "request" {
			_, _ = io.WriteString(c, response)
		}
	}()
	c := authProxy(t, proxy, "password")
	defer c.Close()
	_, _ = c.Write(requestAddress(1, server.Addr().String()))
	var reply [10]byte
	if _, err = io.ReadFull(c, reply[:]); err != nil || reply[1] != 0 {
		t.Fatal("connect failed", err, reply)
	}
	_, _ = io.WriteString(c, "request")
	if err = c.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(c)
	if err != nil || string(got) != response {
		t.Fatalf("half-close lost response: got %d of %d bytes, %v", len(got), len(response), err)
	}
}

func TestDialWorkerCancellationInterruptsHandshake(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worker.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accepted <- c
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		c, _, err := dialWorker(ctx, path, "tcp", "203.0.113.5:443")
		if c != nil {
			c.Close()
		}
		done <- err
	}()
	select {
	case c := <-accepted:
		defer c.Close()
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not accept")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled handshake succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled handshake kept waiting for the worker")
	}
}

func TestRevocationReleasesConnectionCapacity(t *testing.T) {
	cfg := defaults()
	cfg.MaxConnections = 1
	m := newManager(cfg, "")
	a, peer := net.Pipe()
	defer peer.Close()
	defer a.Close()
	if !m.track(a) {
		t.Fatal("initial connection rejected")
	}
	m.revoke()
	b, other := net.Pipe()
	defer other.Close()
	defer b.Close()
	if !m.track(b) {
		t.Fatal("revoked connection retained capacity until its handler finished")
	}
	m.untrack(a)
	m.untrack(b)
}

func TestDaemonLockCannotBeStolenAndReleasesOnClose(t *testing.T) {
	dir := t.TempDir()
	unlock, err := lockDaemon(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !daemonAlive(dir) {
		unlock()
		t.Fatal("active daemon lock was not detected")
	}
	if other, err := lockDaemon(dir); err == nil {
		other()
		unlock()
		t.Fatal("second daemon acquired the lock")
	}
	unlock()
	if daemonAlive(dir) {
		t.Fatal("closed daemon still owns the lock")
	}
	other, err := lockDaemon(dir)
	if err != nil {
		t.Fatal("stale PID prevented daemon restart", err)
	}
	other()
}

func TestUnchangedHealthStatusDoesNotRewriteDisk(t *testing.T) {
	cfg := defaults()
	cfg.DataDir = t.TempDir()
	m := newManager(cfg, "")
	node := Node{Host: "jp-node"}
	geo := &Geo{IP: "203.0.113.5", Country: "JP"}
	m.setStatus("ready", "verified", node, geo)
	path := filepath.Join(cfg.DataDir, "state.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	first := m.snapshot().Updated
	m.setStatus("ready", "verified", node, geo)
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("identical health probe rewrote persisted state: %v", err)
	}
	if !m.snapshot().Updated.After(first) {
		t.Fatal("live status did not record the latest health probe")
	}
	m.setStatus("waiting", "probe failed", node, nil)
	after, err = os.ReadFile(path)
	if err != nil || bytes.Equal(before, after) {
		t.Fatal("state transition was not persisted", err)
	}
}

func TestFrameReaderGrowthKeepsHeaderAndEnforcesPacketLimit(t *testing.T) {
	var wire bytes.Buffer
	payloads := [][]byte{nil, []byte("small"), bytes.Repeat([]byte{7}, maxPacket-10), []byte("small-again")}
	for _, payload := range payloads {
		if err := writeFrame(&wire, payload); err != nil {
			t.Fatal(err)
		}
	}
	header := []byte{0, 0, 0, 1, 203, 0, 113, 5, 0, 53}
	r := frameReader{source: &wire, buffer: append([]byte(nil), header...), prefix: len(header), limit: maxPacket - len(header)}
	for i, payload := range payloads {
		packet, err := r.read()
		if err != nil || !bytes.Equal(packet[:len(header)], header) || !bytes.Equal(packet[len(header):], payload) {
			t.Fatalf("packet %d corrupted after buffer growth: %v", i, err)
		}
		if cap(r.buffer) > maxPacket || i < 2 && cap(r.buffer) > 2048 {
			t.Fatal("small datagram retained an oversized buffer")
		}
	}
	var oversized [2]byte
	binary.BigEndian.PutUint16(oversized[:], maxPacket)
	r.source = bytes.NewReader(oversized[:])
	if _, err := r.read(); err == nil {
		t.Fatal("accepted a payload too large for a SOCKS UDP datagram")
	}
	r.source = bytes.NewReader([]byte{0, 3, 1})
	if _, err := r.read(); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal("truncated frame was not rejected", err)
	}
}

func TestProxyUDPLargeAndEmptyDatagrams(t *testing.T) {
	proxy, _, _ := startProxyFixture(t)
	echo, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		buf := make([]byte, maxPacket)
		for {
			n, addr, err := echo.ReadFromUDPAddrPort(buf)
			if err != nil {
				return
			}
			_, _ = echo.WriteToUDPAddrPort(buf[:n], addr)
		}
	}()
	c := authProxy(t, proxy, "password")
	defer c.Close()
	_, _ = c.Write(requestAddress(3, "0.0.0.0:0"))
	var reply [10]byte
	if _, err = io.ReadFull(c, reply[:]); err != nil || reply[1] != 0 {
		t.Fatal("association failed", err, reply)
	}
	u, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IP(reply[4:8]), Port: int(binary.BigEndian.Uint16(reply[8:]))})
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	header, err := udpResponseHeader(echo.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, maxPacket)
	for _, size := range []int{32, maxPacket - len(header), 0, 32} {
		payload := bytes.Repeat([]byte{byte(size)}, size)
		packet := append(append([]byte(nil), header...), payload...)
		_ = u.SetDeadline(time.Now().Add(3 * time.Second))
		if _, err = u.Write(packet); err != nil {
			t.Fatal(err)
		}
		n, err := u.Read(buf)
		if err != nil || !bytes.Equal(buf[:n], packet) {
			t.Fatalf("UDP payload %d bytes was corrupted: got %d, %v", size, n, err)
		}
	}
}

func TestQueuedWakeCannotBlockManagerShutdown(t *testing.T) {
	cfg := defaults()
	cfg.DataDir = t.TempDir()
	m := newManager(cfg, "")
	m.wake <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() { m.run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("initial wake blocked behind an already queued action")
	}
}

func TestNodeRefreshCoalescesAndExplicitRefreshBypassesCache(t *testing.T) {
	var requests atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		once.Do(func() { close(entered) })
		select {
		case <-release:
			_, _ = w.Write(benchmarkNodes(2))
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg := defaults()
	cfg.DataDir = t.TempDir()
	cfg.Sources = []string{server.URL}
	m := newManager(cfg, "")
	defer m.sourceClient.CloseIdleConnections()
	results := make(chan []Node, 16)
	go func() { results <- m.refresh(ctx, cfg, false) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("node source was not queried")
	}
	for i := 0; i < 15; i++ {
		go func() { results <- m.refresh(ctx, cfg, false) }()
	}
	close(release)
	for i := 0; i < 16; i++ {
		if nodes := <-results; len(nodes) != 2 {
			t.Fatal("refresh returned no validated nodes")
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("concurrent reconnects made %d downloads", requests.Load())
	}
	if len(m.refresh(ctx, cfg, true)) != 2 || requests.Load() != 2 {
		t.Fatal("explicit refresh did not bypass the node cache")
	}
	m.mu.Lock()
	m.nodesUpdated = time.Now().Add(-2 * time.Minute)
	m.mu.Unlock()
	if len(m.refresh(ctx, cfg, false)) != 2 || requests.Load() != 3 {
		t.Fatal("expired node cache was not refreshed")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testHTTPResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

type socketTunnel struct {
	*fakeTunnel
	path string
}

func (s *socketTunnel) socket() string { return s.path }

func TestProbeHTTPReuseAndSourceChange(t *testing.T) {
	_, access, _ := startProxyFixture(t)
	var connections, probes, metadata atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probes.Add(1)
		_, _ = io.WriteString(w, "203.0.113.5")
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.StartTLS()
	defer server.Close()
	cfg := defaults()
	cfg.ProbeURL = server.URL
	m := newManager(cfg, "")
	defer m.sourceClient.CloseIdleConnections()
	m.hostClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == cfg.ProbeURL {
			return testHTTPResponse("203.0.113.6"), nil
		}
		metadata.Add(1)
		return testHTTPResponse(`{"country_code":"JP","hosting":false}`), nil
	})
	active := &socketTunnel{fakeTunnel: &fakeTunnel{}, path: access.socket}
	client := tunnelClient(active)
	client.Transport.(*http.Transport).TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	defer client.CloseIdleConnections()
	for i := 0; i < 3; i++ {
		if _, err := m.probe(context.Background(), cfg, active, client); err != nil {
			t.Fatal(err)
		}
	}
	if connections.Load() != 1 || probes.Load() != 3 || metadata.Load() != 1 {
		t.Fatalf("connection/probe/geo counts: %d/%d/%d", connections.Load(), probes.Load(), metadata.Load())
	}
	cfg.GeoURL = "https://other.example/geo?ip=%s"
	if _, err := m.probe(context.Background(), cfg, active, client); err != nil || metadata.Load() != 2 {
		t.Fatal("new metadata source reused the previous provider's cache", err)
	}
}

func TestGeoCacheHasHardCapacityWithoutExpiredEntries(t *testing.T) {
	cfg := defaults()
	m := newManager(cfg, "")
	defer m.sourceClient.CloseIdleConnections()
	m.hostClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == cfg.ProbeURL {
			return testHTTPResponse("203.0.113.254"), nil
		}
		return testHTTPResponse(`{"country_code":"JP","hosting":false}`), nil
	})
	ip := ""
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return testHTTPResponse(ip), nil })}
	for i := 1; i <= 200; i++ {
		ip = "203.0.113." + strconv.Itoa(i)
		if _, err := m.probe(context.Background(), cfg, &fakeTunnel{}, client); err != nil {
			t.Fatal(err)
		}
	}
	if len(m.geoCache) != 128 {
		t.Fatalf("geo cache retained %d fresh entries", len(m.geoCache))
	}
}

func TestProcessExitIsBroadcastAndCleanupDoesNotWaitAgain(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^$")
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done, exited := make(chan error, 1), make(chan error, 1)
	cleaned := false
	go waitProcess(cmd, done, exited, func() { cleaned = true })
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		t.Fatal("process exit was not reported to the monitor")
	}
	<-done
	if !cleaned {
		t.Fatal("process exit was reported before output cleanup")
	}
	finished := make(chan struct{})
	go func() { stopProcess(cmd, done); close(finished) }()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("cleanup waited for an already consumed process exit")
	}
}
