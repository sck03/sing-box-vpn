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

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func subscriptionFixture(t *testing.T, root, token, body string) sub.Config {
	t.Helper()
	c := sub.DefaultConfig()
	if err := ensureSubscriptionGrant(&c, token); err != nil {
		t.Fatal(err)
	}
	if err := AtomicJSON(filepath.Join(root, subscriptionsConfig), c); err != nil {
		t.Fatal(err)
	}
	if err := AtomicJSON(filepath.Join(root, "subscription.json"), subscriptionSettings{Enabled: true, Port: 12345, Token: token, Listen: "::"}); err != nil {
		t.Fatal(err)
	}
	writeTestSnapshot(t, root, body)
	return c
}

func writeTestSnapshot(t *testing.T, root, body string) {
	t.Helper()
	a := sub.Artifact{Name: "test", MIME: "application/json", Data: []byte(body), ETag: `"` + sub.Digest([]byte(body)) + `"`}
	if err := AtomicJSON(filepath.Join(root, subscriptionsSnapshot), sub.Snapshot{Version: sub.SchemaVersion, Artifacts: map[string]sub.Artifact{"normal-sing-box": a, "vpngate-sing-box": {Name: "vpn", MIME: "application/json", Data: []byte(`{"scope":"vpngate"}`), ETag: `"` + sub.Digest([]byte(`{"scope":"vpngate"}`)) + `"`}}}); err != nil {
		t.Fatal(err)
	}
}

func TestSubscriptionAuthenticationETagRevocation(t *testing.T) {
	root, token := t.TempDir(), strings.Repeat("a", 43)
	c := subscriptionFixture(t, root, token, `{"version":1}`)
	handler := subscriptionHandler(root)
	for _, path := range []string{"/s/wrong/normal-sing-box", "/s/" + token + "/app.json", "/s/" + token + "/../app.json", "/s/" + token + "/vpngate/secrets.json", "/s/" + token + "/normal-sing-box?url=http://localhost"} {
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		if r.Code != 404 {
			t.Fatal(path, r.Code)
		}
	}
	path := "/s/" + token + "/normal-sing-box"
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
	if r.Code != 200 || r.Body.String() != `{"version":1}` || r.Header().Get("Cache-Control") != "private, no-cache" {
		t.Fatal(r)
	}
	tag := r.Header().Get("ETag")
	if tag == "" {
		t.Fatal("missing ETag")
	}
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("If-None-Match", tag)
	r = httptest.NewRecorder()
	handler.ServeHTTP(r, req)
	if r.Code != 304 || r.Body.Len() != 0 {
		t.Fatal(r)
	}
	c.Grants[0].Revoked = true
	if err := AtomicJSON(filepath.Join(root, subscriptionsConfig), c); err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRecorder()
	handler.ServeHTTP(r, req)
	if r.Code != 404 {
		t.Fatal("revoked token accepted", r.Code)
	}
}

func TestSubscriptionUpdateWithinSameSecond(t *testing.T) {
	root, token := t.TempDir(), strings.Repeat("a", 43)
	subscriptionFixture(t, root, token, `{"version":1}`)
	handler := subscriptionHandler(root)
	modified := time.Now().Add(-time.Minute).Truncate(time.Second)
	tag := ""
	for _, body := range []string{`{"version":1}`, `{"version":2}`} {
		writeTestSnapshot(t, root, body)
		if err := os.Chtimes(filepath.Join(root, subscriptionsSnapshot), modified, modified); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("GET", "/s/"+token+"/normal-sing-box", nil)
		req.Header.Set("If-None-Match", tag)
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, req)
		if r.Code != 200 || r.Body.String() != body {
			t.Fatal("stale response", r.Code, r.Body.String())
		}
		tag = r.Header().Get("ETag")
	}
}

