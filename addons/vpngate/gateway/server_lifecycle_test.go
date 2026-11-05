package main

import (
	"context"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestProxyShutdownClosesIncompleteHandshake(t *testing.T) {
	for _, mode := range []string{"cancel", "listener-failure"} {
		t.Run(mode, func(t *testing.T) { checkProxyShutdown(t, mode == "listener-failure") })
	}
}

func checkProxyShutdown(t *testing.T, listenerFailure bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	access := &testAccess{ready: true, conns: map[net.Conn]bool{}}
	done := make(chan error, 1)
	go func() { done <- serveProxy(ctx, ln, access) }()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	c.Write([]byte{5, 1, 2})
	var reply [2]byte
	if _, err := io.ReadFull(c, reply[:]); err != nil {
		t.Fatal(err)
	}
	if listenerFailure {
		ln.Close()
	} else {
		cancel()
	}
	select {
	case err := <-done:
		if (err != nil) != listenerFailure {
			t.Fatalf("proxy stop error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("proxy did not stop")
	}
	access.mu.Lock()
	remaining := len(access.conns)
	for c := range access.conns {
		c.Close()
	}
	access.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("stopped proxy retained %d incomplete handshakes", remaining)
	}
}

func TestWorkerShutdownWaitsForPendingDial(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	w := &worker{socket: filepath.Join(t.TempDir(), "w.sock"), dialOverride: func(ctx context.Context, _, _ string) (net.Conn, error) {
		close(entered)
		<-ctx.Done()
		<-release
		return nil, ctx.Err()
	}}
	done := make(chan error, 1)
	go func() { done <- w.serve(ctx) }()
	var c net.Conn
	var err error
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c, err = net.Dial("unix", w.socket)
		if err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(deadline)
	if err := writeObject(c, dialRequest{Network: "tcp", Address: "203.0.113.5:80"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("worker dial did not start")
	}
	cancel()
	select {
	case err := <-done:
		t.Fatalf("worker returned before its pending dial finished: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	// Unblock the dial without closing release twice in the deferred cleanup.
	release <- struct{}{}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not finish after its dial returned")
	}
}
