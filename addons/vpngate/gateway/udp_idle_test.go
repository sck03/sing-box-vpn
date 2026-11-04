package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"os"
	"testing"
	"time"
)

func TestUDPActiveOneWay(t *testing.T) {
	if os.Getenv("RUN_UDP_IDLE_TESTS") != "1" {
		t.Skip("set RUN_UDP_IDLE_TESTS=1 for the 65-second UDP lifetime test")
	}
	for _, direction := range []string{"upload", "download"} {
		t.Run(direction, func(t *testing.T) {
			t.Parallel()
			proxy, _, _ := startProxyFixture(t)
			server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			control := authProxy(t, proxy, "password")
			defer control.Close()
			_, _ = control.Write(requestAddress(3, "0.0.0.0:0"))
			var reply [10]byte
			if _, err = io.ReadFull(control, reply[:]); err != nil || reply[1] != 0 {
				t.Fatal("UDP association failed", err, reply)
			}
			client, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IP(reply[4:8]), Port: int(binary.BigEndian.Uint16(reply[8:]))})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			header, err := udpResponseHeader(server.LocalAddr().String())
			if err != nil {
				t.Fatal(err)
			}
			packet := append(header, 'x')
			if direction == "upload" {
				go func() {
					var buf [16]byte
					var peer string
					stable := true
					for {
						n, from, err := server.ReadFromUDPAddrPort(buf[:])
						if err != nil {
							return
						}
						if peer == "" {
							peer = from.String()
						}
						stable = stable && peer == from.String()
						if n == 1 && buf[0] == '!' {
							status := byte(0)
							if stable {
								status = 1
							}
							_, _ = server.WriteToUDPAddrPort([]byte{status}, from)
							return
						}
					}
				}()
			} else {
				go func() {
					var buf [16]byte
					_, peer, err := server.ReadFromUDPAddrPort(buf[:])
					if err != nil {
						return
					}
					for i := 0; i < 67; i++ {
						if _, err := server.WriteToUDPAddrPort([]byte("download"), peer); err != nil {
							return
						}
						time.Sleep(time.Second)
					}
				}()
			}
			if _, err = client.Write(packet); err != nil {
				t.Fatal(err)
			}
			until := time.Now().Add(65 * time.Second)
			buf := make([]byte, 128)
			for time.Now().Before(until) {
				if direction == "upload" {
					time.Sleep(time.Second)
					if _, err = client.Write(packet); err != nil {
						t.Fatal(err)
					}
				} else {
					_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
					n, err := client.Read(buf)
					if err != nil || n < 10 || !bytes.Equal(buf[10:n], []byte("download")) {
						t.Fatal("active download expired", err)
					}
				}
			}
			if direction == "upload" {
				packet[len(packet)-1] = '!'
				_, _ = client.Write(packet)
				_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
				n, err := client.Read(buf)
				if err != nil || n != 11 || buf[10] != 1 {
					t.Fatal("active upload was closed and reconnected", err)
				}
			}
		})
	}
}
