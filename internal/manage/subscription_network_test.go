package manage

import (
	"context"
	"errors"
	"fmt"
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
	writeTestSnapshot(t, root, body)
	var settings subscriptionSettings
	ReadJSON(filepath.Join(root, "subscription.json"), &settings)
	token := strings.Repeat("a", 64)
	seedOutputGrant(t, root, token, []string{"normal-sing-box"}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		listener, err := net.Listen("tcp", net.JoinHostPort(settings.Listen, fmt.Sprint(settings.Port)))
		if err != nil {
			done <- err
			return
		}
		server := &http.Server{Handler: subscriptionHandler(root)}
		go func() { <-ctx.Done(); server.Close() }()
		err = server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		done <- err
	}()
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
			endpoint := fixtureSubscriptionBase(settings, address) + "/s/" + token + "/normal-sing-box"
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
