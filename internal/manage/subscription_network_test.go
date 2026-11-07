package manage

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSubscriptionURLsReachBothAddressFamilies(t *testing.T) {
	probe, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skip("IPv6 loopback unavailable:", err)
	}
	probe.Close()
	root := t.TempDir()
	if err := configureSubscription(root, App{}, VPNSettings{}, 0); err != nil {
		t.Fatal(err)
	}
	const body = `{"subscription":"current"}`
	if err := AtomicBytes(filepath.Join(root, "sbox.json"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- RunSubscription(ctx, []string{"--root", root}, io.Discard) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("subscription did not stop")
		}
	})
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	for _, address := range []string{"127.0.0.1", "::1"} {
		t.Run(address, func(t *testing.T) {
			var urls bytes.Buffer
			if err := subscriptionURLs(root, address, normalScope, &urls); err != nil {
				t.Fatal(err)
			}
			endpoint := strings.Split(urls.String(), "\n")[0]
			deadline := time.Now().Add(3 * time.Second)
			for {
				response, err := client.Get(endpoint)
				if err == nil {
					data, readErr := io.ReadAll(response.Body)
					response.Body.Close()
					if readErr != nil || response.StatusCode != http.StatusOK || string(data) != body {
						t.Fatalf("invalid subscription response: status=%d body=%q err=%v", response.StatusCode, data, readErr)
					}
					return
				}
				if time.Now().After(deadline) {
					t.Fatal("generated subscription URL is unreachable:", err)
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}
