package main

import (
	"bytes"
	"fmt"
	"net"
	"net/netip"
	"testing"
	"time"
)

// A valid IPv4 datagram can be too large after adding the SOCKS header. Drop
// only that reply, then preserve framing and the target's original source port.
func TestUDPReplySizeDoesNotResetSession(t *testing.T) {
	_, access, _ := startProxyFixture(t)
	_, client := udpAssociationFixture(t, access)
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	sizes := []int{maxPacket - 9, maxPacket, maxPacket - 10, 0, 1400}
	done := make(chan error, 1)
	go func() {
		var first netip.AddrPort
		var request [1]byte
		for i, size := range sizes {
			_ = server.SetDeadline(time.Now().Add(3 * time.Second))
			_, peer, err := server.ReadFromUDPAddrPort(request[:])
			if err != nil {
				done <- err
				return
			}
			if first.IsValid() && first != peer {
				done <- fmt.Errorf("target source port changed: %s -> %s", first, peer)
				return
			}
			first = peer
			for _, reply := range [][]byte{bytes.Repeat([]byte{byte(i)}, size), []byte("marker")} {
				if _, err := server.WriteToUDPAddrPort(reply, peer); err != nil {
					done <- err
					return
				}
			}
		}
		done <- nil
	}()
	buffer := make([]byte, maxPacket)
	for i, size := range sizes {
		if _, err := client.Write(udpTestPacket(server.LocalAddr().String(), "x")); err != nil {
			t.Fatal(err)
		}
		_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
		if size <= maxPacket-10 {
			n, err := client.Read(buffer)
			if err != nil || n != size+10 || !bytes.Equal(buffer[10:n], bytes.Repeat([]byte{byte(i)}, size)) {
				t.Fatalf("reply size %d: received %d bytes, %v", size, n, err)
			}
		}
		n, err := client.Read(buffer)
		if err != nil || n != 16 || string(buffer[10:n]) != "marker" {
			t.Fatalf("reply size %d disrupted following datagram: received %d bytes, %v", size, n, err)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
