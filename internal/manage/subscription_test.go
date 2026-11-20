package manage

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/database"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func subscriptionFixture(t *testing.T, root, token, body string) sub.Config {
	t.Helper()
	c := fixtureSubscriptionConfig()
	seedOutputGrant(t, root, token, []string{"normal-sing-box", "vpngate-sing-box"}, nil)
	if err := AtomicJSON(filepath.Join(root, subscriptionsConfig), c); err != nil {
		t.Fatal(err)
	}
	if err := AtomicJSON(filepath.Join(root, "subscription.json"), subscriptionSettings{Enabled: true, Port: 12345, Listen: "::"}); err != nil {
		t.Fatal(err)
	}
	writeTestSnapshot(t, root, body)
	return c
}

func seedOutputGrant(t *testing.T, root, token string, outputs []string, limit *int64) {
	t.Helper()
	s, err := accounting.Open(filepath.Join(root, database.Filename))
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	db, err := database.Open(filepath.Join(root, database.Filename))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	raw, _ := json.Marshal(outputs)
	_, err = db.Exec(`INSERT INTO download_grants(id,scope,profiles,outputs,max_downloads,hash) VALUES('fixture','','[]',?,?,?) ON CONFLICT(id) DO UPDATE SET outputs=excluded.outputs,max_downloads=excluded.max_downloads,hash=excluded.hash`, string(raw), limit, sub.Digest([]byte(token)))
	if err != nil {
		t.Fatal(err)
	}
}

func writeTestSnapshot(t *testing.T, root, body string) {
	t.Helper()
	a := sub.Artifact{Name: "test", MIME: "application/json", Data: []byte(body), ETag: `"` + sub.Digest([]byte(body)) + `"`}
	if err := AtomicJSON(filepath.Join(root, subscriptionsSnapshot), sub.Snapshot{Version: sub.SchemaVersion, Artifacts: map[string]sub.Artifact{"normal-sing-box": a, "vpngate-sing-box": {Name: "vpn", MIME: "application/json", Data: []byte(`{"scope":"vpngate"}`), ETag: `"` + sub.Digest([]byte(`{"scope":"vpngate"}`)) + `"`}}}); err != nil {
		t.Fatal(err)
	}
}

func TestSubscriptionAuthenticationETagRevocation(t *testing.T) {
	root, token := t.TempDir(), strings.Repeat("a", 64)
	subscriptionFixture(t, root, token, `{"version":1}`)
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
	store, err := accounting.Open(filepath.Join(root, database.Filename))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.RevokeGrant(t.Context(), "fixture"); err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRecorder()
	handler.ServeHTTP(r, req)
	if r.Code != 404 {
		t.Fatal("revoked token accepted", r.Code)
	}
}

func TestSubscriptionUpdateWithinSameSecond(t *testing.T) {
	root, token := t.TempDir(), strings.Repeat("a", 64)
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
	root, token := t.TempDir(), strings.Repeat("a", 64)
	subscriptionFixture(t, root, token, `{"version":1}`)
	limit := int64(1)
	seedOutputGrant(t, root, token, []string{"normal-sing-box"}, &limit)
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
	root, token := t.TempDir(), strings.Repeat("a", 64)
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
	token := strings.Repeat("a", 64)
	seedOutputGrant(t, root, token, []string{"normal-provider", "vpngate-provider"}, nil)
	for _, scope := range []string{normalScope, vpngateScope} {
		var urls strings.Builder
		if err := subscriptionURLs(root, "127.0.0.1", scope, &urls); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(urls.String(), "/s/") {
			t.Fatal("read-only display leaked a token")
		}
		r := httptest.NewRecorder()
		subscriptionHandler(root).ServeHTTP(r, httptest.NewRequest("GET", "/s/"+token+"/"+scope+"-provider", nil))
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
	if first.Port != second.Port || first.PublicBaseURL != second.PublicBaseURL {
		t.Fatal("ordinary enable rotated address")
	}
	if err := resetSubscriptionToken(root); err == nil {
		t.Fatal("obsolete default-token operation accepted")
	}
}

func TestDisabledSubscriptionDoesNotListen(t *testing.T) {
	root := t.TempDir()
	if err := AtomicJSON(filepath.Join(root, "subscription.json"), subscriptionSettings{Enabled: false, Port: 12345, Listen: "127.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	if err := RunSubscription(context.Background(), []string{"--root", root}, io.Discard); err == nil {
		t.Fatal("disabled server started")
	}
}

func TestPanelAdvancedDownloadsWithoutIndependentListener(t *testing.T) {
	root, token := t.TempDir(), strings.Repeat("a", 64)
	subscriptionFixture(t, root, token, `{"version":1}`)
	if err := os.Remove(filepath.Join(root, "subscription.json")); err != nil {
		t.Fatal(err)
	}
	s, err := OpenPlatform(root, "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := httptest.NewRecorder()
	s.Handler(PanelOptions{PublicURL: "http://example.com"}).ServeHTTP(r, httptest.NewRequest("GET", "http://example.com/s/"+token+"/normal-sing-box", nil))
	if r.Code != 200 || r.Body.String() != `{"version":1}` {
		t.Fatal(r.Code, r.Body.String())
	}
}
