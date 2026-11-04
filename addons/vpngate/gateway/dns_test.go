package main

import (
	"context"
	"encoding/binary"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// Return one A record, or a truncated UDP answer that requires TCP retry.
func dnsTestAnswer(query []byte, truncated bool) []byte {
	if len(query) < 12 || binary.BigEndian.Uint16(query[4:6]) != 1 {
		return nil
	}
	end := 12
	for end < len(query) && query[end] != 0 {
		if query[end] > 63 {
			return nil
		}
		end += 1 + int(query[end])
	}
	end += 5 // Root label, QTYPE and QCLASS.
	if end > len(query) {
		return nil
	}
	response := append([]byte(nil), query[:end]...)
	binary.BigEndian.PutUint16(response[2:4], 0x8180)
	clear(response[6:12])
	if truncated {
		response[2] |= 2
		return response
	}
	binary.BigEndian.PutUint16(response[6:8], 1)
	return append(response, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4, 203, 0, 113, 5)
}

func TestWorkerDNSFallsBackToTCP(t *testing.T) {
	tcp, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	udp, err := net.ListenPacket("udp4", tcp.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	var tcpQueries atomic.Int32
	go func() {
		buf := make([]byte, 4096)
		for {
			n, address, err := udp.ReadFrom(buf)
			if err != nil {
				return
			}
			if response := dnsTestAnswer(buf[:n], true); response != nil {
				_, _ = udp.WriteTo(response, address)
			}
		}
	}()
	go func() {
		for {
			c, err := tcp.Accept()
			if err != nil {
				return
			}
			func() {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(3 * time.Second))
				query, err := readFrame(c, 4096)
				if err != nil {
					return
				}
				tcpQueries.Add(1)
				_ = writeFrame(c, dnsTestAnswer(query, false))
			}()
		}
	}()
	w := worker{dns: tcp.Addr().String()}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// UDP dial resolves the address without sending traffic to the test IP.
	c, err := w.dial(ctx, "udp", "large-answer.test:443")
	if err != nil {
		t.Fatal("truncated DNS answer did not fall back to TCP", err)
	}
	defer c.Close()
	if c.RemoteAddr().String() != "203.0.113.5:443" || tcpQueries.Load() != 1 {
		t.Fatalf("unexpected DNS result: %s, TCP queries=%d", c.RemoteAddr(), tcpQueries.Load())
	}
}
