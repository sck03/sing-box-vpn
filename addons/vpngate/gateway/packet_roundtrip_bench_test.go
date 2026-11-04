package main

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func BenchmarkTunnelUDPRoundTrip(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	echo, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		b.Fatal(err)
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
	socket := filepath.Join(b.TempDir(), "w.sock")
	w := worker{socket: socket, dialOverride: (&net.Dialer{}).DialContext}
	done := make(chan error, 1)
	go func() { done <- w.serve(ctx) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				b.Error(err)
			}
		case <-time.After(time.Second):
			b.Error("benchmark worker did not stop")
		}
	}()
	ready, stop := context.WithTimeout(ctx, 3*time.Second)
	defer stop()
	var c net.Conn
	for {
		c, _, err = dialWorker(ready, socket, "udp", echo.LocalAddr().String())
		if err == nil {
			break
		}
		if ready.Err() != nil {
			b.Fatal("benchmark worker did not start", err)
		}
		time.Sleep(time.Millisecond)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(30 * time.Second))
	packet := make([]byte, 1402)
	frames := frameReader{source: c, limit: maxPacket}
	if err := writePacket(c, packet); err != nil {
		b.Fatal(err)
	}
	if _, err := frames.read(); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(1400)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := writePacket(c, packet); err != nil {
			b.Fatal(err)
		}
		if _, err := frames.read(); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
}
