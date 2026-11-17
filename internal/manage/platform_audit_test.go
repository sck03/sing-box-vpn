package manage

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/domain"
)

func TestInitializeRejectsInvalidAdminAndRetriesProjection(t *testing.T) {
	root, core := t.TempDir(), coreBinary(t)
	s, err := OpenPlatform(root, core, core, true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Initialize(t.Context(), "127.0.0.1", "test", "invalid admin", "correct-password-123"); err == nil {
		t.Fatal("invalid ID accepted")
	}
	if _, err = os.Stat(filepath.Join(root, "app.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid initialization published files", err)
	}
	// Force a projection failure after the database transaction has committed.
	if err = os.Mkdir(filepath.Join(root, "runtime-revision.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = s.Initialize(t.Context(), "127.0.0.1", "test", "admin", "correct-password-123"); err == nil {
		t.Fatal("expected projection failure")
	}
	if _, err = s.Store.State(t.Context()); err != nil {
		t.Fatal("configuration not durable", err)
	}
	if _, err = s.Auth.Login(t.Context(), "admin", "correct-password-123", "", "local"); err != nil {
		t.Fatal("administrator not committed with configuration", err)
	}
	if err = os.Remove(filepath.Join(root, "runtime-revision.json")); err != nil {
		t.Fatal(err)
	}
	if err = s.Initialize(t.Context(), "127.0.0.1", "test", "admin", "correct-password-123"); err != nil {
		t.Fatal("retry failed", err)
	}
	if _, err = os.Stat(filepath.Join(root, initializationMarker)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("initialization still pending", err)
	}
	if err = s.Initialize(t.Context(), "127.0.0.1", "test", "admin", "correct-password-123"); err == nil {
		t.Fatal("initialized platform overwritten")
	}
}

func TestUsageSurvivesExhaustedDownloadsAndRetention(t *testing.T) {
	root := t.TempDir()
	store, err := accounting.Open(filepath.Join(root, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.SaveAccount(t.Context(), domain.Account{ID: "a", Name: "Alice"}); err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	token, err := store.CreateGrant(t.Context(), accounting.Grant{ID: "g", AccountID: "a", Clients: []string{"uri"}, MaxDownloads: &zero})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.PruneHistory(t.Context(), time.Now().AddDate(0, 0, 31)); err != nil {
		t.Fatal(err)
	}
	service := AccountService{Root: root, Store: store}
	for _, tc := range []struct {
		resource string
		status   int
	}{{"usage", 200}, {"uri", 403}} {
		w := httptest.NewRecorder()
		service.ServeDownload(w, httptest.NewRequest("GET", "/u/"+token+"/"+tc.resource, nil))
		if w.Code != tc.status {
			t.Fatal(tc, w.Code, w.Body.String())
		}
	}
	if err = store.RevokeGrant(t.Context(), "g"); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	service.ServeDownload(w, httptest.NewRequest("GET", "/u/"+token+"/usage", nil))
	if w.Code != 404 {
		t.Fatal("revoked usage exposed", w.Code)
	}
}

func TestPanelStreamsLeaveRoomForManagementRequests(t *testing.T) {
	root, core, _ := appFixture(t)
	s, _, session := platformFixture(t, root, core)
	server := httptest.NewUnstartedServer(nil)
	server.Config.Handler = s.Handler(PanelOptions{PublicURL: "http://" + server.Listener.Addr().String()})
	server.Start()
	defer server.Close()
	transport := &http.Transport{DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	get := func(path string) *http.Response {
		r, err := http.NewRequestWithContext(ctx, "GET", server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.AddCookie(&http.Cookie{Name: "sbvpn", Value: session.Token})
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	for range 32 {
		r := get("/api/events")
		defer r.Body.Close()
		if r.StatusCode != 200 {
			t.Fatal(r.Status)
		}
	}
	r := get("/api/events")
	io.Copy(io.Discard, r.Body)
	r.Body.Close()
	if r.StatusCode != 503 {
		t.Fatal("stream limit missing", r.Status)
	}
	r = get("/api/auth/session")
	defer r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal("streams starved management", r.Status)
	}
}

func TestForwardedPeerTrustBoundary(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8")}
	for _, tc := range []struct{ header, want string }{
		{"192.0.2.1, 203.0.113.7, 10.0.0.2", "203.0.113.7"},
		{"spoofed, 203.0.113.7", "203.0.113.7"},
		{"192.0.2.1, invalid", "127.0.0.1"},
		{"", "127.0.0.1"},
	} {
		if got := forwardedPeer(tc.header, "127.0.0.1", trusted); got != tc.want {
			t.Fatal(tc, got)
		}
	}
}
