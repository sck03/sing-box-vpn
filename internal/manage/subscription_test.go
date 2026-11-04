package manage

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSubscriptionAuthenticationAndFreshFileContent(t *testing.T) {
	root, token := t.TempDir(), strings.Repeat("a", 43)
	if err := AtomicBytes(filepath.Join(root, "sbox.json"), []byte(`{"version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := AtomicBytes(filepath.Join(root, "vpngate", "sbox.json"), []byte(`{"scope":"vpngate"}`), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(subscriptionHandler(root, token))
	defer server.Close()
	for _, path := range []string{"wrong/sbox.json", token + "/app.json", token + "/../app.json", token + "/missing/file", "中文/sbox.json", "wrong/vpngate/sbox.json", token + "/vpngate/secrets.json", token + "/vpngate/server.json", token + "/vpngate/../sbox.json"} {
		response, err := server.Client().Get(server.URL + "/" + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 404 {
			t.Fatal("unauthorized file exposed", path, response.StatusCode)
		}
	}
	for _, body := range []string{`{"version":1}`, `{"version":2}`} {
		if err := AtomicBytes(filepath.Join(root, "sbox.json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		response, err := server.Client().Get(server.URL + "/" + token + "/sbox.json")
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || string(data) != body || response.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("stale subscription", err)
		}
	}
	response, err := server.Client().Get(server.URL + "/" + token + "/vpngate/sbox.json")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || string(data) != `{"scope":"vpngate"}` {
		t.Fatal("separate VPNGate subscription returned ordinary content", err)
	}
	req, _ := http.NewRequest("HEAD", server.URL+"/"+token+"/sbox.json", nil)
	response, err = server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 || response.ContentLength == 0 {
		t.Fatal("HEAD response invalid")
	}
}
func TestSubscriptionConnectionLimitAndShutdown(t *testing.T) {
	base, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener := &limitedListener{Listener: base, slots: make(chan struct{}, 1), done: make(chan struct{})}
	defer listener.Close()
	client, err := net.Dial("tcp4", base.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	first, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	result := make(chan error, 1)
	go func() {
		c, err := listener.Accept()
		if c != nil {
			c.Close()
		}
		result <- err
	}()
	select {
	case <-result:
		t.Fatal("accepted beyond connection limit")
	case <-time.After(20 * time.Millisecond):
	}
	listener.Close()
	select {
	case err := <-result:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not unblock saturated listener")
	}
	first.Close()
	first.Close()
	if len(listener.slots) != 0 {
		t.Fatal("connection capacity was not released exactly once")
	}
}
func TestDisabledSubscriptionDoesNotListen(t *testing.T) {
	root := t.TempDir()
	if err := AtomicJSON(filepath.Join(root, "subscription.json"), subscriptionSettings{false, 12345, strings.Repeat("a", 43), "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	if err := RunSubscription(context.Background(), []string{"--root", root}, io.Discard); err == nil {
		t.Fatal("disabled subscription started")
	}
	if err := os.Remove(filepath.Join(root, "subscription.json")); err != nil {
		t.Fatal(err)
	}
	if err := RunSubscription(context.Background(), []string{"--root", root}, io.Discard); err == nil {
		t.Fatal("unconfigured subscription started")
	}
}
