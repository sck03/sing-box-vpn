package main

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

type pendingUDPAccess struct {
	*testAccess
	entered chan context.Context
}

func (a *pendingUDPAccess) open(ctx context.Context, _ ticket, _, _ string) (net.Conn, string, error) {
	a.entered <- ctx
	<-ctx.Done()
	return nil, "", ctx.Err()
}

func TestUDPControlCloseCancelsPendingDial(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	a := &pendingUDPAccess{testAccess: &testAccess{ready: true, conns: map[net.Conn]bool{}}, entered: make(chan context.Context, 1)}
	go func() { _ = serveProxy(ctx, ln, a) }()
	control := authProxy(t, ln.Addr().String(), "password")
	defer control.Close()
	_, _ = control.Write(requestAddress(3, "0.0.0.0:0"))
	var reply [10]byte
	if _, err = io.ReadFull(control, reply[:]); err != nil || reply[1] != 0 {
		t.Fatal("UDP association failed", err, reply)
	}
	udp, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IP(reply[4:8]), Port: int(binary.BigEndian.Uint16(reply[8:]))})
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	header, err := udpResponseHeader("203.0.113.5:53")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = udp.Write(append(header, 1)); err != nil {
		t.Fatal(err)
	}
	var dialContext context.Context
	select {
	case dialContext = <-a.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("UDP target dial did not start")
	}
	control.Close()
	select {
	case <-dialContext.Done():
	case <-time.After(time.Second):
		t.Fatal("closed UDP association retained its pending dial")
	}
}

func TestTunnelDeathInterruptsHealthProbe(t *testing.T) {
	cfg := defaults()
	cfg.HealthInterval = 1
	m := newManager(cfg, "")
	defer m.sourceClient.CloseIdleConnections()
	active := &fakeTunnel{dead: make(chan error)}
	entered := make(chan struct{})
	m.probeOverride = func(ctx context.Context, _ Config, _ tunnel) (Geo, error) {
		close(entered)
		<-ctx.Done()
		return Geo{}, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- m.monitor(ctx, cfg, active, Node{}, nil) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("health probe did not start")
	}
	close(active.dead)
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "process stopped") {
			t.Fatal("tunnel exit reason was lost", err)
		}
	case <-time.After(time.Second):
		t.Fatal("tunnel exit waited for a blocked health probe")
	}
}

func TestTunnelDeathInterruptsStartupProbe(t *testing.T) {
	cfg := defaults()
	cfg.Enabled = true
	cfg.DataDir, cfg.RunDir = t.TempDir(), t.TempDir()
	m := newManager(cfg, "")
	m.previous = Node{Host: "saved-jp", IP: "203.0.113.5", Country: "JP", Profile: safeTestProfile}
	m.sourceClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	active := &fakeTunnel{dead: make(chan error)}
	m.factory = func(Config) tunnel { return active }
	entered, stopped := make(chan struct{}), make(chan struct{})
	m.probeOverride = func(ctx context.Context, _ Config, _ tunnel) (Geo, error) {
		close(entered)
		<-ctx.Done()
		close(stopped)
		return Geo{}, context.Cause(ctx)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); m.run(ctx) }()
	defer func() { cancel(); <-done }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("startup probe did not begin")
	}
	close(active.dead)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("dead tunnel retained its blocked startup probe")
	}
	if m.snapshot().State == "ready" {
		t.Fatal("dead tunnel enabled forwarding")
	}
}
