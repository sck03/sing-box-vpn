package manage

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/adminauth"
	"github.com/sck03/sing-box-vpn/internal/domain"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestStateRolesDoNotExposeCredentialFieldFamilies(t *testing.T) {
	root, core, _ := appFixture(t)
	s, config, admin := platformFixture(t, root, core)
	config.Subscriptions.Templates[0].Origin = &sub.TemplateOrigin{SourceURL: "https://example.invalid/template?token=sentinel-origin", Format: "rule-list"}
	config.Subscriptions.Templates[0].ProbeURL = "https://example.invalid/probe?key=sentinel-probe"
	config.App.Advanced.Outbounds = []Object{{"type": "hysteria2", "obfs": Object{"password": "sentinel-obfs"}, "obfs_password": "sentinel-flat"}}
	config.Gateway["sources"] = []string{"https://example.invalid/nodes?token=sentinel-gateway"}
	if _, err := s.Store.Queue(t.Context(), "apply", "admin", []byte(`{}`), 1, mustJSON(t, config)); err != nil {
		t.Fatal(err)
	}
	handler := s.Handler(PanelOptions{PublicURL: "http://example.com"})
	for _, role := range []string{"viewer", "operator"} {
		if err := s.Auth.Save(t.Context(), "admin", adminauth.Admin{ID: role, Role: role}, "test-password-123"); err != nil {
			t.Fatal(err)
		}
		session, err := s.Auth.Login(t.Context(), role, "test-password-123", "", role)
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, panelRequest("GET", "/api/state", nil, session))
		if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte("sentinel-")) {
			t.Fatal(role, w.Code, w.Body.String())
		}
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, panelRequest("GET", "/api/config", nil, session))
		if w.Code != 403 {
			t.Fatal(role, w.Code)
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, panelRequest("GET", "/api/config", nil, admin))
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("sentinel-origin")) || !bytes.Contains(w.Body.Bytes(), []byte("sentinel-gateway")) {
		t.Fatal("admin secret view lost", w.Code)
	}
}

func TestAccountDetailUsesEffectiveStateAndCreationConflict(t *testing.T) {
	root, core, _ := appFixture(t)
	s, _, admin := platformFixture(t, root, core)
	if err := s.Accounts.SaveSubscriber(t.Context(), accounting.Subscriber{ID: "person", Name: "Person", Disabled: true}); err != nil {
		t.Fatal(err)
	}
	a := domain.Account{ID: "account", Name: "Account", SubscriberID: "person"}
	if err := s.Accounts.SaveAccount(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	handler := s.Handler(PanelOptions{PublicURL: "http://example.com"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, panelRequest("GET", "/api/accounts/detail?id=account", nil, admin))
	var detail AccountSummary
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil || w.Code != 200 || detail.State != "subscriber_disabled" || detail.Denial != "subscriber_disabled" {
		t.Fatal(w.Code, w.Body.String(), err)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, panelRequest("POST", "/api/accounts", mustJSON(t, Object{"account": a}), admin))
	if w.Code != 409 || !bytes.Contains(w.Body.Bytes(), []byte(`"code":"already_exists"`)) {
		t.Fatal(w.Code, w.Body.String())
	}
}
