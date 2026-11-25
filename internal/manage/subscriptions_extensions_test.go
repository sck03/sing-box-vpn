package manage

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestSubscriptionAdminAuthenticationAndPreview(t *testing.T) {
	root, core, _ := appFixture(t)
	s, configuration, session := platformFixture(t, root, core)
	handler := s.Handler(PanelOptions{PublicURL: "http://example.com"})
	for _, tc := range []struct {
		path, token, origin string
		status              int
	}{
		{"/", "", "", 200}, {"/api/config", "", "", 401},
		{"/api/config", strings.Repeat("a", 64), "", 401},
		{"/api/config", session.Token, "https://evil.example", 403},
		{"/api/config", session.Token, "", 200},
	} {
		r := httptest.NewRequest("GET", "http://example.com"+tc.path, nil)
		r.AddCookie(&http.Cookie{Name: "sbvpn", Value: tc.token})
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s: status %d", tc.path, w.Code)
		}
	}
	before := readBytes(t, filepath.Join(root, subscriptionsSnapshot))
	data, _ := json.Marshal(configuration)
	r := panelRequest("POST", "/api/config/preview", data, session)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 || !bytes.Equal(before, readBytes(t, filepath.Join(root, subscriptionsSnapshot))) {
		t.Fatal("preview changed published state", w.Body.String())
	}
	if err := s.Serve(context.Background(), PanelOptions{Listen: "0.0.0.0:9099"}, io.Discard); err == nil {
		t.Fatal("admin accepted public binding")
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSubscriptionDeliverySnapshotAndUnchanged(t *testing.T) {
	root, core, _ := appFixture(t)
	var uploads atomic.Int32
	var received []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("bad delivery authentication/method")
		}
		received, _ = io.ReadAll(r.Body)
		uploads.Add(1)
		w.WriteHeader(204)
	}))
	defer server.Close()
	c, _, _, _ := loadSubscriptions(root)
	c.Deliveries = []sub.Delivery{{ID: "device", Kind: "http-put", Output: "normal-provider", URL: server.URL, Token: "test-token"}}
	if err := AtomicJSON(filepath.Join(root, subscriptionsConfig), c); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := deliverSubscriptions(context.Background(), root, "device", false, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	if uploads.Load() != 1 || !bytes.Equal(received, readArtifact(t, root, "normal-provider")) {
		t.Fatal("delivery duplicated or used a different snapshot")
	}
	_ = core
}

func TestSubscriptionWebDAVAndSecretGistDelivery(t *testing.T) {
	artifact := sub.Artifact{Data: []byte("proxies: []\n"), MIME: "text/yaml"}
	t.Run("webdav", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, password, ok := r.BasicAuth()
			body, _ := io.ReadAll(r.Body)
			if !ok || user != "device" || password != "test-password" || r.Method != "PUT" || !bytes.Equal(body, artifact.Data) {
				t.Error("WebDAV must send authenticated PUT with exact snapshot")
			}
			w.WriteHeader(http.StatusCreated)
		}))
		defer server.Close()
		err := sendSubscriptionArtifact(context.Background(), server.Client(), sub.Delivery{Kind: "webdav", URL: server.URL, Username: "device", Password: "test-password"}, artifact)
		if err != nil {
			t.Fatal(err)
		}
	})
	for _, visibility := range []string{`{"public":false}`, `{"public":true}`, `{}`, `{"public":null}`, `invalid`} {
		t.Run("gist-"+visibility, func(t *testing.T) {
			var uploads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("missing Gist authentication")
				}
				if r.Method == http.MethodGet {
					_, _ = io.WriteString(w, visibility)
					return
				}
				uploads.Add(1)
				var body struct {
					Files map[string]struct{ Content string } `json:"files"`
				}
				if r.Method != http.MethodPatch || r.Header.Get("Content-Type") != "application/json" || json.NewDecoder(r.Body).Decode(&body) != nil || body.Files["provider.yaml"].Content != string(artifact.Data) {
					t.Error("invalid Gist upload")
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			err := sendSubscriptionArtifact(context.Background(), server.Client(), sub.Delivery{Kind: "github-gist", URL: server.URL, Token: "test-token", Filename: "provider.yaml"}, artifact)
			if visibility == `{"public":false}` {
				if err != nil || uploads.Load() != 1 {
					t.Fatal("secret Gist upload failed", err)
				}
			} else if err == nil || uploads.Load() != 0 {
				t.Fatal("uploaded without verified secret visibility", err)
			}
		})
	}
}

