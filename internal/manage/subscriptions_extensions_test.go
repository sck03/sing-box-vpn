package manage

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestSubscriptionAdminAuthenticationAndPreview(t *testing.T) {
	root, core, _ := appFixture(t)
	handler := subscriptionAdminHandler(root, core, strings.Repeat("b", 43))
	for _, tc := range []struct {
		path, token, origin string
		status              int
	}{
		{"/", "", "", 200}, {"/api/config", "", "", 401},
		{"/api/config", strings.Repeat("a", 43), "", 401},
		{"/api/config", strings.Repeat("b", 43), "https://evil.example", 403},
		{"/api/config", strings.Repeat("b", 43), "", 200},
	} {
		r := httptest.NewRequest("GET", tc.path, nil)
		r.Header.Set("Authorization", "Bearer "+tc.token)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s: status %d", tc.path, w.Code)
		}
	}
	c, _, _, _ := loadSubscriptions(root)
	before := readBytes(t, filepath.Join(root, subscriptionsSnapshot))
	data, _ := json.Marshal(adminRequest{Config: mustJSON(t, c)})
	r := httptest.NewRequest("POST", "/api/preview", bytes.NewReader(data))
	r.Header.Set("Authorization", "Bearer "+strings.Repeat("b", 43))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 || !bytes.Equal(before, readBytes(t, filepath.Join(root, subscriptionsSnapshot))) {
		t.Fatal("preview changed published state", w.Body.String())
	}
	if err := serveSubscriptionAdmin(context.Background(), root, core, "0.0.0.0:9099", io.Discard); err == nil {
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
	if uploads.Load() != 1 || !bytes.Equal(received, readBytes(t, filepath.Join(root, "proxies.yaml"))) {
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
	c := sub.DefaultConfig()
	c.Outputs = c.Outputs[:1]
	c.Outputs[0].ID = "custom"
	if err := ensureSubscriptionGrant(&c, strings.Repeat("a", 43)); err != nil || len(c.Grants) != 0 {
		t.Fatal("custom outputs must use explicit grants", err)
	}
	root, token := t.TempDir(), strings.Repeat("b", 43)
	c = subscriptionFixture(t, root, token, "content")
	usage := filepath.Join(root, "subscriptions/usage", c.Grants[0].TokenHash+".json")
	if err := AtomicJSON(usage, uint64(9)); err != nil {
		t.Fatal(err)
	}
	if err := RunSubscriptions(context.Background(), []string{"--root", root, "revoke", "--id", c.Grants[0].ID}, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(usage); !os.IsNotExist(err) {
		t.Fatal("revoked usage remains", err)
	}
	var links bytes.Buffer
	if err := subscriptionURLs(root, "127.0.0.1", "normal", &links); err != nil || strings.Contains(links.String(), "/s/") {
		t.Fatal("read-only display emitted revoked links", err, links.String())
	}
}

func TestSubscriptionApplyPrunesUnusedResourcesWithoutRemoteSources(t *testing.T) {
	root, core, app := appFixture(t)
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
	file := filepath.Join(t.TempDir(), "config.json")
	if err := AtomicJSON(file, c); err != nil {
		t.Fatal(err)
	}
	if err := RunSubscriptions(context.Background(), []string{"--root", root, "--core", core, "apply", "--file", file}, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	_, r, _, err = loadSubscriptions(root)
	if err != nil || len(r.Resources) != 0 || len(r.Deliveries) != 0 {
		t.Fatal("unused resource state retained without source caches", err)
	}
	before := readBytes(t, filepath.Join(root, subscriptionsConfig))
	args := []string{"--root", root, "grant", "--id", "phone", "--outputs", "normal-provider", "--max-downloads", "2"}
	if err := RunSubscriptions(context.Background(), args, nil, io.Discard); err == nil || !bytes.Equal(before, readBytes(t, filepath.Join(root, subscriptionsConfig))) {
		t.Fatal("unconfigured share must fail without changing authorization", err)
	}
	if err := configureSubscription(root, app, VPNSettings{}, 0); err != nil {
		t.Fatal(err)
	}
	var links bytes.Buffer
	if err := RunSubscriptions(context.Background(), args, nil, &links); err != nil || !strings.Contains(links.String(), "/normal-provider") {
		t.Fatal("configured share was not created", err)
	}
}

func TestSubscriptionOnDemandProtocolProbe(t *testing.T) {
	root, core, app := appFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, core, "run", "-c", filepath.Join(root, "sb.json"))
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	local, err := collectLocalSubscriptionNodes(app, VPNSettings{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range local[normalScope] {
		if node.Protocol == "anytls" {
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
	token := strings.Repeat("a", 43)
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
