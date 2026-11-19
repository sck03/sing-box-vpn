package manage

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestImportedTrojanTransportsWithMihomo(t *testing.T) {
	core := os.Getenv("SING_BOX_BINARY")
	if core == "" || mihomoBinary() == "" {
		t.Skip("both client cores are required")
	}
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer endpoint.Close()
	for _, network := range []string{"ws", "grpc"} {
		t.Run(network, func(t *testing.T) {
			root := t.TempDir()
			certificate, key := filepath.Join(root, "cert.pem"), filepath.Join(root, "key.pem")
			if err := EnsureCertificate(certificate, key, "localhost"); err != nil {
				t.Fatal(err)
			}
			port, err := AvailablePort(map[int]bool{})
			if err != nil {
				t.Fatal(err)
			}
			transport := Object{"type": network}
			n := sub.Node{Name: "节点,用于验证", Protocol: "trojan", Server: "127.0.0.1", Port: port, Password: "imported-trojan-password", TLS: sub.TLS{Enabled: true, Insecure: true, ServerName: "localhost"}, Transport: sub.Transport{Type: network}}
			if network == "ws" {
				transport["path"] = "/test"
				n.Transport.Path = "/test"
			} else {
				transport["service_name"] = "test"
				n.Transport.ServiceName = "test"
			}
			config := Object{"log": Object{"level": "error"}, "inbounds": []Object{{"type": "trojan", "listen": "127.0.0.1", "listen_port": port, "users": []Object{{"password": n.Password}}, "transport": transport, "tls": Object{"enabled": true, "certificate_path": certificate, "key_path": key}}}, "outbounds": []Object{{"type": "direct", "tag": "direct"}}, "route": Object{"final": "direct"}}
			path := filepath.Join(root, "server.json")
			if err := AtomicJSON(path, config); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cmd := exec.CommandContext(ctx, core, "run", "-c", path)
			cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() { _ = cmd.Wait(); close(done) }()
			defer func() { cancel(); <-done }()
			deadline := time.Now().Add(10 * time.Second)
			for {
				connection, err := net.DialTimeout("tcp", net.JoinHostPort(n.Server, fmt.Sprint(port)), 100*time.Millisecond)
				if err == nil {
					connection.Close()
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("fixture server did not become ready", err)
				}
				select {
				case <-done:
					t.Fatal("fixture server exited")
				case <-time.After(20 * time.Millisecond):
				}
			}
			if err := probeNodeWithEngine(ctx, root, core, endpoint.URL, "mihomo", n); err != nil {
				t.Fatal("converted transport failed", err)
			}
			if network == "ws" {
				n.Transport.Path = "/wrong"
			} else {
				n.Transport.ServiceName = "wrong"
			}
			negative, stop := context.WithTimeout(ctx, 2*time.Second)
			defer stop()
			if err := probeNodeWithEngine(negative, root, core, endpoint.URL, "mihomo", n); err == nil {
				t.Fatal("probe did not exercise the transport")
			}
		})
	}
}
