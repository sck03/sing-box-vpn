package manage

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
	"github.com/sck03/sing-box-vpn/internal/domain"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestSubscriptionDownloadCountsActualResponses(t *testing.T) {
	for _, tc := range []struct {
		name, method, header, value string
		status                      int
		count                       int64
	}{
		{"get", "GET", "", "", 200, 1},
		{"head", "HEAD", "", "", 200, 0},
		{"unchanged", "GET", "If-None-Match", "TAG", 304, 0},
		{"weak", "GET", "If-None-Match", "W/TAG", 304, 0},
		{"etag-list", "GET", "If-None-Match", `"another", TAG`, 304, 0},
		{"wildcard", "GET", "If-None-Match", "*", 304, 0},
		{"malformed-prefix", "GET", "If-None-Match", "invalid, TAG", 200, 1},
		{"range", "GET", "Range", "bytes=0-2", 206, 1},
		{"multipart-range", "GET", "Range", "bytes=0-2,4-6", 206, 1},
		{"invalid-range", "GET", "Range", "bytes=9999-", 416, 0},
		{"failed-precondition", "GET", "If-Match", `"different"`, 412, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, token, body := t.TempDir(), strings.Repeat("a", 64), `{"version":1}`
			subscriptionFixture(t, root, token, body)
			limit := int64(1)
			seedOutputGrant(t, root, token, []string{"normal-sing-box"}, &limit)
			service, closeSource, err := openAccountSource(root)
			if err != nil {
				t.Fatal(err)
			}
			defer closeSource()
			handler := subscriptionHandlerView(service)
			request := httptest.NewRequest(tc.method, "/s/"+token+"/normal-sing-box", nil)
			if tc.header != "" {
				request.Header.Set(tc.header, strings.ReplaceAll(tc.value, "TAG", `"`+sub.Digest([]byte(body))+`"`))
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			grant, _ := service.Store.LookupGrant(t.Context(), token)
			if response.Code != tc.status || grant.Downloads != tc.count {
				t.Fatalf("status=%d downloads=%d; want %d/%d", response.Code, grant.Downloads, tc.status, tc.count)
			}
			if tc.count == 1 {
				for range 2 {
					response = httptest.NewRecorder()
					handler.ServeHTTP(response, request)
					if response.Code != 404 || strings.Contains(response.Body.String(), body) {
						t.Fatal("exhausted link returned content", response)
					}
				}
			}
		})
	}
}

func TestSubscriptionConcurrentDownloadLimit(t *testing.T) {
	root, token := t.TempDir(), strings.Repeat("c", 64)
	subscriptionFixture(t, root, token, "private body")
	limit := int64(1)
	seedOutputGrant(t, root, token, []string{"normal-sing-box"}, &limit)
	service, closeSource, err := openAccountSource(root)
	if err != nil {
		t.Fatal(err)
	}
	defer closeSource()
	handler := subscriptionHandlerView(service)
	var sent atomic.Int64
	var group sync.WaitGroup
	for range 12 {
		group.Go(func() {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest("GET", "/s/"+token+"/normal-sing-box", nil))
			if response.Code == 200 {
				sent.Add(1)
			} else if response.Code != 404 {
				t.Errorf("unexpected status %d", response.Code)
			}
		})
	}
	group.Wait()
	if sent.Load() != 1 {
		t.Fatal("allowance violated", sent.Load())
	}
}

