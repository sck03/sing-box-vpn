package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

type delayedUDPAccess struct {
	proxyAccess
	entered  chan string
	finished chan string
	release  chan struct{}
	target   string
}

func (a *delayedUDPAccess) open(ctx context.Context, t ticket, network, address string) (net.Conn, string, error) {
	if strings.HasPrefix(address, "slow") {
		a.entered <- address
		defer func() { a.finished <- address }()
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		case <-a.release:
		}
		if a.target == "" {
			return nil, "", errors.New("test DNS lookup failed")
		}
		address = a.target
	}
	return a.proxyAccess.open(ctx, t, network, address)
}

func delayedUDPFixture(t *testing.T, target string) (net.Conn, *net.UDPConn, *delayedUDPAccess) {
	t.Helper()
	_, access, _ := startProxyFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	delayed := &delayedUDPAccess{proxyAccess: access, entered: make(chan string, 32), finished: make(chan string, 32), release: make(chan struct{}), target: target}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go serveProxy(ctx, listener, delayed)
	control := authProxy(t, listener.Addr().String(), "password")
	t.Cleanup(func() { control.Close() })
	_, _ = control.Write(requestAddress(3, "0.0.0.0:0"))
	var reply [10]byte
	if _, err = io.ReadFull(control, reply[:]); err != nil || reply[1] != 0 {
		t.Fatal("UDP association failed", err, reply)
	}
	client, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IP(reply[4:8]), Port: int(binary.BigEndian.Uint16(reply[8:]))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return control, client, delayed
}

func udpTestPacket(target, payload string) []byte {
	host, port, _ := net.SplitHostPort(target)
	value, _ := strconv.Atoi(port)
	var header []byte
	if ip := net.ParseIP(host).To4(); ip != nil {
		header = append([]byte{0, 0, 0, 1}, ip...)
	} else {
		header = append([]byte{0, 0, 0, 3, byte(len(host))}, host...)
	}
	header = binary.BigEndian.AppendUint16(header, uint16(value))
	return append(header, payload...)
}

func udpLifecycleEcho(t testing.TB) (*net.UDPConn, <-chan string) {
	t.Helper()
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	peers := make(chan string, 32)
	go func() {
		var buf [2048]byte
		for {
			n, from, err := server.ReadFromUDPAddrPort(buf[:])
			if err != nil {
				return
			}
			select {
			case peers <- from.String():
			default:
			}
			_, _ = server.WriteToUDPAddrPort(buf[:n], from)
		}
	}()
	return server, peers
}

func receiveUDPPayload(t *testing.T, client *net.UDPConn, want string) {
	t.Helper()
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	var buf [2048]byte
	n, err := client.Read(buf[:])
	if err != nil || n < 10 || string(buf[10:n]) != want {
		t.Fatalf("UDP reply %q: received %d bytes, %v", want, n, err)
	}
}

func TestSlowUDPResolutionDoesNotStallExistingTarget(t *testing.T) {
	server, peers := udpLifecycleEcho(t)
	_, client, delayed := delayedUDPFixture(t, "")
	defer close(delayed.release)
	address := server.LocalAddr().String()
	_, _ = client.Write(udpTestPacket(address, "before"))
	receiveUDPPayload(t, client, "before")
	first := <-peers
	_, _ = client.Write(udpTestPacket("slow.example:53", "waiting"))
	select {
	case <-delayed.entered:
	case <-time.After(time.Second):
		t.Fatal("slow lookup did not start")
	}
	_, _ = client.Write(udpTestPacket(address, "after"))
	receiveUDPPayload(t, client, "after")
	if next := <-peers; next != first {
		t.Fatalf("existing UDP target changed source port: %s -> %s", first, next)
	}
}

