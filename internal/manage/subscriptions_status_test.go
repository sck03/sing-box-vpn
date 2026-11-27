package manage

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/adminauth"
	"github.com/sck03/sing-box-vpn/internal/domain"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestSubscriptionActivityShowsResourcesAndWebhookLifecycle(t *testing.T) {
	root, core, _ := appFixture(t)
	service, config, session := platformFixture(t, root, core)
	var status atomic.Int32
	status.Store(http.StatusNoContent)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(int(status.Load())) }))
	defer remote.Close()
	c := config.Subscriptions
	c.Webhook = remote.URL + "?secret=webhook-secret"
	c.Templates[0].URL = remote.URL + "/template?secret=template-secret"
	if err := AtomicJSON(filepath.Join(root, subscriptionsConfig), c); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	r := sub.Runtime{Resources: map[string]sub.ResourceCache{"template:" + c.Templates[0].ID: {URL: c.Templates[0].URL, Body: json.RawMessage(`{"hidden":"cached-body-secret"}`), Checked: now, Success: now.Add(-time.Hour), Next: now.Add(time.Minute), Failures: 1, Error: "upstream unavailable"}}}
	if err := writeSubscriptionRuntime(root, c, r); err != nil {
		t.Fatal(err)
	}
	handler := service.Handler(PanelOptions{PublicURL: "http://example.com"})
	read := func() Object {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, panelRequest("GET", "/api/subscriptions/status", nil, session))
		if response.Code != 200 {
			t.Fatal(response.Code, response.Body.String())
		}
		for _, secret := range []string{"webhook-secret", "template-secret", "cached-body-secret", "endpoint_hash"} {
			if strings.Contains(response.Body.String(), secret) {
				t.Fatal("status exposed private material", secret)
			}
		}
		var data Object
		if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		return data
	}
	initial := read()
	resources := objects(initial["resources"])
	if len(resources) != 1 || resources[0]["state"] != "failed" || resources[0]["checked"] == nil || resources[0]["success"] == nil {
		t.Fatal(resources)
	}
	if object(object(initial["notification"])["activity"])["state"] != "pending" {
		t.Fatal(initial)
	}
	notifySubscriptionEvent(t.Context(), remote.Client(), root, c, sub.Run{Operation: "resource", ObjectID: "template"})
	sent := object(object(read()["notification"])["activity"])
	if sent["state"] != "ok" || sent["success"] == nil {
		t.Fatal(sent)
	}
	status.Store(http.StatusServiceUnavailable)
	notifySubscriptionEvent(t.Context(), remote.Client(), root, c, sub.Run{Operation: "refresh", ObjectID: "source"})
	failed := object(object(read()["notification"])["activity"])
	if failed["state"] != "failed" || failed["success"] != sent["success"] || !strings.Contains(text(failed["error"]), "503") {
		t.Fatal(failed)
	}
	c.Webhook = remote.URL + "/changed"
	if err := writeSubscriptionRuntime(root, c, r); err != nil {
		t.Fatal(err)
	}
	if object(object(read()["notification"])["activity"])["state"] != "pending" {
		t.Fatal("old endpoint success was reused")
	}
	c.Webhook = ""
	if err := writeSubscriptionRuntime(root, c, r); err != nil {
		t.Fatal(err)
	}
	if object(object(read()["notification"])["activity"])["state"] != "disabled" {
		t.Fatal("disabled webhook still active")
	}
	if err := service.Auth.Save(t.Context(), "admin", adminauth.Admin{ID: "viewer", Role: "viewer"}, "viewer-password-123"); err != nil {
		t.Fatal(err)
	}
	viewer, err := service.Auth.Login(t.Context(), "viewer", "viewer-password-123", "", "viewer-peer")
	if err != nil {
		t.Fatal(err)
	}
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, panelRequest("GET", "/api/subscriptions/status", nil, viewer))
	if denied.Code != 403 {
		t.Fatal("viewer accessed private operational status", denied.Code)
	}
	config.Subscriptions.Webhook = "https://example.com/?token=state-secret"
	if _, err = service.Store.Queue(t.Context(), "apply", domain.TaskSubmission{Actor: "admin", Source: domain.TaskUser}, []byte(`{}`), 1, mustJSON(t, config)); err != nil {
		t.Fatal(err)
	}
	state := httptest.NewRecorder()
	handler.ServeHTTP(state, panelRequest("GET", "/api/state", nil, viewer))
	if state.Code != 200 || bytes.Contains(state.Body.Bytes(), []byte("state-secret")) {
		t.Fatal("read-only state exposed webhook", state.Body.String())
	}
}

func TestResourceActivityRetainsLastSuccessAfterFailure(t *testing.T) {
	var fail atomic.Bool
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		_ = json.NewEncoder(w).Encode(sub.Template{ID: "remote", Client: "uri", Version: 1, Mode: "nodes"})
	}))
	defer remote.Close()
	c := sub.Config{Version: sub.SchemaVersion, Templates: []sub.Template{{ID: "remote", Client: "uri", Version: 1, Mode: "nodes", URL: remote.URL}}}
	now := time.Now()
	_, first, _ := refreshSubscriptionResources(t.Context(), remote.Client(), &c, nil, now, true)
	if first["template:remote"].Checked != now || first["template:remote"].Success != now {
		t.Fatal(first)
	}
	fail.Store(true)
	_, second, _ := refreshSubscriptionResources(t.Context(), remote.Client(), &c, first, now.Add(time.Minute), true)
	value := second["template:remote"]
	if value.Error == "" || value.Success != now || value.Checked != now.Add(time.Minute) || !bytes.Equal(value.Body, first["template:remote"].Body) {
		t.Fatal(value)
	}
}