func TestSubscriptionDownloadRequiresConfirmedGeneration(t *testing.T) {
	root, token := t.TempDir(), strings.Repeat("b", 64)
	config := subscriptionFixture(t, root, token, "candidate")
	service, closeSource, err := openAccountSource(root)
	if err != nil {
		t.Fatal(err)
	}
	defer closeSource()
	control := service.Control.(*controlstore.Store)
	config.Revision++
	if _, err := control.Queue(t.Context(), "apply", domain.TaskSubmission{Actor: "test", Source: domain.TaskUser}, []byte(`{}`), 1, mustJSON(t, PlatformConfig{Schema: PlatformSchema, Subscriptions: config})); err != nil {
		t.Fatal(err)
	}
	var snapshot sub.Snapshot
	if _, err := ReadJSON(filepath.Join(root, subscriptionsSnapshot), &snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.Revision = config.Revision
	for name, value := range map[string]any{subscriptionsConfig: config, subscriptionsSnapshot: snapshot, "runtime-revision.json": int64(2), subscriptionsOutputStatus: outputProjection(config, snapshot)} {
		if err := AtomicJSON(filepath.Join(root, name), value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeSubscriptionRuntime(root, config, sub.Runtime{}); err != nil {
		t.Fatal(err)
	}
	platform := PlatformService{Root: root, Store: control, Accounts: service.Store}
	handler := subscriptionHandlerView(service)
	for _, confirmed := range []bool{false, true} {
		if confirmed {
			if err := control.Applied(t.Context(), 2); err != nil {
				t.Fatal(err)
			}
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", "/s/"+token+"/normal-sing-box", nil))
		if confirmed && response.Code != 200 || !confirmed && response.Code != 503 {
			t.Fatal(confirmed, response.Code)
		}
		status, err := platform.SubscriptionStatus(t.Context())
		if err != nil || status.Outputs[0].Available != confirmed {
			t.Fatal(confirmed, status, err)
		}
	}
	// A same-config snapshot from another revision must also be refused.
	snapshot.Revision++
	if err := AtomicJSON(filepath.Join(root, subscriptionsSnapshot), snapshot); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/s/"+token+"/normal-sing-box", nil))
	if response.Code != 503 {
		t.Fatal("mismatched snapshot accepted", response.Code)
	}
	// Replacing the config at the same revision must invalidate the hot cache.
	snapshot.Revision = config.Revision
	if err := AtomicJSON(filepath.Join(root, subscriptionsSnapshot), snapshot); err != nil {
		t.Fatal(err)
	}
	config.Outputs[0].Name = "unexpected replacement"
	if err := AtomicJSON(filepath.Join(root, subscriptionsConfig), config); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/s/"+token+"/normal-sing-box", nil))
	if response.Code != 503 {
		t.Fatal("changed config reused previous confirmation", response.Code)
	}
}

func TestSubscriptionFreshnessUsesContributingOrRequiredSources(t *testing.T) {
	now := time.Now()
	config := sub.Config{Revision: 1, Sources: []sub.Source{
		{ID: "valid", Kind: "inline"}, {ID: "remote", Kind: "remote", MaxStaleSeconds: 60},
	}, Collections: []sub.Collection{{ID: "all", Sources: []string{"valid", "remote"}}}, Outputs: []sub.Output{{ID: "out", Collection: "all"}}}
	snapshot := sub.Snapshot{Version: sub.SchemaVersion, Revision: 1, Artifacts: map[string]sub.Artifact{"out": {}}, Contributors: map[string][]string{"out": {"valid"}}, SourceTimes: map[string]time.Time{"remote": now.Add(-time.Minute)}}
	if err := sub.PolicyForOutput(config, snapshot, "out").Validate(now); err != nil {
		t.Fatal("unused optional source blocked output", err)
	}
	snapshot.Contributors["out"] = []string{"valid", "remote"}
	if err := sub.PolicyForOutput(config, snapshot, "out").Validate(now); err == nil {
		t.Fatal("expired contributor accepted")
	}
	snapshot.Contributors["out"] = []string{"valid"}
	config.Sources[1].Required = true
	if err := sub.PolicyForOutput(config, snapshot, "out").Validate(now); err == nil {
		t.Fatal("expired required source accepted")
	}
	snapshot.SourceTimes["remote"] = now
	if err := sub.PolicyForOutput(config, snapshot, "out").Validate(now); err != nil {
		t.Fatal("fresh required source rejected", err)
	}
}

func TestSubscriptionAuthorizesActualContributors(t *testing.T) {
	for _, mode := range []string{"absent", "filtered", "included"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			config := sub.Config{Version: sub.SchemaVersion, Revision: 1,
				Sources:     []sub.Source{{ID: "optional", Kind: "account", AccountID: "disabled"}, {ID: "valid", Kind: "inline", Format: "uri", Content: "trojan://long-password-123456@example.com:443?security=tls#Valid", Required: true}},
				Collections: []sub.Collection{{ID: "all", Sources: []string{"optional", "valid"}}},
				Templates:   []sub.Template{{ID: "uri", Client: "uri", Version: 1, Mode: "nodes"}}, Outputs: []sub.Output{{ID: "out", Collection: "all", Template: "uri"}},
			}
			local := map[string][]sub.Node{}
			if mode != "absent" {
				local["account:optional"] = []sub.Node{{ID: "account", SourceID: "forged", Name: "Account", Protocol: "trojan", Server: "account.example", Port: 443, Password: "account-password", TLS: sub.TLS{Enabled: true}}}
			}
			if mode == "filtered" {
				config.Collections[0].Processor.Exclude = "Account"
			}
			snapshot, _, err := sub.Compile(config, sub.Runtime{}, local, time.Now(), sub.Candidate)
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(snapshot.Contributors["out"], "optional") != (mode == "included") {
				t.Fatal(snapshot.Contributors)
			}
			confirmTestSubscriptions(t, root, config)
			service, closeSource, err := openAccountSource(root)
			if err != nil {
				t.Fatal(err)
			}
			defer closeSource()
			if err := service.Store.SaveAccount(t.Context(), domain.Account{ID: "disabled", Name: "Disabled", Disabled: true}); err != nil {
				t.Fatal(err)
			}
			for name, value := range map[string]any{subscriptionsConfig: config, subscriptionsSnapshot: snapshot, subscriptionsOutputStatus: outputProjection(config, snapshot)} {
				if err := AtomicJSON(filepath.Join(root, name), value); err != nil {
					t.Fatal(err)
				}
			}
			if err := writeSubscriptionRuntime(root, config, sub.Runtime{}); err != nil {
				t.Fatal(err)
			}
			token, err := service.Store.CreateGrant(t.Context(), accounting.Grant{ID: "share", Outputs: []string{"out"}})
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			subscriptionHandlerView(service).ServeHTTP(response, httptest.NewRequest("GET", "/s/"+token+"/out", nil))
			allowed := mode != "included"
			if (response.Code == 200) != allowed {
				t.Fatal(response.Code, response.Body.String())
			}
			platform := PlatformService{Root: root, Store: service.Control.(*controlstore.Store), Accounts: service.Store}
			status, err := platform.SubscriptionStatus(t.Context())
			if err != nil || len(status.Outputs) != 1 || status.Outputs[0].Available != allowed {
				t.Fatal(status, err)
			}
			calls := 0
			client := &http.Client{Transport: testTransport(func(*http.Request) (*http.Response, error) { calls++; return byteResponse(nil), nil })}
			delivery := deliverSubscription(t.Context(), client, root, config, snapshot, sub.Delivery{ID: "device", Kind: "http-put", Output: "out", URL: "https://example.test/upload"}, sub.DeliveryState{}, service.Store)
			if (delivery.Status == "delivered") != allowed || (calls == 1) != allowed {
				t.Fatal(delivery, calls)
			}
		})
	}
}
