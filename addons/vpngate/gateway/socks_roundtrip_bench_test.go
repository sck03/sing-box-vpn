package main

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func BenchmarkSOCKSUDPRoundTrip(b *testing.B) {
	proxy, _, _ := startProxyFixture(b)
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
	control := authProxy(b, proxy, "password")
	defer control.Close()
	if _, err := control.Write(requestAddress(3, "0.0.0.0:0")); err != nil {
		b.Fatal(err)
	}
	var reply [10]byte
	if _, err := io.ReadFull(control, reply[:]); err != nil || reply[1] != 0 {
		b.Fatal("UDP association failed", reply, err)
	}
	client, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IP(reply[4:8]), Port: int(binary.BigEndian.Uint16(reply[8:]))})
	if err != nil {
		b.Fatal(err)
	}
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(30 * time.Second))
	header, err := udpResponseHeader(echo.LocalAddr().String())
	if err != nil {
		b.Fatal(err)
	}
	packet := append(header, make([]byte, 1400)...)
	answer := make([]byte, len(packet))
	roundTrip := func() {
		if _, err := client.Write(packet); err != nil {
			b.Fatal(err)
		}
		if n, err := client.Read(answer); err != nil || n != len(packet) {
			b.Fatal("UDP reply failed", n, err)
		}
	}
	roundTrip()
	b.ReportAllocs()
	b.SetBytes(1400)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		roundTrip()
	}
	b.StopTimer()
}
