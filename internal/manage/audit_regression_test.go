package manage

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/database"
	"github.com/sck03/sing-box-vpn/internal/domain"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestResourceCacheSurvivesRebuildAnd304(t *testing.T) {
	root, core, _ := appFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == "r1" {
			w.WriteHeader(304)
			return
		}
		w.Header().Set("ETag", "r1")
		w.Write([]byte(`[{"type":"domain","value":"cache.example","target":"DIRECT"}]`))
	}))
	defer server.Close()
	c, _, _, err := loadSubscriptions(root)
	if err != nil {
		t.Fatal(err)
	}
	c.RuleSets = []sub.RuleSet{{ID: "remote", URL: server.URL}}
	if err = AtomicJSON(filepath.Join(root, subscriptionsConfig), c); err != nil {
		t.Fatal(err)
	}
	before := readBytes(t, filepath.Join(root, subscriptionsConfig))
	for range 2 {
		if err = refreshSubscriptions(t.Context(), root, core, true); err != nil {
			t.Fatal(err)
		}
		current, state, local, err := loadSubscriptions(root)
		if err != nil {
			t.Fatal(err)
		}
		effective, err := sub.ResolveResources(current, state.Resources)
		if err != nil || len(effective.RuleSets[0].Rules) != 1 {
			t.Fatal(effective, err)
		}
		if _, _, _, err = compileSubscriptions(t.Context(), root, core, current, state, local, sub.Available); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, readBytes(t, filepath.Join(root, subscriptionsConfig))) {
			t.Fatal("refresh edited authoritative config")
		}
	}
}

func TestDownloadOriginAndIdleScheduler(t *testing.T) {
	root, core, _ := appFixture(t)
	s, c, _ := platformFixture(t, root, core)
	c.Subscriptions.Sources, c.Subscriptions.Collections, c.Subscriptions.Outputs = nil, nil, nil
	c.Listener.Enabled, c.Listener.PublicBaseURL = true, "https://download.example/sub/"
	id, err := s.Store.Queue(t.Context(), "apply", "test", []byte(`{}`), 1, mustJSON(t, c))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.Applied(t.Context(), 2); err != nil {
		t.Fatal(err)
	}
	if err = s.Store.Finish(t.Context(), id, nil); err != nil {
		t.Fatal(err)
	}
	if got, err := s.grantOptions(t.Context(), "https://admin.example"); err != nil || got.origin != "https://download.example/sub" {
		t.Fatal(got, err)
	}
	if err = AtomicJSON(filepath.Join(root, subscriptionsConfig), c.Subscriptions); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err = s.scheduleSubscriptions(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	db, err := database.Open(filepath.Join(root, database.Filename))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM control_tasks WHERE state='queued'`).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

func TestDisabledSubscriberBlocksCustomOutputAndExport(t *testing.T) {
	root, core, _ := appFixture(t)
	s, _, _ := platformFixture(t, root, core)
	ctx := t.Context()
	if err := s.Accounts.SaveSubscriber(ctx, accounting.Subscriber{ID: "person", Name: "Person", Disabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.Accounts.SaveAccount(ctx, domain.Account{ID: "a", Name: "A", SubscriberID: "person"}); err != nil {
		t.Fatal(err)
	}
	c := fixtureSubscriptionConfig()
	c.Sources = []sub.Source{{ID: "a", Kind: "account", AccountID: "a"}}
	c.Collections = []sub.Collection{{ID: "all", Sources: []string{"a"}}}
	c.Outputs = []sub.Output{{ID: "custom", Collection: "all", Template: c.Templates[0].ID}}
	if err := checkOutputAccounts(ctx, root, c, "custom", s.Accounts); err == nil {
		t.Fatal("disabled subscriber authorized")
	}
	if _, err := (s.accountService()).ExportDetailed(ctx, application.AccountExportRequest{ID: "a", Profile: "uri"}); err == nil {
		t.Fatal("disabled subscriber exported")
	}
}

func TestLargePlatformBackupRoundTrip(t *testing.T) {
	root, core, _ := appFixture(t)
	s, _, _ := platformFixture(t, root, core)
	db, err := database.Open(filepath.Join(root, database.Filename))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Enlarge a real database without changing its schema.
	if _, err = db.Exec(`INSERT INTO observations(name,value) VALUES('large',?)`, strings.Repeat("x", 7<<20)); err != nil {
		t.Fatal(err)
	}
	password := []byte("large-backup-password")
	data, err := s.Backup(t.Context(), password)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) <= 7<<20 {
		t.Fatal("fixture does not cover large binary archive")
	}
	backup, err := decodePlatformBackup(data, password)
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	info, err := os.Stat(backup.Database)
	if err != nil || info.Size() <= 7<<20 {
		t.Fatal(info, err)
	}
	if response := restoreRequest(t, data, map[string]string{"password": string(password)}); response.Code != 200 {
		t.Fatal("binary upload rejected", response.Code)
	}
	if err = RestoreDisaster(t.Context(), filepath.Join(t.TempDir(), "restored"), data, password, "recovered", "recovered-password"); err != nil {
		t.Fatal(err)
	}
}

func TestGatewayStateUsesDurableConfiguration(t *testing.T) {
	root, core, _ := appFixture(t)
	s, _, _ := platformFixture(t, root, core)
	ctx := t.Context()
	state, err := s.Store.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := decodePlatform(state.Desired)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.PrepareCountry(ctx, initial, "JP")
	if err != nil {
		t.Fatal(err)
	}
	c.Subscriptions.Sources, c.Subscriptions.Collections, c.Subscriptions.Outputs = nil, nil, nil
	id, err := s.Store.Queue(ctx, "apply", "setup", []byte(`{}`), 1, mustJSON(t, c))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.Applied(ctx, 2); err != nil {
		t.Fatal(err)
	}
	if err = s.Store.Finish(ctx, id, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Queue(ctx, application.TaskCommand{Operation: "gateway", Config: json.RawMessage(`{"op":"stop"}`)}, "operator"); err != nil {
		t.Fatal(err)
	}
	state, err = s.Store.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	desired, err := decodePlatform(state.Desired)
	if err != nil || desired.Gateway["enabled"] != false || state.Revision != 3 || state.AppliedRevision != 2 {
		t.Fatal(state.Revision, desired.Gateway, err)
	}
	if _, err = s.Queue(ctx, application.TaskCommand{Operation: "gateway", Config: json.RawMessage(`{"op":"start"}`)}, "operator"); err == nil {
		t.Fatal("pending configuration was overwritten")
	}
	// A fresh process reads the durable desired state instead of the projection.
	other, err := OpenPlatform(root, core, core, true)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	state, err = other.Store.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	desired, err = decodePlatform(state.Desired)
	if err != nil || desired.Gateway["enabled"] != false {
		t.Fatal(desired.Gateway, err)
	}
}