func TestSubscriptionLimitsAndScope(t *testing.T) {
	root, token := t.TempDir(), strings.Repeat("a", 43)
	c := subscriptionFixture(t, root, token, `{"version":1}`)
	c.Grants[0].Outputs = []string{"normal-sing-box"}
	c.Grants[0].MaxDownloads = 1
	if err := AtomicJSON(filepath.Join(root, subscriptionsConfig), c); err != nil {
		t.Fatal(err)
	}
	handler := subscriptionHandler(root)
	for _, tc := range []struct {
		method, path string
		status       int
	}{{"GET", "vpngate-sing-box", 404}, {"HEAD", "normal-sing-box", 200}, {"GET", "normal-sing-box", 200}, {"GET", "normal-sing-box", 404}} {
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequest(tc.method, "/s/"+token+"/"+tc.path, nil))
		if r.Code != tc.status {
			t.Fatal(tc, r.Code)
		}
	}
	r := httptest.NewRecorder()
	subscriptionHandler(root).ServeHTTP(r, httptest.NewRequest("GET", "/s/"+token+"/normal-sing-box", nil))
	if r.Code != 404 {
		t.Fatal("count reset")
	}
}

func TestSubscriptionRejectsUnavailableSnapshot(t *testing.T) {
	root, token := t.TempDir(), strings.Repeat("a", 43)
	subscriptionFixture(t, root, token, `{"version":1}`)
	handler := subscriptionHandler(root)
	if err := AtomicBytes(filepath.Join(root, subscriptionsSnapshot), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest("GET", "/s/"+token+"/normal-sing-box", nil))
	if r.Code != 503 {
		t.Fatal(r.Code)
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
	ReadJSON(filepath.Join(root, "subscription.json"), &settings)
	for _, scope := range []string{normalScope, vpngateScope} {
		var urls strings.Builder
		if err := subscriptionURLs(root, "127.0.0.1", scope, &urls); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(urls.String(), "/s/"+settings.Token+"/"+scope+"-provider") {
			t.Fatal("missing provider URL")
		}
		r := httptest.NewRecorder()
		subscriptionHandler(root).ServeHTTP(r, httptest.NewRequest("GET", "/s/"+settings.Token+"/"+scope+"-provider", nil))
		if r.Code != 200 || !strings.Contains(r.Body.String(), `"proxies"`) {
			t.Fatal(r.Code, r.Body.String())
		}
	}
}

func TestSubscriptionEnablePreservesAddress(t *testing.T) {
	root := t.TempDir()
	if err := configureSubscription(root, App{}, VPNSettings{}, 0, "127.0.0.1", "https://example.com/sub"); err != nil {
		t.Fatal(err)
	}
	var first, second subscriptionSettings
	ReadJSON(filepath.Join(root, "subscription.json"), &first)
	if err := disableSubscription(root); err != nil {
		t.Fatal(err)
	}
	if err := configureSubscription(root, App{}, VPNSettings{}, 0); err != nil {
		t.Fatal(err)
	}
	ReadJSON(filepath.Join(root, "subscription.json"), &second)
	if first.Token != second.Token || first.Port != second.Port || first.PublicBaseURL != second.PublicBaseURL {
		t.Fatal("ordinary enable rotated address")
	}
	if err := resetSubscriptionToken(root); err != nil {
		t.Fatal(err)
	}
	ReadJSON(filepath.Join(root, "subscription.json"), &second)
	if first.Token == second.Token {
		t.Fatal("reset did not rotate")
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
		t.Fatal("capacity exceeded")
	case <-time.After(20 * time.Millisecond):
	}
	listener.Close()
	select {
	case err := <-result:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked shutdown")
	}
	first.Close()
	first.Close()
	if len(listener.slots) != 0 {
		t.Fatal("capacity leak")
	}
}

func TestDisabledSubscriptionDoesNotListen(t *testing.T) {
	root := t.TempDir()
	if err := AtomicJSON(filepath.Join(root, "subscription.json"), subscriptionSettings{Enabled: false, Port: 12345, Token: strings.Repeat("a", 43), Listen: "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	if err := RunSubscription(context.Background(), []string{"--root", root}, io.Discard); err == nil {
		t.Fatal("disabled server started")
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
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done(); close(stopped) })}
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
	io.WriteString(client, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		close(fail)
		t.Fatal("not started")
	}
	close(fail)
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("failure hidden")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("not stopped")
	}
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("request survived")
	}
}
