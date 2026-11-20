package manage

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
	"github.com/sck03/sing-box-vpn/internal/domain"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestUnpublishedRemoteTemplateOnlyBlocksItsOutput(t *testing.T) {
	root, core, app := appFixture(t)
	store, err := accounting.Open(filepath.Join(root, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	config := PlatformConfig{App: app, Subscriptions: sub.DefaultConfig()}
	config.Subscriptions.Templates = append(config.Subscriptions.Templates, sub.Template{
		ID: "remote", Client: "mihomo", Mode: "config", Version: 1, URL: "https://example.test/template",
	})
	config.Subscriptions.AccountDefaults = map[string]sub.AccountExportPolicy{
		"mihomo-config": {Template: "remote", SkipUnsupported: true},
	}
	config.Schema = PlatformSchema
	service := AccountService{Root: root, Core: core, Store: store, Config: &config}
	if err := store.SaveAccount(t.Context(), domain.Account{ID: "a", Name: "Account"}); err != nil {
		t.Fatal(err)
	}
	if err := service.Execute(t.Context(), application.AccountCommand{Operation: "bind", ID: "a", Scope: "normal"}, new(bytes.Buffer)); err != nil {
		t.Fatal(err)
	}
	options, err := service.OutputOptions(t.Context(), "a", "normal", []string{"uri", "mihomo-provider", "mihomo-config"})
	if err != nil {
		t.Fatal(err)
	}
	if !options[0].Available || !options[1].Available || options[2].Available || options[2].Mode != "config" {
		t.Fatal(options)
	}
	output, err := service.ExportDetailed(t.Context(), application.AccountExportRequest{ID: "a", Scope: "normal", Profile: "uri"})
	if err != nil || len(output.Artifact.Data) == 0 {
		t.Fatal(err)
	}
	_, err = service.ExportDetailed(t.Context(), application.AccountExportRequest{ID: "a", Scope: "normal", Profile: "mihomo-config"})
	if err == nil {
		t.Fatal("unpublished remote template was used")
	}
}

func TestRemoteTemplateSnapshotFailuresPreserveBuiltins(t *testing.T) {
	root, core, app := appFixture(t)
	store, err := accounting.Open(filepath.Join(root, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	control, err := controlstore.Open(filepath.Join(root, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	config := PlatformConfig{Schema: PlatformSchema, App: app, Subscriptions: sub.DefaultConfig()}
	remote := sub.Template{ID: "remote", Client: "mihomo", Mode: "config", Version: 1, URL: "https://example.test/template"}
	config.Subscriptions.Templates = append(config.Subscriptions.Templates, remote)
	config.Subscriptions.AccountDefaults = map[string]sub.AccountExportPolicy{"mihomo-config": {Template: "remote", SkipUnsupported: true}}
	if err := control.Initialize(t.Context(), mustJSON(t, config)); err != nil {
		t.Fatal(err)
	}
	service := AccountService{Root: root, Core: core, Store: store, Control: control, Config: &config}
	if err := store.SaveAccount(t.Context(), domain.Account{ID: "a", Name: "Account"}); err != nil {
		t.Fatal(err)
	}
	if err := service.Execute(t.Context(), application.AccountCommand{Operation: "bind", ID: "a", Scope: "normal"}, new(bytes.Buffer)); err != nil {
		t.Fatal(err)
	}
	service.Config = nil
	if err := AtomicJSON(filepath.Join(root, subscriptionsConfig), config.Subscriptions); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"missing", "corrupt", "stale", "published"} {
		t.Run(state, func(t *testing.T) {
			path := filepath.Join(root, subscriptionsSnapshot)
			if state == "missing" {
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
			} else if state == "corrupt" {
				if err := AtomicBytes(path, []byte("{broken"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				revision := config.Subscriptions.Revision
				if state == "stale" {
					revision++
				}
				published := sub.Snapshot{Version: sub.SchemaVersion, Revision: revision, Templates: map[string]sub.Template{"remote": remote}}
				if err := AtomicJSON(path, published); err != nil {
					t.Fatal(err)
				}
			}
			options, err := service.OutputOptions(t.Context(), "a", "normal", []string{"uri", "mihomo-config"})
			if err != nil {
				t.Fatal(err)
			}
			if !options[0].Available || options[1].Available != (state == "published") {
				t.Fatal(options)
			}
		})
	}
}
