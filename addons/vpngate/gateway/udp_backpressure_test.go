package main

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

type blockedUDPWriter struct {
	net.Conn
	started, done chan struct{}
	once          sync.Once
	finished      sync.Once
}

func (c *blockedUDPWriter) Write(b []byte) (int, error) {
	c.once.Do(func() { close(c.started) })
	n, err := c.Conn.Write(b)
	c.finished.Do(func() { close(c.done) })
	return n, err
}

type blockedUDPAccess struct {
	proxyAccess
	connection *blockedUDPWriter
}

func (a *blockedUDPAccess) open(ctx context.Context, t ticket, network, address string) (net.Conn, string, error) {
	if strings.HasPrefix(address, "blocked") {
		a.track(a.connection)
		return a.connection, "203.0.113.5:53", nil
	}
	return a.proxyAccess.open(ctx, t, network, address)
}

func TestBlockedUDPWriteDoesNotStallOtherPorts(t *testing.T) {
	server, peers := udpLifecycleEcho(t)
	_, access, _ := startProxyFixture(t)
	bridge, peer := net.Pipe()
	t.Cleanup(func() { bridge.Close(); peer.Close() })
	blocked := &blockedUDPWriter{Conn: bridge, started: make(chan struct{}), done: make(chan struct{})}
	control, client := udpAssociationFixture(t, &blockedUDPAccess{proxyAccess: access, connection: blocked})
	healthy := server.LocalAddr().String()
	_, _ = client.Write(udpTestPacket(healthy, "before"))
	receiveUDPPayload(t, client, "before")
	first := <-peers
	_, _ = client.Write(udpTestPacket("blocked.example:53", "first"))
	select {
	case <-blocked.started:
	case <-time.After(time.Second):
		t.Fatal("blocked writer did not start")
	}
	_, _ = client.Write(udpTestPacket("blocked.example:53", "second"))
	_, _ = client.Write(udpTestPacket(healthy, "after"))
	receiveUDPPayload(t, client, "after")
	if next := <-peers; next != first {
		t.Fatal("healthy target changed its source port")
	}
	control.Close()
	select {
	case <-blocked.done:
	case <-time.After(time.Second):
		t.Fatal("closed association retained a blocked writer")
	}
}