func TestSubscriptionCustomOutputsAndRevokedUsageCleanup(t *testing.T) {
	c := fixtureSubscriptionConfig()
	c.Outputs = c.Outputs[:1]
	c.Outputs[0].ID = "custom"
	root, token := t.TempDir(), strings.Repeat("b", 64)
	subscriptionFixture(t, root, token, "content")
	store, err := accounting.Open(filepath.Join(root, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.RevokeGrant(t.Context(), "fixture"); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	subscriptionHandler(root).ServeHTTP(response, httptest.NewRequest("GET", "/s/"+token+"/normal-sing-box", nil))
	if response.Code != 404 {
		t.Fatal("revoked grant remains usable")
	}

}

func TestSubscriptionApplyPrunesUnusedResourcesWithoutRemoteSources(t *testing.T) {
	root, core, _ := appFixture(t)
	c, r, _, err := loadSubscriptions(root)
	if err != nil {
		t.Fatal(err)
	}
	r.Sources = map[string]sub.SourceCache{}
	r.Resources = map[string]sub.ResourceCache{"rules:deleted": {URL: "https://example.com/old"}}
	r.Deliveries = map[string]sub.DeliveryState{"deleted": {Digest: "old"}}
	if err := AtomicJSON(filepath.Join(root, subscriptionsRuntime), r); err != nil {
		t.Fatal(err)
	}
	service, config, _ := platformFixture(t, root, core)
	config.Subscriptions = c
	if err := applyFixture(t, service, config, 1); err != nil {
		t.Fatal(err)
	}
	_, r, _, err = loadSubscriptions(root)
	if err != nil || len(r.Resources) != 0 || len(r.Deliveries) != 0 {
		t.Fatal("unused resource state retained without source caches", err)
	}

}

func TestSubscriptionOnDemandProtocolProbe(t *testing.T) {
	root, core, app := appFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var probeServer Object
	if _, err := ReadJSON(filepath.Join(root, "sb.json"), &probeServer); err != nil {
		t.Fatal(err)
	}
	for _, inbound := range objects(probeServer["inbounds"]) {
		kind := text(inbound["type"])
		credential := fixtureCredential(kind)
		user := Object{}
		if credential.UUID != "" {
			user["uuid"] = credential.UUID
		}
		if credential.Password != "" {
			user["password"] = credential.Password
		}
		if flow := inbound["_managed_flow"]; flow != nil {
			user["flow"] = flow
		}
		delete(inbound, "_managed_flow")
		inbound["users"] = []Object{user}
	}
	probePath := filepath.Join(root, "probe-fixture.json")
	if err := AtomicJSON(probePath, probeServer); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, core, "run", "-c", probePath)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	local, err := fixtureSubscriptionNodes(app, VPNSettings{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range local[normalScope] {
		if node.Protocol == "anytls" {
			// Starting the process does not mean its listener is ready. A mixed
			// proxy correctly returns 502 while the upstream is still starting.
			for {
				conn, err := net.DialTimeout("tcp", net.JoinHostPort(node.Server, strconv.Itoa(node.Port)), 200*time.Millisecond)
				if err == nil {
					conn.Close()
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("probe fixture did not start", ctx.Err())
				case <-time.After(50 * time.Millisecond):
				}
			}
			if err := probeSubscriptionNode(ctx, root, core, server.URL, node); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("missing probe node")
}

// Opt-in sustained traffic; reports actual duration, heap and goroutine recovery.
func TestSubscriptionSoak(t *testing.T) {
	value := os.Getenv("RUN_SUBSCRIPTION_SOAK")
	if value == "" {
		t.Skip("set RUN_SUBSCRIPTION_SOAK, e.g. 10m")
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < time.Minute {
		t.Fatal("soak duration must be at least one minute")
	}
	root := t.TempDir()
	token := strings.Repeat("a", 64)
	subscriptionFixture(t, root, token, strings.Repeat("x", 1024))
	server := httptest.NewServer(subscriptionHandler(root))
	client := server.Client()
	endpoint := server.URL + "/s/" + token + "/normal-sing-box"
	for range 50 {
		response, err := client.Get(endpoint)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	goroutines := runtime.NumGoroutine()
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	var count, failed atomic.Uint64
	var workers sync.WaitGroup
	started := time.Now()
	for range 8 {
		workers.Go(func() {
			ticker := time.NewTicker(20 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					request, _ := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
					response, err := client.Do(request)
					if err != nil {
						if ctx.Err() == nil {
							failed.Add(1)
						}
						continue
					}
					if response.StatusCode != 200 {
						failed.Add(1)
					}
					_, err = io.Copy(io.Discard, response.Body)
					response.Body.Close()
					if err != nil && ctx.Err() == nil {
						failed.Add(1)
					}
					count.Add(1)
				}
			}
		})
	}
	workers.Wait()
	client.CloseIdleConnections()
	server.Close()
	runtime.GC()
	runtime.ReadMemStats(&after)
	t.Logf("duration=%s requests=%d failures=%d heap_before=%d heap_after=%d goroutines_before=%d goroutines_after=%d", time.Since(started), count.Load(), failed.Load(), before.HeapAlloc, after.HeapAlloc, goroutines, runtime.NumGoroutine())
	if failed.Load() != 0 || after.HeapAlloc > before.HeapAlloc+16<<20 || runtime.NumGoroutine() > goroutines+4 {
		t.Fatal("sustained traffic did not recover resources")
	}
}
