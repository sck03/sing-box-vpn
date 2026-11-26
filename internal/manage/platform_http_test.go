package manage

import (
	"bytes"
	"errors"
	"github.com/sck03/sing-box-vpn/internal/adminauth"
	"github.com/sck03/sing-box-vpn/internal/domain"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func platformFixture(t *testing.T, root, core string) (*PlatformService, PlatformConfig, adminauth.Session) {
	t.Helper()
	runtime := os.Getenv("MANAGED_CORE_BINARY")
	if runtime == "" {
		t.Skip("set MANAGED_CORE_BINARY for platform integration tests")
	}
	s, e := OpenPlatform(root, core, runtime, true)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	var app App
	if _, e = ReadJSON(root+"/app.json", &app); e != nil {
		t.Fatal(e)
	}
	subs, _, _, e := loadSubscriptions(root)
	if e != nil {
		t.Fatal(e)
	}
	c := PlatformConfig{Schema: PlatformSchema, App: app, Subscriptions: subs, VPN: VPNSettings{ProxyPort: 7928, Profiles: map[string]Profile{}}, VPNSecrets: map[string]Secrets{}, Gateway: Object{}, Listener: subscriptionSettings{Listen: "127.0.0.1", Port: 2097}, Argo: TunnelSettings{Mode: "off"}, Certificates: []CertificateAsset{}}
	if e = s.Store.Initialize(t.Context(), mustJSON(t, c)); e != nil {
		t.Fatal(e)
	}
	if e = AtomicJSON(root+"/runtime-revision.json", int64(1)); e != nil {
		t.Fatal(e)
	}
	if e = s.Auth.Bootstrap(t.Context(), "admin", "correct-password-123", false); e != nil {
		t.Fatal(e)
	}
	session, e := s.Auth.Login(t.Context(), "admin", "correct-password-123", "", "local")
	if e != nil {
		t.Fatal(e)
	}
	return s, c, session
}
func panelRequest(method, path string, body []byte, session adminauth.Session) *http.Request {
	r := httptest.NewRequest(method, "http://example.com"+path, bytes.NewReader(body))
	if session.Token != "" {
		r.AddCookie(&http.Cookie{Name: "sbvpn", Value: session.Token})
		r.Header.Set("X-CSRF-Token", session.CSRF)
	}
	return r
}
func TestPanelRolesOriginHostAndCSRF(t *testing.T) {
	root, core, _ := appFixture(t)
	s, _, admin := platformFixture(t, root, core)
	if e := s.Auth.Save(t.Context(), "admin", adminauth.Admin{ID: "viewer", Role: "viewer"}, "viewer-password-123"); e != nil {
		t.Fatal(e)
	}
	viewer, e := s.Auth.Login(t.Context(), "viewer", "viewer-password-123", "", "viewer-peer")
	if e != nil {
		t.Fatal(e)
	}
	handler := s.Handler(PanelOptions{PublicURL: "http://example.com"})
	for _, tc := range []struct {
		name, method, path string
		session            adminauth.Session
		mutate             func(*http.Request)
		want               int
	}{
		{"anonymous", "GET", "/api/state", adminauth.Session{}, nil, 401},
		{"anonymous catalog", "GET", "/api/clients", adminauth.Session{}, nil, 401},
		{"authenticated catalog", "GET", "/api/clients", admin, nil, 200},
		{"anonymous input formats", "GET", "/api/subscription-capabilities", adminauth.Session{}, nil, 401},
		{"authenticated input formats", "GET", "/api/subscription-capabilities", admin, nil, 200},
		{"viewer subscription actions", "GET", "/api/subscriptions/actions", viewer, nil, 403},
		{"admin subscription actions", "GET", "/api/subscriptions/actions", admin, nil, 200},
		{"viewer state", "GET", "/api/state", viewer, nil, 200},
		{"viewer secret", "GET", "/api/config", viewer, nil, 403},
		{"viewer panel settings", "GET", "/api/panel/settings", viewer, nil, 403},
		{"viewer panel settings write", "POST", "/api/panel/settings", viewer, nil, 403},
		{"viewer panel preview", "POST", "/api/panel/settings/preview", viewer, nil, 403},
		{"viewer task", "POST", "/api/tasks", viewer, nil, 403},
		{"viewer task details", "GET", "/api/tasks/detail?id=1", viewer, nil, 403},
		{"viewer impact", "GET", "/api/inbounds/impact?scope=normal&id=in", viewer, nil, 403},
		{"viewer accounts", "GET", "/api/accounts?limit=20", viewer, nil, 200},
		{"invalid page", "GET", "/api/accounts?offset=-1", admin, nil, 400},
		{"viewer program", "GET", "/api/program", viewer, nil, 403},
		{"viewer certificate preparation", "POST", "/api/certificates/prepare", viewer, nil, 403},
		{"viewer template import", "POST", "/api/subscription-templates/import", viewer, nil, 403},
		{"viewer options", "GET", "/api/account-options", viewer, nil, 403},
		{"viewer inbound options", "GET", "/api/inbound-options?scope=normal", viewer, nil, 403},
		{"viewer export port options", "GET", "/api/accounts/ports?id=alice", viewer, nil, 403},
		{"admin inbound options", "GET", "/api/inbound-options?scope=normal", admin, nil, 200},
		{"unconfigured VPN options", "GET", "/api/inbound-options?scope=vpngate", admin, nil, 200},
		{"CSRF", "POST", "/api/tasks", admin, func(r *http.Request) { r.Header.Del("X-CSRF-Token") }, 401},
		{"origin", "GET", "/api/state", admin, func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, 403},
		{"host", "GET", "/api/state", admin, func(r *http.Request) { r.Host = "evil.example" }, 400},
		{"fetch site", "GET", "/api/state", admin, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := panelRequest(tc.method, tc.path, []byte(`{"operation":"service","service":"sing-box","value":"restart"}`), tc.session)
			if tc.mutate != nil {
				tc.mutate(r)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	id, e := s.Store.Queue(t.Context(), "apply", "test", []byte(`{}`), 0, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.Result(t.Context(), id, []byte(`{"diagnostic":"private-value"}`)); e != nil {
		t.Fatal(e)
	}
	if e = s.Store.Finish(t.Context(), id, errors.New("private-error")); e != nil {
		t.Fatal(e)
	}
	for _, role := range []adminauth.Session{admin, viewer} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, panelRequest("GET", "/api/overview", nil, role))
		if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte("private-")) {
			t.Fatal("task summary leaked detail", w.Body.String())
		}
	}
	r := panelRequest("GET", "/api/state", nil, admin)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	for _, secret := range []string{s.Root + "/key.pem", "correct-password-123"} {
		if bytes.Contains(w.Body.Bytes(), []byte(secret)) {
			t.Fatal("secret leak")
		}
	}
	if e = s.Auth.Revoke(t.Context(), "admin"); e != nil {
		t.Fatal(e)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("revoked session", w.Code)
	}
}
func TestPanelAccountRevision(t *testing.T) {
	root, core, _ := appFixture(t)
	s, _, session := platformFixture(t, root, core)
	handler := s.Handler(PanelOptions{PublicURL: "http://example.com"})
	a := domain.Account{ID: "alice", Name: "Alice"}
	for i, rev := range []int64{0, 1, 1} {
		a.Revision = rev
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, panelRequest("POST", "/api/accounts", mustJSON(t, AccountSave{a}), session))
		want := 200
		if i == 2 {
			want = 409
		}
		if w.Code != want {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
func TestPanelTLSBindingPolicy(t *testing.T) {
	s := &PlatformService{}
	if e := s.Serve(t.Context(), PanelOptions{Listen: "0.0.0.0:0"}, new(bytes.Buffer)); e == nil {
		t.Fatal("public cleartext panel")
	}
}

func TestPanelQRCodeRoute(t *testing.T) {
	root, core, _ := appFixture(t)
	s, _, session := platformFixture(t, root, core)
	w := httptest.NewRecorder()
	s.Handler(PanelOptions{PublicURL: "http://example.com"}).ServeHTTP(w, panelRequest("POST", "/api/qr", []byte(`{"content":"vless://example"}`), session))
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("data:image/png;base64,")) {
		t.Fatal(w.Code, w.Body.String())
	}
}