func TestUDPResolutionPreservesQueuedPackets(t *testing.T) {
	server, peers := udpLifecycleEcho(t)
	_, client, delayed := delayedUDPFixture(t, server.LocalAddr().String())
	for _, payload := range []string{"first", "second", "third"} {
		_, _ = client.Write(udpTestPacket("slow.example:53", payload))
	}
	select {
	case <-delayed.entered:
	case <-time.After(time.Second):
		t.Fatal("lookup did not start")
	}
	close(delayed.release)
	for _, payload := range []string{"first", "second", "third"} {
		receiveUDPPayload(t, client, payload)
	}
	first := <-peers
	if second, third := <-peers, <-peers; first != second || first != third {
		t.Fatal("queued datagrams created separate target sessions")
	}
}

func TestClosingUDPAssociationCancelsParallelLookups(t *testing.T) {
	control, client, delayed := delayedUDPFixture(t, "")
	for _, target := range []string{"slow-one.example:53", "slow-two.example:53", "slow-three.example:53"} {
		_, _ = client.Write(udpTestPacket(target, "waiting"))
	}
	for i := 0; i < 3; i++ {
		select {
		case <-delayed.entered:
		case <-time.After(time.Second):
			t.Fatal("new targets did not resolve independently")
		}
	}
	control.Close()
	for i := 0; i < 3; i++ {
		select {
		case <-delayed.finished:
		case <-time.After(time.Second):
			t.Fatal("closed association retained a pending lookup")
		}
	}
}

func TestPendingUDPDialsStayBounded(t *testing.T) {
	server, _ := udpLifecycleEcho(t)
	control, client, delayed := delayedUDPFixture(t, "")
	address := server.LocalAddr().String()
	_, _ = client.Write(udpTestPacket(address, "before"))
	receiveUDPPayload(t, client, "before")
	for i := 0; i < 32; i++ {
		_, _ = client.Write(udpTestPacket(fmt.Sprintf("slow-%d.example:53", i), "waiting"))
	}
	_, _ = client.Write(udpTestPacket(address, "marker"))
	receiveUDPPayload(t, client, "marker")
	for i := 0; i < 8; i++ {
		select {
		case <-delayed.entered:
		case <-time.After(time.Second):
			t.Fatal("pending targets did not start")
		}
	}
	select {
	case <-delayed.entered:
		t.Fatal("association started more than eight simultaneous lookups")
	case <-time.After(50 * time.Millisecond):
	}
	control.Close()
	for i := 0; i < 8; i++ {
		select {
		case <-delayed.finished:
		case <-time.After(time.Second):
			t.Fatal("association did not release its pending dial capacity")
		}
	}
}

func TestPendingUDPPacketQueueStaysBounded(t *testing.T) {
	server, _ := udpLifecycleEcho(t)
	_, client, delayed := delayedUDPFixture(t, server.LocalAddr().String())
	for i := 0; i < 96; i++ {
		_, _ = client.Write(udpTestPacket("slow.example:53", fmt.Sprintf("queued-%d", i)))
	}
	_, _ = client.Write(udpTestPacket(server.LocalAddr().String(), "marker"))
	receiveUDPPayload(t, client, "marker")
	close(delayed.release)
	for i := 0; i < 64; i++ {
		receiveUDPPayload(t, client, fmt.Sprintf("queued-%d", i))
	}
	_ = client.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	var buf [2048]byte
	if _, err := client.Read(buf[:]); err == nil {
		t.Fatal("pending target retained more than 64 datagrams")
	}
}

func BenchmarkWorkerUDPSession(b *testing.B) {
	server, _ := udpLifecycleEcho(b)
	w := &worker{dialOverride: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
	request := dialRequest{Network: "udp", Address: server.LocalAddr().String()}
	packet := append([]byte{0, 0}, bytes.Repeat([]byte{'x'}, 1400)...)
	for b.Loop() {
		bridge, client := net.Pipe()
		done := make(chan struct{})
		go func() { defer close(done); defer bridge.Close(); w.handle(context.Background(), bridge) }()
		if err := writeObject(client, request); err != nil {
			b.Fatal(err)
		}
		if _, err := readFrame(client, 4096); err != nil {
			b.Fatal(err)
		}
		if err := writePacket(client, packet); err != nil {
			b.Fatal(err)
		}
		if _, err := readFrame(client, maxPacket); err != nil {
			b.Fatal(err)
		}
		client.Close()
		<-done
	}
}
