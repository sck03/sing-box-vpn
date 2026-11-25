package vpngate

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

func TestSingleClientBudgetForwardsTCPAndUDP(t *testing.T) {
	for _, network := range []string{"tcp", "udp"} {
		t.Run(network, func(t *testing.T) {
			m, _, requests := pendingDialFixture(t)
			m.cfg.ProxyUser, m.cfg.ProxyPass = "user", "password"
			if network == "udp" {
				control, client := udpAssociationFixture(t, m)
				if _, err := client.Write(udpTestPacket("203.0.113.5:443", "ping")); err != nil {
					t.Fatal(err)
				}
				worker := awaitDial(t, requests)
				if err := writeObject(worker, dialResponse{Address: "203.0.113.5:443"}); err != nil {
					t.Fatal(err)
				}
				packet, err := readFrame(worker, maxPacket)
				if err != nil {
					t.Fatal(err)
				}
				if err := writePacket(worker, append([]byte{0, 0}, packet...)); err != nil {
					t.Fatal(err)
				}
				receiveUDPPayload(t, client, "ping")
				m.revoke()
				_ = control.SetReadDeadline(time.Now().Add(time.Second))
				if _, err := control.Read(make([]byte, 1)); err == nil {
					t.Fatal("revoked association survived")
				}
				return
			}
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan struct{})
			go func() { defer close(done); _ = serveProxy(ctx, listener, m) }()
			t.Cleanup(func() { cancel(); <-done })
			client := authProxy(t, listener.Addr().String(), "password")
			defer client.Close()
			_ = client.SetDeadline(time.Now().Add(3 * time.Second))
			if _, err := client.Write(requestAddress(1, "203.0.113.5:443")); err != nil {
				t.Fatal(err)
			}
			worker := awaitDial(t, requests)
			if err := writeObject(worker, dialResponse{Address: "203.0.113.5:443"}); err != nil {
				t.Fatal(err)
			}
			var reply [10]byte
			if _, err := io.ReadFull(client, reply[:]); err != nil || reply[1] != 0 {
				t.Fatal("first forward rejected", reply, err)
			}
			if _, err := worker.Write([]byte("ping")); err != nil {
				t.Fatal(err)
			}
			var payload [4]byte
			if _, err := io.ReadFull(client, payload[:]); err != nil || string(payload[:]) != "ping" {
				t.Fatal(payload, err)
			}
		})
	}
}
