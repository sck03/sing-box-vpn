package main

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func udpBatchFixture(b testing.TB) (*net.UDPConn, [][]byte) {
	b.Helper()
	_, access, _ := startProxyFixture(b)
	_, client := udpAssociationFixture(b, access)
	packets := make([][]byte, 8)
	for i := range packets {
		server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			b.Fatal(err)
		}
		b.Cleanup(func() { server.Close() })
		go func() {
			var buf [4096]byte
			for {
				n, peer, err := server.ReadFromUDPAddrPort(buf[:])
				if err != nil {
					return
				}
				_, _ = server.WriteToUDPAddrPort(buf[:n], peer)
			}
		}()
		header, err := udpResponseHeader(server.LocalAddr().String())
		if err != nil {
			b.Fatal(err)
		}
		packets[i] = append(header, bytes.Repeat([]byte{'x'}, 1400)...)
	}
	return client, packets
}

func BenchmarkSOCKSUDPTargetBatch(b *testing.B) {
	client, packets := udpBatchFixture(b)
	var response [4096]byte
	roundTrip := func() {
		_ = client.SetDeadline(time.Now().Add(5 * time.Second))
		for _, packet := range packets {
			if _, err := client.Write(packet); err != nil {
				b.Fatal(err)
			}
		}
		for range packets {
			if n, err := client.Read(response[:]); err != nil || n != 1410 {
				b.Fatal("UDP batch failed", n, err)
			}
		}
	}
	roundTrip()
	b.ReportAllocs()
	b.SetBytes(8 * 1400)
	for b.Loop() {
		roundTrip()
	}
}

func TestUDPConcurrentPortSequences(t *testing.T) {
	client, packets := udpBatchFixture(t)
	if err := client.SetReadBuffer(256 << 10); err != nil {
		t.Fatal(err)
	}
	var response [4096]byte
	last := make([]int, len(packets))
	for i := range last {
		last[i] = -1
	}
	for batch := 0; batch < 32; batch++ {
		for sequence := batch * 4; sequence < (batch+1)*4; sequence++ {
			for target, packet := range packets {
				packet[10] = byte(target)
				binary.BigEndian.PutUint16(packet[11:13], uint16(sequence))
				if _, err := client.Write(packet); err != nil {
					t.Fatal(err)
				}
			}
		}
		for i := 0; i < 4*len(packets); i++ {
			_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
			n, err := client.Read(response[:])
			if err != nil || n != 1410 {
				t.Fatal("concurrent UDP transfer lost a packet", n, err)
			}
			target := int(response[10])
			if target >= len(packets) || !bytes.Equal(response[4:10], packets[target][4:10]) {
				t.Fatal("reply came from the wrong target port")
			}
			sequence := int(binary.BigEndian.Uint16(response[11:13]))
			if sequence != last[target]+1 || !bytes.Equal(response[13:n], packets[target][13:]) {
				t.Fatal("UDP payload or per-target order changed", target, sequence)
			}
			last[target] = sequence
		}
	}
}
