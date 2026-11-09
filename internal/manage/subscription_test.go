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

func TestSubscriptionRejectsUnavailableFiles(t *testing.T) {
	root, token := t.TempDir(), strings.Repeat("a", 43)
	handler := subscriptionHandler(root, token)
	for _, scope := range []string{normalScope, vpngateScope} {
		directory, err := exportDirectory(root, scope)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		prefix := ""
		if scope == vpngateScope {
			prefix = "vpngate/"
		}
		for _, name := range []string{"sbox.json", "clmi.yaml", "proxies.yaml", "jhsub.txt"} {
			filename := name
			if name == "jhsub.txt" {
				filename = "jhsub-base64.txt"
			}
			path := filepath.Join(directory, filename)
			for _, size := range []int64{0, maxJSONFile + 1} {
				file, err := os.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				err = file.Truncate(size)
				file.Close()
				if err != nil {
					t.Fatal(err)
				}
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					response := httptest.NewRecorder()
					handler.ServeHTTP(response, httptest.NewRequest(method, "/"+token+"/"+prefix+name, nil))
					if response.Code != http.StatusServiceUnavailable {
						t.Errorf("%s %s%s with %d bytes returned %d", method, prefix, name, size, response.Code)
					}
				}
			}
		}
	}
}

func TestNativeProviderSubscription(t *testing.T) {
	root, core, app := appFixture(t)
	if _, err := PrepareVPN(context.Background(), root, core, app, "JP"); err != nil {
		t.Fatal(err)
	}
	if err := configureSubscription(root, app, VPNSettings{}, 0); err != nil {
		t.Fatal(err)
	}
	var settings subscriptionSettings
	if _, err := ReadJSON(filepath.Join(root, "subscription.json"), &settings); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(subscriptionHandler(root, settings.Token))
	defer server.Close()
	for _, scope := range []string{normalScope, vpngateScope} {
		var urls strings.Builder
		if err := subscriptionURLs(root, "127.0.0.1", scope, &urls); err != nil {
			t.Fatal(err)
		}
		found := false
		for endpoint := range strings.FieldsSeq(urls.String()) {
			if !strings.HasSuffix(endpoint, "/proxies.yaml") {
				continue
			}
			found = true
			prefix := strings.Index(endpoint, "/"+settings.Token+"/")
			if prefix < 0 {
				t.Fatal("provider URL omitted authentication")
			}
			response, err := server.Client().Get(server.URL + endpoint[prefix:])
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(response.Body)
			response.Body.Close()
			directory, _ := exportDirectory(root, scope)
			if err != nil || response.StatusCode != http.StatusOK || string(data) != string(readBytes(t, filepath.Join(directory, "proxies.yaml"))) {
				t.Fatal("provider subscription returned incorrect scope or content", scope, err)
			}
			if response.Header.Get("Content-Type") != "application/yaml; charset=utf-8" {
				t.Fatal("provider content type missing")
			}
		}
		if !found {
			t.Fatal("provider URL not advertised", scope)
		}
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

func TestSubscriptionUpdateWithinSameSecond(t *testing.T) {
	root, token := t.TempDir(), strings.Repeat("a", 43)
	path := filepath.Join(root, "sbox.json")
	handler := subscriptionHandler(root, token)
	modified := time.Now().Add(-time.Minute).Truncate(time.Second)
	for i, body := range []string{`{"version":1}`, `{"version":2}`} {
		if err := AtomicBytes(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, modified, modified); err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodGet, "/"+token+"/sbox.json", nil)
		if i > 0 {
			request.Header.Set("If-Modified-Since", modified.UTC().Format(http.TimeFormat))
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Body.String() != body {
			t.Fatalf("update %d returned status %d, body %q", i, response.Code, response.Body.String())
		}
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

type failingSubscriptionListener struct {
	net.Listener
	fail     <-chan struct{}
	accepted bool
}

func (l *failingSubscriptionListener) Accept() (net.Conn, error) {
	if l.accepted {
		<-l.fail
		return nil, errors.New("listener failed")
	}
	l.accepted = true
	return l.Listener.Accept()
}

func TestSubscriptionListenerFailureClosesActiveRequests(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	started, stopped := make(chan struct{}), make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(stopped)
	})}
	defer server.Close()
	fail := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- serveSubscription(context.Background(), &failingSubscriptionListener{Listener: listener, fail: fail}, server)
	}()
	client, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		close(fail)
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := io.WriteString(client, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n"); err != nil {
		close(fail)
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		close(fail)
		t.Fatal("request did not start")
	}
	close(fail)
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "listener failed") {
			t.Fatal("listener failure was hidden", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not exit")
	}
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("active request survived listener failure")
	}
}
