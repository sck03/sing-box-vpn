package main

import (
	"context"
	"errors"
	"io"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type openResult struct {
	conn net.Conn
	err  error
}

func pendingDialFixture(t *testing.T) (*manager, ticket, <-chan net.Conn) {
	t.Helper()
	cfg := defaults()
	cfg.DataDir = t.TempDir()
	cfg.MaxConnections = 1
	m := newManager(cfg, "")
	path := filepath.Join(t.TempDir(), "w.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	requests := make(chan net.Conn, 16)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = serveConnections(ctx, ln, nil, func(ctx context.Context, c net.Conn) {
			if _, err := readFrame(c, 4096); err == nil {
				select {
				case requests <- c:
				case <-ctx.Done():
				}
				<-ctx.Done()
			}
		})
	}()
	t.Cleanup(func() { m.revoke(); cancel(); <-done; m.sourceClient.CloseIdleConnections() })
	m.current = &backend{workerSocket: path}
	m.ready = true
	ticket, _ := m.authenticate(cfg.ProxyUser, cfg.ProxyPass)
	return m, ticket, requests
}

func beginManagerOpen(t *testing.T, m *manager, ticket ticket, network string) (<-chan openResult, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done, finished := make(chan openResult, 1), make(chan struct{})
	go func() {
		defer close(finished)
		c, _, err := m.open(ctx, ticket, network, "203.0.113.5:443")
		done <- openResult{c, err}
	}()
	t.Cleanup(func() { cancel(); <-finished })
	return done, cancel
}

func awaitOpen(t *testing.T, done <-chan openResult) openResult {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(time.Second):
		t.Fatal("manager kept waiting for an unavailable connection slot or revoked dial")
		return openResult{}
	}
}

func awaitDial(t *testing.T, requests <-chan net.Conn) net.Conn {
	t.Helper()
	select {
	case c := <-requests:
		return c
	case <-time.After(3 * time.Second):
		t.Fatal("worker request did not arrive")
		return nil
	}
}

func TestManagerPendingDialUsesConnectionCapacity(t *testing.T) {
	for _, network := range []string{"tcp", "udp"} {
		t.Run(network, func(t *testing.T) {
			m, ticket, requests := pendingDialFixture(t)
			first, _ := beginManagerOpen(t, m, ticket, network)
			worker := awaitDial(t, requests)
			incoming, peer := net.Pipe()
			defer incoming.Close()
			defer peer.Close()
			if m.track(incoming) {
				m.untrack(incoming)
				t.Error("incoming connection stole the pending dial's capacity")
			}
			second, _ := beginManagerOpen(t, m, ticket, network)
			if result := awaitOpen(t, second); result.err == nil || result.conn != nil {
				t.Fatal("capacity overflow was accepted")
			}
			if err := writeObject(worker, dialResponse{Address: "203.0.113.5:443"}); err != nil {
				t.Fatal(err)
			}
			result := awaitOpen(t, first)
			if result.err != nil {
				t.Fatal("reserved dial could not become an active connection", result.err)
			}
			defer result.conn.Close()
			if m.track(incoming) {
				t.Fatal("active connection lost its reserved capacity")
			}
			m.untrack(result.conn)
			if !m.track(incoming) {
				t.Fatal("completed dial retained a second slot")
			}
		})
	}
}

func TestManagerRevocationCancelsPendingDial(t *testing.T) {
	m, ticket, requests := pendingDialFixture(t)
	done, _ := beginManagerOpen(t, m, ticket, "tcp")
	worker := awaitDial(t, requests)
	m.revoke()
	result := awaitOpen(t, done)
	if result.err == nil || result.conn != nil {
		t.Fatal("revoked dial succeeded")
	}
	_ = worker.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := worker.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatal("revoked dial retained its worker connection", err)
	}
	m.mu.Lock()
	m.ready = true
	m.mu.Unlock()
	if m.allowed(ticket) {
		t.Fatal("old authentication survived revocation")
	}
	fresh, _ := m.authenticate(m.cfg.ProxyUser, m.cfg.ProxyPass)
	next, _ := beginManagerOpen(t, m, fresh, "udp")
	worker = awaitDial(t, requests)
	if err := writeObject(worker, dialResponse{Address: "203.0.113.5:443"}); err != nil {
		t.Fatal(err)
	}
	if result := awaitOpen(t, next); result.err != nil {
		t.Fatal("revocation did not release dial capacity", result.err)
	}
}

func TestManagerFailedDialReleasesCapacity(t *testing.T) {
	for _, failure := range []string{"cancel", "worker-error", "invalid-reply", "disconnect"} {
		t.Run(failure, func(t *testing.T) {
			m, ticket, requests := pendingDialFixture(t)
			done, cancel := beginManagerOpen(t, m, ticket, "tcp")
			worker := awaitDial(t, requests)
			switch failure {
			case "cancel":
				cancel()
			case "worker-error":
				_ = writeObject(worker, dialResponse{Error: "unreachable"})
			case "invalid-reply":
				_ = writeFrame(worker, []byte("broken JSON"))
			case "disconnect":
				worker.Close()
			}
			if result := awaitOpen(t, done); result.err == nil || result.conn != nil {
				t.Fatal("failed dial returned a connection")
			}
			incoming, peer := net.Pipe()
			defer incoming.Close()
			defer peer.Close()
			if !m.track(incoming) {
				t.Fatal("failed dial retained capacity")
			}
		})
	}
}

func TestManagerConcurrentDialsRespectCapacity(t *testing.T) {
	m, ticket, requests := pendingDialFixture(t)
	m.cfg.MaxConnections = 4
	ctx, cancel := context.WithCancel(context.Background())
	var callers sync.WaitGroup
	done := make(chan openResult, 32)
	t.Cleanup(func() { cancel(); callers.Wait() })
	for i := 0; i < cap(done); i++ {
		callers.Go(func() {
			c, _, err := m.open(ctx, ticket, "udp", "203.0.113.5:443")
			done <- openResult{c, err}
		})
	}
	for i := 0; i < cap(done)-m.cfg.MaxConnections; i++ {
		if result := awaitOpen(t, done); result.err == nil || result.conn != nil {
			t.Fatal("concurrent dial exceeded capacity")
		}
	}
	for i := 0; i < m.cfg.MaxConnections; i++ {
		if err := writeObject(awaitDial(t, requests), dialResponse{Address: "203.0.113.5:443"}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < m.cfg.MaxConnections; i++ {
		if result := awaitOpen(t, done); result.err != nil || result.conn == nil {
			t.Fatal("concurrent reserved dial was rejected", result.err)
		}
	}
	select {
	case <-requests:
		t.Fatal("rejected caller still opened a worker connection")
	default:
	}
}

func BenchmarkManagerOpenAtCapacity(b *testing.B) {
	cfg := defaults()
	cfg.DataDir = b.TempDir()
	cfg.MaxConnections = 1
	m := newManager(cfg, "")
	defer m.sourceClient.CloseIdleConnections()
	m.current = &backend{workerSocket: filepath.Join(b.TempDir(), "absent.sock")}
	m.ready = true
	c, peer := net.Pipe()
	defer c.Close()
	defer peer.Close()
	m.track(c)
	ticket, _ := m.authenticate(cfg.ProxyUser, cfg.ProxyPass)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if c, _, err := m.open(context.Background(), ticket, "tcp", "203.0.113.5:443"); err == nil || c != nil {
			b.Fatal("capacity overflow was accepted")
		}
	}
}
