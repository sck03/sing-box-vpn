package manage

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

func TestPanelAccessSettingsValidation(t *testing.T) {
	root := t.TempDir()
	cert, key := filepath.Join(root, "cert.pem"), filepath.Join(root, "key.pem")
	if err := EnsureCertificate(cert, key, "panel.example.com"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		options PanelOptions
		valid   bool
	}{
		{"local", PanelOptions{Listen: "127.0.0.1:2096"}, true},
		{"ephemeral saved settings", PanelOptions{Listen: "127.0.0.1:0"}, false},
		{"TLS", PanelOptions{Listen: "0.0.0.0:2096", PublicURL: "https://panel.example.com:2096", Certificate: cert, Key: key}, true},
		{"reverse proxy", PanelOptions{Listen: "127.0.0.1:2096", PublicURL: "https://panel.example.com", TrustedProxy: []string{"127.0.0.1/32"}}, true},
		{"cleartext", PanelOptions{Listen: "0.0.0.0:2096", PublicURL: "http://panel.example.com"}, false},
		{"missing TLS", PanelOptions{Listen: "127.0.0.1:2096", PublicURL: "https://panel.example.com"}, false},
		{"mismatched domain", PanelOptions{Listen: "0.0.0.0:2096", PublicURL: "https://wrong.example.com", Certificate: cert, Key: key}, false},
		{"partial key pair", PanelOptions{Listen: "127.0.0.1:2096", Certificate: cert}, false},
		{"no public origin", PanelOptions{Listen: "0.0.0.0:2096", Certificate: cert, Key: key}, false},
		{"query", PanelOptions{Listen: "127.0.0.1:2096", PublicURL: "http://localhost?"}, false},
		{"path", PanelOptions{Listen: "127.0.0.1:2096", PublicURL: "http://localhost/panel"}, false},
		{"invalid port", PanelOptions{Listen: "127.0.0.1:65536"}, false},
		{"invalid proxy", PanelOptions{Listen: "127.0.0.1:2096", TrustedProxy: []string{"bad"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.options.Validate(); (err == nil) != tc.valid {
				t.Fatalf("valid=%v, error=%v", tc.valid, err)
			}
		})
	}
}

func TestPanelEphemeralDevelopmentListener(t *testing.T) {
	for _, address := range []string{"0.0.0.0:0", "[::]:0", "127.0.0.1:-1", "127.0.0.1:65536"} {
		if err := (PanelOptions{Listen: address}).validate(true); err == nil {
			t.Fatalf("development accepted invalid/public ephemeral listener %s", address)
		}
	}
	if err := (PanelOptions{Listen: "[::1]:0"}).validate(true); err != nil {
		t.Fatal(err)
	}
	root, core, _ := appFixture(t)
	s, _, _ := platformFixture(t, root, core)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reader, writer := io.Pipe()
	defer reader.Close()
	done := make(chan error, 1)
	go func() {
		err := s.Serve(ctx, PanelOptions{Listen: "127.0.0.1:0"}, writer)
		writer.CloseWithError(err)
		done <- err
	}()
	line := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(reader)
		if scanner.Scan() {
			line <- scanner.Text()
		} else {
			line <- ""
		}
	}()
	select {
	case message := <-line:
		address := strings.TrimPrefix(message, "管理面板：")
		parsed, err := url.Parse(address)
		if err != nil || parsed.Hostname() != "127.0.0.1" || parsed.Port() == "" || parsed.Port() == "0" {
			t.Fatalf("invalid actual listener %q: %v", message, err)
		}
		response, err := (&http.Client{Timeout: 5 * time.Second}).Get(address + "/api/capabilities")
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatal(response.Status)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("development listener did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("development listener did not stop")
	}
}

func TestPanelSettingsPreviewSaveConflictAndPermissions(t *testing.T) {
	root, core, _ := appFixture(t)
	s, _, admin := platformFixture(t, root, core)
	active := PanelOptions{Listen: "127.0.0.1:2096", PublicURL: "http://example.com"}
	handler := s.Handler(active)
	_, revision, err := s.panelSettings()
	if err != nil {
		t.Fatal(err)
	}
	next := PanelOptions{Listen: "127.0.0.1:2096", PublicURL: "https://panel.example.com", TrustedProxy: []string{"127.0.0.1/32"}}
	body := mustJSON(t, Object{"options": next, "revision": revision})
	for i, path := range []string{"/api/panel/settings/preview", "/api/panel/settings", "/api/panel/settings"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, panelRequest("POST", path, body, admin))
		want := 200
		if i == 2 {
			want = 409
		}
		if path == "/api/panel/settings/preview" {
			_, after, e := s.panelSettings()
			if e != nil || after != revision {
				t.Fatal("preview modified settings", e)
			}
		} else {
			_, after, _ := s.panelSettings()
			if after == revision {
				t.Fatal("settings were not saved")
			}
		}
		if w.Code != want {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	// Authentication and CSRF run before settings are read or written.
	r := panelRequest("POST", "/api/panel/settings", body, admin)
	r.Header.Del("X-CSRF-Token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("missing CSRF accepted", w.Code)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, panelRequest("GET", "/api/panel/settings", nil, admin))
	var result struct {
		Options, Active PanelOptions
		RestartRequired bool `json:"restart_required"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Options.PublicURL != next.PublicURL || result.Active.PublicURL != active.PublicURL || !result.RestartRequired {
		t.Fatal(w.Body.String())
	}
}

func TestShareRejectsLoopbackBeforeCreatingGrant(t *testing.T) {
	root, core, _ := appFixture(t)
	s, _, admin := platformFixture(t, root, core)
	if err := s.Accounts.SaveAccount(t.Context(), domain.Account{ID: "a", Name: "Alice"}); err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"http://127.0.0.1:2096", "http://localhost:2096", "http://[::1]:2096", "http://0.0.0.0:2096"} {
		handler := s.Handler(PanelOptions{PublicURL: origin})
		r := panelRequest("POST", "/api/grants", mustJSON(t, Object{"id": "share", "account_id": "a", "clients": []string{"uri"}}), admin)
		u, _ := url.Parse(origin)
		r.Host = u.Host
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 400 || !bytes.Contains(w.Body.Bytes(), []byte("公网访问")) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	page, err := s.Accounts.GrantPage(t.Context(), "", 0, 20)
	if err != nil || page.Total != 0 {
		t.Fatal("invalid share persisted", page, err)
	}
	origin, err := s.downloadOrigin(t.Context(), "https://panel.example.com")
	if err != nil || origin != "https://panel.example.com" {
		t.Fatal(origin, err)
	}
}

func TestServiceCatalogNamesAndWorkspaces(t *testing.T) {
	root, core, _ := appFixture(t)
	s, _, _ := platformFixture(t, root, core)
	status, err := s.ServiceStatus(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"sing-box": "inbounds", "vpngate-core": "vpngate", "vpngate": "vpngate", "subscription": "access", "argo": "outbounds"}
	if len(status) != len(want) {
		t.Fatal(status)
	}
	for _, service := range status {
		name := text(service["name"])
		if page, ok := want[name]; !ok || text(service["page"]) != page || text(service["description"]) == "" {
			t.Fatal(service)
		}
	}
	if scopeService("vpngate") != "vpngate-core" || scopeService("normal") != "sing-box" || serviceDetails("vpngate").Runtime != "" {
		t.Fatal("gateway and metered runtime identities conflated")
	}
	for _, old := range []string{"sing-box-vpngate", "yg-vpngate", "sb-subscription", "sb-argo", "user"} {
		if _, err := s.Queue(t.Context(), PlatformAction{Operation: "service", Service: old, Value: "start"}, "test"); err == nil {
			t.Fatal("old service accepted", old)
		}
	}
}
