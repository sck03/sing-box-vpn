package manage

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/domain"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestAccountCompilationCacheBoundedAndConcurrent(t *testing.T) {
	cache := &accountExportCache{}
	var builds atomic.Int32
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(func() {
			_, err := cache.compile(t.Context(), "same-input", func() (accountCompilation, error) {
				builds.Add(1)
				return accountCompilation{artifact: sub.Artifact{Data: []byte("config")}}, nil
			})
			if err != nil {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	if builds.Load() != 1 {
		t.Fatal("duplicate compilation", builds.Load())
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := cache.compile(cancelled, "same-input", nil); err == nil {
		t.Fatal("cancelled request used cache")
	}
	cache.mu.Lock()
	e := cache.entries["same-input"]
	v := e.Value.(accountCacheEntry)
	v.expires = time.Now().Add(-time.Second)
	e.Value = v
	cache.mu.Unlock()
	if _, ok := cache.get("same-input"); ok {
		t.Fatal("expired compilation retained")
	}
	for i := range 200 {
		cache.remember(strconv.Itoa(i), accountCompilation{})
	}
	if len(cache.entries) > accountCacheEntries {
		t.Fatal("unbounded entries")
	}
	for i := range 20 {
		cache.remember("large"+strconv.Itoa(i), accountCompilation{artifact: sub.Artifact{Data: make([]byte, 1<<20)}})
	}
	if cache.bytes > accountCacheBytes {
		t.Fatal("unbounded bytes")
	}
}

func TestAccountExportCacheRespectsGrantAndCredentialChanges(t *testing.T) {
	root, _, app := appFixture(t)
	store, err := accounting.Open(filepath.Join(root, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	config := PlatformConfig{App: app, Subscriptions: sub.DefaultConfig()}
	cache := &accountExportCache{}
	service := AccountService{Root: root, Store: store, Config: &config, exports: cache}
	if err := store.SaveAccount(t.Context(), domain.Account{ID: "a", Name: "Account"}); err != nil {
		t.Fatal(err)
	}
	if err := service.Execute(t.Context(), application.AccountCommand{Operation: "bind", ID: "a", Scope: "normal"}, new(bytes.Buffer)); err != nil {
		t.Fatal(err)
	}
	token, err := store.CreateGrant(t.Context(), accounting.Grant{ID: "grant", AccountID: "a", Scope: "normal", Clients: []string{"uri"}})
	if err != nil {
		t.Fatal(err)
	}
	handler := unifiedDownloadHandler(service)
	usage := httptest.NewRecorder()
	handler.ServeHTTP(usage, httptest.NewRequest("GET", "/u/"+token+"/usage", nil))
	var page struct {
		Clients []AccountClientOption `json:"clients"`
	}
	if err := json.Unmarshal(usage.Body.Bytes(), &page); err != nil {
		t.Fatal(usage.Code, err, usage.Body.String())
	}
	if len(page.Clients) != 1 || page.Clients[0].Client != "uri" || len(cache.entries) != 1 {
		t.Fatal("compiled clients outside grant", page.Clients, len(cache.entries))
	}
	command := application.AccountCommand{ID: "a", Scope: "normal", Client: "uri"}
	first, err := service.ExportDetailed(t.Context(), command)
	if err != nil {
		t.Fatal(err)
	}
	first.Artifact.Data[0] = '!'
	next, err := service.ExportDetailed(t.Context(), command)
	if err != nil || next.Artifact.Data[0] == '!' || len(cache.entries) != 1 {
		t.Fatal("cache was mutated", err)
	}
	credentials, err := store.AccountCredentials(t.Context(), "a", "normal")
	if err != nil {
		t.Fatal(err)
	}
	c := credentials[0]
	if err := store.Rotate(t.Context(), c.ID, randomUUID(), randomSecret(), c.Generation); err != nil {
		t.Fatal(err)
	}
	rotated, err := service.ExportDetailed(t.Context(), command)
	if err != nil || rotated.Artifact.ETag == next.Artifact.ETag {
		t.Fatal("stale credentials served", err)
	}
	account, err := store.Account(t.Context(), "a")
	if err != nil {
		t.Fatal(err)
	}
	account.Disabled = true
	if err := store.SaveAccount(t.Context(), account.Account); err != nil {
		t.Fatal(err)
	}
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, httptest.NewRequest("GET", "/u/"+token+"/uri", nil))
	if denied.Code != http.StatusNotFound {
		t.Fatal("cached compilation bypassed account denial", denied.Code)
	}
	if err := store.RevokeGrant(t.Context(), "grant"); err != nil {
		t.Fatal(err)
	}
	denied = httptest.NewRecorder()
	handler.ServeHTTP(denied, httptest.NewRequest("GET", "/u/"+token+"/uri", nil))
	if denied.Code != http.StatusNotFound {
		t.Fatal("cached compilation bypassed revocation", denied.Code)
	}
}

func TestCachedAccountExportRequiresConfiguredValidator(t *testing.T) {
	t.Setenv("SBVPN_REQUIRE_CLIENT_CHECK", "")
	config := sub.DefaultConfig()
	inputs := accountExportInputs{
		config: config, templates: sub.EffectiveTemplates(config), digest: "same-input",
		nodes: []sub.Node{{Name: "node", Protocol: "trojan", Server: "example.com", Port: 443, Password: "secret", TLS: sub.TLS{Enabled: true}}},
	}
	service := AccountService{Root: t.TempDir(), exports: &accountExportCache{}}
	first, err := service.compileAccount(t.Context(), inputs, "account", "sing-box")
	if err != nil || first.option.Validation.State != "unavailable" || len(service.exports.entries) != 1 {
		t.Fatal(first.option, err)
	}
	t.Setenv("SBVPN_REQUIRE_CLIENT_CHECK", "1")
	strict, err := service.compileAccount(t.Context(), inputs, "account", "sing-box")
	if err == nil || strict.option.Available || strict.option.Validation.State != "failed" {
		t.Fatal("cached export bypassed required core validation", strict.option, err)
	}
}

// Isolates planning/rendering after the account and credentials are captured.
// Database reads, input hashing and network transfer are outside this benchmark.
func BenchmarkAccountCompilation(b *testing.B) {
	config := sub.DefaultConfig()
	inputs := accountExportInputs{config: config, templates: sub.EffectiveTemplates(config), digest: "benchmark-nodes"}
	for i := range 256 {
		name := "node-" + strconv.Itoa(i)
		inputs.nodes = append(inputs.nodes, sub.Node{Name: name, Protocol: "trojan", Server: name + ".example.com", Port: 443, Password: "secret", TLS: sub.TLS{Enabled: true}})
	}
	for _, name := range []string{"uncached", "cached"} {
		b.Run(name, func(b *testing.B) {
			service := AccountService{}
			if name == "cached" {
				service.exports = &accountExportCache{}
			}
			if _, err := service.compileAccount(b.Context(), inputs, "account", "uri"); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := service.compileAccount(b.Context(), inputs, "account", "uri"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestParserUpgradeDiscardsConditionalSourceCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != "" {
			t.Error("old parser reused conditional validator")
		}
		w.Header().Set("ETag", `"same-upstream-content"`)
		_, _ = w.Write([]byte("trojan://secret@example.com:443#fresh"))
	}))
	defer server.Close()
	source := sub.Source{ID: "source", Kind: "remote", Format: "uri", URL: server.URL}
	oldHash, _ := json.Marshal(struct {
		Parser int
		Source sub.Source
	}{sub.ParserVersion - 1, source})
	old := sub.SourceCache{SourceHash: sub.Digest(oldHash), ETag: `"same-upstream-content"`, Nodes: []sub.Node{{Name: "old"}}, Success: time.Now()}
	got := refreshSource(t.Context(), server.Client(), source, old, time.Now())
	if got.Error != "" || len(got.Nodes) != 1 || got.Nodes[0].Name != "fresh" || got.SourceHash != sub.SourceHash(source) {
		t.Fatal(got)
	}
}
