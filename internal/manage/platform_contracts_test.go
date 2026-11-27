package manage

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/domain"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func contractNode() sub.Node {
	return sub.Node{ID: "node", Name: "Review TUIC", Protocol: "tuic", Server: "1.1.1.1", Port: 443, UUID: "11111111-1111-4111-8111-111111111111", Password: "review-password", TLS: sub.TLS{Enabled: true, ServerName: "example.com"}}
}

func TestGatewayContractRejectsBadPolicyBeforeSaving(t *testing.T) {
	root, core, _ := appFixture(t)
	service, config, _ := platformFixture(t, root, core)
	configured, err := prepareCountry(service.Root, config, "JP")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []struct {
		key   string
		value any
	}{{"connect_timeout", 4}, {"connect_timeout", 181}, {"max_connections", 1025}, {"health_interval", 0}, {"unknown_option", true}} {
		t.Run(value.key+"-"+string(mustJSON(t, value.value)), func(t *testing.T) {
			candidate, err := decodePlatform(mustJSON(t, configured))
			if err != nil {
				t.Fatal(err)
			}
			candidate.Gateway[value.key] = value.value
			if _, err := service.PreviewConfiguration(t.Context(), candidate); err == nil {
				t.Fatal("preview accepted invalid gateway")
			}
			if _, err := service.QueueApply(t.Context(), candidate, 1, "admin"); err == nil {
				t.Fatal("queued invalid gateway")
			}
		})
	}
	state, err := service.Store.Revisions(t.Context())
	if err != nil || state.Revision != 1 {
		t.Fatal(state, err)
	}
}

func TestPublicShareNavigationRetainsManagementOriginProtection(t *testing.T) {
	root, core, _ := appFixture(t)
	service, _, session := platformFixture(t, root, core)
	if err := service.Accounts.SaveAccount(t.Context(), domain.Account{ID: "shared", Name: "Shared"}); err != nil {
		t.Fatal(err)
	}
	token, err := service.Accounts.CreateGrant(t.Context(), accounting.Grant{ID: "share", AccountID: "shared", Profiles: []string{"uri"}})
	if err != nil {
		t.Fatal(err)
	}
	handler := service.Handler(PanelOptions{PublicURL: "http://example.com"})
	for _, tc := range []struct {
		path   string
		status int
	}{{"/", 200}, {"/user", 200}, {"/panel/theme.js", 200}, {"/panel/client-platforms.js", 200}, {"/u/" + token + "/usage", 200}, {"/api/state", 403}} {
		request := panelRequest("GET", tc.path, nil, session)
		request.Header.Set("Sec-Fetch-Site", "cross-site")
		request.Header.Set("Origin", "https://other.example")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.path, response.Code, response.Body.String())
		}
	}
}

func TestPreviewAndApplyRejectUnsupportedOutputTogether(t *testing.T) {
	root, core, _ := appFixture(t)
	service, config, session := platformFixture(t, root, core)
	config.Subscriptions = sub.Config{Version: sub.SchemaVersion, Revision: 1,
		Sources:     []sub.Source{{ID: "source", Kind: "inline", Format: "nodes", Content: string(mustJSON(t, []sub.Node{contractNode()})), Required: true}},
		Collections: []sub.Collection{{ID: "collection", Sources: []string{"source"}}},
		Templates:   []sub.Template{{ID: "template", Client: "loon", Version: 1, Mode: "config"}},
		Outputs:     []sub.Output{{ID: "output", Collection: "collection", Template: "template"}},
	}
	before := readBytes(t, filepath.Join(root, subscriptionsSnapshot))
	handler := service.Handler(PanelOptions{PublicURL: "http://example.com"})
	for _, path := range []string{"/api/config/preview"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, panelRequest("POST", path, mustJSON(t, config), session))
		if response.Code != 400 || !strings.Contains(response.Body.String(), "does not support tuic") {
			t.Fatal(path, response.Code, response.Body.String())
		}
	}
	if _, err := service.QueueApply(t.Context(), config, 1, "admin"); err == nil {
		t.Fatal("invalid output queued")
	}
	removed := httptest.NewRecorder()
	handler.ServeHTTP(removed, panelRequest("POST", "/api/subscriptions/preview", mustJSON(t, config), session))
	if removed.Code != http.StatusNotFound {
		t.Fatal("removed preview API is still exposed", removed.Code)
	}
	if !bytes.Equal(before, readBytes(t, filepath.Join(root, subscriptionsSnapshot))) {
		t.Fatal("preview wrote artifacts")
	}
}

func TestQuickWarpUsesEndpointAndCandidateKeepsPreparedSource(t *testing.T) {
	root, core, _ := appFixture(t)
	service, config, _ := platformFixture(t, root, core)
	config.App.Advanced.Endpoints = []Object{{"type": "wireguard", "tag": "warp-out", "private_key": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)), "address": []string{"172.16.0.2/32"}, "peers": []Object{{"address": "1.1.1.1", "port": 2408, "public_key": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32)), "allowed_ips": []string{"0.0.0.0/0"}}}}}
	config.App.Advanced.Route = Object{"rules": []Object{{"domain_suffix": []string{"example.com"}, "action": "route", "outbound": "warp-out"}}, "final": "direct", "default_domain_resolver": "dns-local"}
	if _, err := service.PreviewConfiguration(t.Context(), config); err != nil {
		t.Fatal("endpoint was not used", err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_ = json.NewEncoder(w).Encode([]sub.Node{contractNode()})
	}))
	defer server.Close()
	config.Subscriptions = sub.Config{Version: sub.SchemaVersion, Revision: 1, Sources: []sub.Source{{ID: "source", Kind: "remote", Format: "nodes", URL: server.URL, Required: true}}, Collections: []sub.Collection{{ID: "set", Sources: []string{"source"}}}, Templates: []sub.Template{{ID: "template", Client: "uri", Version: 1, Mode: "nodes"}}, Outputs: []sub.Output{{ID: "output", Collection: "set", Template: "template"}}}
	if _, err := service.QueueApply(t.Context(), config, 1, "admin"); err != nil {
		t.Fatal(err)
	}
	task, err := service.Store.ClaimLane(t.Context(), "control")
	if err != nil {
		t.Fatal(err)
	}
	var input applyInput
	if err = json.Unmarshal(task.Input, &input); err != nil {
		t.Fatal(err)
	}
	server.Close()
	files, err := service.buildConfig(t.Context(), config, sub.Candidate, input.Sources)
	if err != nil {
		t.Fatal("accepted source needed another network fetch", err)
	}
	var snapshot sub.Snapshot
	if err = json.Unmarshal(files[subscriptionsSnapshot], &snapshot); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 || len(snapshot.Artifacts) != 1 {
		t.Fatal(requests.Load(), len(snapshot.Artifacts))
	}
}

func TestCancelledTaskPreservesPartialRedactedResults(t *testing.T) {
	root, core, _ := appFixture(t)
	service, _, _ := platformFixture(t, root, core)
	id, err := service.Store.Queue(t.Context(), "subscription", "admin", []byte(`{}`), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err = service.storeTaskOutput(ctx, id, []byte(`{"outcome":"partial","passed":1,"total":2,"password":"hidden"}`), context.Canceled)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	task, err := service.Store.Task(t.Context(), id)
	if err != nil || task.Outcome != "partial" || bytes.Contains(task.Result, []byte("hidden")) || !bytes.Contains(task.Result, []byte(`"passed":1`)) {
		t.Fatal(string(task.Result), err)
	}
}
