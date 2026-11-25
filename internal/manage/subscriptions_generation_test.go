package manage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
	"github.com/sck03/sing-box-vpn/internal/domain"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestRefreshRejectsUnconfirmedGeneration(t *testing.T) {
	root, core, app := appFixture(t)
	accounts, err := accounting.Open(filepath.Join(root, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer accounts.Close()
	store, err := controlstore.Open(filepath.Join(root, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	config := PlatformConfig{Schema: PlatformSchema, App: app, Subscriptions: sub.DefaultConfig()}
	config.Subscriptions.Sources = []sub.Source{{ID: "account", Kind: "account", AccountID: "a", Scope: "normal", Required: true}}
	config.Subscriptions.Collections = []sub.Collection{{ID: "account", Sources: []string{"account"}}}
	config.Subscriptions.Outputs = []sub.Output{{ID: "account", Collection: "account", Template: "normal-uri"}}
	if err := store.Initialize(t.Context(), mustJSON(t, config)); err != nil {
		t.Fatal(err)
	}
	service := AccountService{Root: root, Core: core, Store: accounts, Control: store, Config: &config}
	if err := accounts.SaveAccount(t.Context(), domain.Account{ID: "a", Name: "Account"}); err != nil {
		t.Fatal(err)
	}
	if err := service.Execute(t.Context(), application.AccountCommand{Operation: "bind", ID: "a", Scope: "normal", InboundIDs: testInboundIDs(t, service)}, io.Discard); err != nil {
		t.Fatal(err)
	}
	config.App.Address = "new.example.test"
	config.Subscriptions.Revision = 2
	if _, err := store.Queue(t.Context(), "apply", "test", []byte("{}"), 1, mustJSON(t, config)); err != nil {
		t.Fatal(err)
	}
	files, snapshot, _, err := compileSubscriptions(t.Context(), root, core, config.Subscriptions, sub.Runtime{}, nil, sub.Candidate, service)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(snapshot.Artifacts["account"].Data, []byte("new.example.test")) {
		t.Fatal("candidate not used")
	}
	if err := files.json("runtime-revision.json", int64(2)); err != nil {
		t.Fatal(err)
	}
	if err := publish(root, files); err != nil {
		t.Fatal(err)
	}
	before := readBytes(t, filepath.Join(root, subscriptionsSnapshot))
	report := application.RefreshResult{}
	_, err = publishSubscriptionRefresh(t.Context(), root, core, config.Subscriptions, nil, nil, nil, time.Now(), false, &report, compilePreparedSubscriptions)
	if !errors.Is(err, application.ErrConflict) {
		t.Fatal("unconfirmed candidate accepted", err)
	}
	if !bytes.Equal(before, readBytes(t, filepath.Join(root, subscriptionsSnapshot))) {
		t.Fatal("refresh replaced candidate")
	}
	if err := store.Applied(t.Context(), 2); err != nil {
		t.Fatal(err)
	}
	_, err = publishSubscriptionRefresh(t.Context(), root, core, config.Subscriptions, nil, nil, nil, time.Now(), false, &report, compilePreparedSubscriptions)
	if err != nil || !report.Published {
		t.Fatal("confirmed candidate rejected", err)
	}
	// A different generation arriving during unlocked compilation must be rejected.
	_, err = publishSubscriptionRefresh(t.Context(), root, core, config.Subscriptions, nil, nil, nil, time.Now(), false, &report, func(ctx context.Context, root, core string, c sub.Config, r sub.Runtime, local map[string][]sub.Node, mode sub.BuildMode) (fileBundle, sub.Snapshot, map[string]sub.Preview, error) {
		files, snapshot, preview, err := compilePreparedSubscriptions(ctx, root, core, c, r, local, mode)
		if e := AtomicJSON(filepath.Join(root, "runtime-revision.json"), int64(3)); e != nil {
			t.Fatal(e)
		}
		return files, snapshot, preview, err
	})
	if !errors.Is(err, application.ErrConflict) {
		t.Fatal("stale compilation committed", err)
	}
}

func TestInstalledPlatformRejectsMissingGeneration(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".platform-v5"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := confirmedSubscriptionConfig(t.Context(), root, sub.DefaultConfig()); err == nil {
		t.Fatal("missing generation accepted")
	}
}
