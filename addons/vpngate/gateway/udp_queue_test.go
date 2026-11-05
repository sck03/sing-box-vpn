package main

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestUDPQueueCapacityAndCancel(t *testing.T) {
	_, access, _ := startProxyFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	delayed := &delayedUDPAccess{proxyAccess: access, entered: make(chan string, 4), finished: make(chan string, 4), release: make(chan struct{})}
	socket, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	u := &udpAssociation{ctx: ctx, access: delayed, ticket: ticket{epoch: 1, country: "JP"}, socket: socket,
		sessions: map[string]*udpSession{}, headers: map[string]*udpSession{}}
	client := netip.MustParseAddrPort("127.0.0.1:40001")
	large := udpTestPacket("slow.example:53", strings.Repeat("x", 64000))
	offset, err := udpHeaderSize(large)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		u.dispatch(large, offset, client)
	}
	select {
	case <-delayed.entered:
	case <-time.After(time.Second):
		t.Fatal("pending target did not start")
	}
	u.mu.Lock()
	if u.queuedBytes > 128<<10 || u.queuedBytes == 0 {
		t.Errorf("one target monopolized the association's buffered capacity: %d", u.queuedBytes)
	}
	u.mu.Unlock()
	cancel()
	u.close()
	done := make(chan struct{})
	go func() { u.workers.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled target retained its queued buffers")
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.queuedBytes != 0 || len(u.sessions) != 0 || len(u.headers) != 0 {
		t.Fatal("cancelled association retained capacity or cached sessions")
	}
}
