package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"
)

// Use the real socket deadlines: rejected traffic must not extend an idle
// association, while TestUDPActiveOneWay verifies either valid direction does.
func TestUDPRejectedTrafficDoesNotExtendIdleTimeout(t *testing.T) {
	if os.Getenv("RUN_UDP_IDLE_TESTS") != "1" {
		t.Skip("set RUN_UDP_IDLE_TESTS=1 for the UDP idle timeout regression")
	}
	t.Parallel()
	for _, kind := range []string{"short", "fragment", "foreign-port", "zero-port"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			server, _ := udpLifecycleEcho(t)
			_, access, _ := startProxyFixture(t)
			control, client := udpAssociationFixture(t, access)
			packet := udpTestPacket(server.LocalAddr().String(), "initial")
			if _, err := client.Write(packet); err != nil {
				t.Fatal(err)
			}
			receiveUDPPayload(t, client, "initial")
			sender := client
			switch kind {
			case "short":
				packet = packet[:3]
			case "fragment":
				packet[2] = 1
			case "zero-port":
				packet[8], packet[9] = 0, 0
			case "foreign-port":
				var err error
				sender, err = net.DialUDP("udp4", nil, client.RemoteAddr().(*net.UDPAddr))
				if err != nil {
					t.Fatal(err)
				}
				defer sender.Close()
			}
			stop, done := make(chan struct{}), make(chan struct{})
			go func() {
				defer close(done)
				ticker := time.NewTicker(100 * time.Millisecond)
				defer ticker.Stop()
				for {
					select {
					case <-stop:
						return
					case <-ticker.C:
						if _, err := sender.Write(packet); err != nil {
							return
						}
					}
				}
			}()
			defer func() { close(stop); <-done }()
			_ = control.SetReadDeadline(time.Now().Add(udpIdleTimeout + 5*time.Second))
			var buf [1]byte
			if _, err := control.Read(buf[:]); !errors.Is(err, io.EOF) {
				t.Fatalf("rejected %s traffic kept idle association open: %v", kind, err)
			}
		})
	}
}

func TestUDPActiveOneWay(t *testing.T) {
	if os.Getenv("RUN_UDP_IDLE_TESTS") != "1" {
		t.Skip("set RUN_UDP_IDLE_TESTS=1 for the 65-second UDP lifetime test")
	}
	t.Parallel()
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
