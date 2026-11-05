package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartupUsesSavedNodeWithoutWaitingForSource(t *testing.T) {
	for _, mode := range []string{"sticky", "fixed"} {
		t.Run(mode, func(t *testing.T) {
			cfg := defaults()
			cfg.Enabled, cfg.Mode = true, mode
			cfg.DataDir, cfg.RunDir = t.TempDir(), t.TempDir()
			cfg.ProxyUser, cfg.ProxyPass = "test", strings.Repeat("a", 64)
			node := Node{Host: "saved-jp", IP: "203.0.113.5", Country: "JP", Profile: safeTestProfile}
			if err := atomicJSON(filepath.Join(cfg.DataDir, "last-node.json"), node); err != nil {
				t.Fatal(err)
			}
			m := newManager(cfg, filepath.Join(cfg.DataDir, "gateway.json"))
			m.sourceClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				<-r.Context().Done()
				return nil, r.Context().Err()
			})
			m.factory = func(Config) tunnel { return &fakeTunnel{dead: make(chan error)} }
			m.probeOverride = func(_ context.Context, c Config, _ tunnel) (Geo, error) {
				g := Geo{IP: node.IP, Country: node.Country}
				return g, c.acceptGeo(g)
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { defer close(done); m.run(ctx) }()
			defer func() { cancel(); <-done }()
			deadline := time.Now().Add(time.Second)
			for time.Now().Before(deadline) {
				if m.snapshot().State == "ready" {
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatal("saved node waited for an unavailable node source before connecting")
		})
	}
}

func TestSavedNodeStillRequiresExitVerification(t *testing.T) {
	cfg := defaults()
	cfg.Enabled = true
	cfg.DataDir, cfg.RunDir = t.TempDir(), t.TempDir()
	m := newManager(cfg, "")
	m.previous = Node{Host: "saved-jp", IP: "203.0.113.5", Country: "JP", Profile: safeTestProfile}
	m.sourceClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	var probes atomic.Int32
	m.factory = func(Config) tunnel { return &fakeTunnel{dead: make(chan error)} }
	m.probeOverride = func(context.Context, Config, tunnel) (Geo, error) {
		probes.Add(1)
		return Geo{}, errors.New("exit country does not match")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	m.run(ctx)
	if probes.Load() == 0 {
		t.Fatal("saved node was never verified")
	}
	if m.ready || m.current != nil {
		t.Fatal("failed exit verification left the saved tunnel available")
	}
}

func TestCooldownRetainsLastTunnelFailure(t *testing.T) {
	cfg := defaults()
	cfg.Enabled = true
	cfg.DataDir, cfg.RunDir = t.TempDir(), t.TempDir()
	node := Node{Host: "saved-jp", IP: "203.0.113.5", Country: "JP", Profile: safeTestProfile}
	m := newManager(cfg, "")
	m.previous, m.nodes, m.nodesUpdated = node, []Node{node}, time.Now()
	m.factory = func(Config) tunnel { return &fakeTunnel{dead: make(chan error)} }
	m.probeOverride = func(_ context.Context, c Config, _ tunnel) (Geo, error) {
		g := Geo{IP: node.IP, Country: "KR"}
		return g, c.acceptGeo(g)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); m.run(ctx) }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		status := m.snapshot()
		if status.State == "waiting" && strings.HasPrefix(status.Reason, "no valid candidate") {
			if !strings.Contains(status.Reason, "does not match JP") {
				t.Fatal("cooldown hid the exit verification failure", status.Reason)
			}
			if status.Exit != nil {
				t.Fatal("rejected exit was still advertised")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("failed cached candidate did not enter cooldown")
}

func TestStartupRetriesTransientVerificationOnSameTunnel(t *testing.T) {
	cfg := defaults()
	cfg.Enabled = true
	cfg.DataDir, cfg.RunDir = t.TempDir(), t.TempDir()
	m := newManager(cfg, "")
	m.previous = Node{Host: "saved-jp", IP: "203.0.113.5", Country: "JP", Profile: safeTestProfile}
	m.sourceClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	var starts, probes atomic.Int32
	m.factory = func(Config) tunnel {
		starts.Add(1)
		return &fakeTunnel{dead: make(chan error)}
	}
	verified := make(chan struct{})
	m.probeOverride = func(context.Context, Config, tunnel) (Geo, error) {
		if probes.Add(1) == 1 {
			return Geo{}, probeUnavailable("temporary exit lookup failure")
		}
		close(verified)
		return Geo{IP: "203.0.113.5", Country: "JP"}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); m.run(ctx) }()
	defer func() { cancel(); <-done }()
	select {
	case <-verified:
	case <-time.After(4 * time.Second):
		t.Fatal("startup did not retry transient verification")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if m.snapshot().State == "ready" {
			if starts.Load() != 1 {
				t.Fatal("temporary verification failure recreated the tunnel")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("successful startup retry could not enable forwarding")
}

func TestReconnectCooldownDoesNotPreventFixedNodeRetry(t *testing.T) {
	cfg := defaults()
	node := Node{Host: "saved-jp", IP: "203.0.113.5", Country: "JP", Profile: safeTestProfile}
	m := newManager(cfg, "")
	defer m.sourceClient.CloseIdleConnections()
	m.previous = node
	m.failed[node.Host] = time.Now().Add(time.Minute)
	if pool := m.reconnectCandidates(cfg, []Node{node}, false); len(pool) != 0 {
		t.Fatal("sticky mode retried a node still in cooldown")
	}
	cfg.Mode, cfg.FixedHost = "fixed", node.Host
	if pool := m.reconnectCandidates(cfg, []Node{node}, false); len(pool) != 1 {
		t.Fatal("fixed mode lost its selected node to cooldown")
	}
	cfg.Mode = "sticky"
	m.failed[node.Host] = time.Now().Add(-time.Second)
	if pool := m.reconnectCandidates(cfg, []Node{node}, false); len(pool) != 1 || len(m.failed) != 0 {
		t.Fatal("expired cooldown prevented reconnection or retained state")
	}
}

func TestRevokedTunnelCannotAppearReady(t *testing.T) {
	cfg := defaults()
	cfg.DataDir = t.TempDir()
	m := newManager(cfg, "")
	defer m.sourceClient.CloseIdleConnections()
	m.ready = true
	m.setStatus("ready", "verified", Node{Host: "saved-jp"}, &Geo{IP: "203.0.113.5", Country: "JP"})
	m.revoke()
	// A slow status write from the old connection must not advertise a revoked exit.
	m.setStatus("ready", "verified", Node{Host: "saved-jp"}, &Geo{IP: "203.0.113.5", Country: "JP"})
	if status := m.snapshot(); status.State == "ready" || status.Exit != nil {
		t.Fatal("revoked forwarding was reported as a live verified exit", status)
	}
}

func BenchmarkReconnectCandidates(b *testing.B) {
	nodes, err := parseNodes(bytes.NewReader(benchmarkNodes(1500)))
	if err != nil {
		b.Fatal(err)
	}
	cfg := defaults()
	m := newManager(cfg, "")
	defer m.sourceClient.CloseIdleConnections()
	m.previous = nodes[0]
	m.failed[nodes[0].Host] = time.Now().Add(time.Hour)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pool := m.reconnectCandidates(cfg, nodes, false)
		if len(pool) != 6 || pool[0].Host == nodes[0].Host {
			b.Fatal("failed node was selected or candidate limit changed")
		}
	}
}
