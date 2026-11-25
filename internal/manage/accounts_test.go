package manage

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/domain"
)

func TestAccountCommandsAndExportsKeepIndependentCredentials(t *testing.T) {
	root, core, app := appFixture(t)
	store, err := accounting.Open(filepath.Join(root, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	config := PlatformConfig{App: app}
	service := AccountService{Root: root, Core: core, Store: store, Config: &config}
	outputs := map[string]string{}
	for _, id := range []string{"alice", "bob"} {
		if err := store.SaveAccount(t.Context(), domain.Account{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
		if err := service.Execute(t.Context(), application.AccountCommand{Operation: "bind", ID: id, Scope: "normal", InboundIDs: testInboundIDs(t, service)}, new(bytes.Buffer)); err != nil {
			t.Fatal(err)
		}
		output, err := service.ExportDetailed(t.Context(), application.AccountExportRequest{ID: id, Scope: "normal", Profile: "sing-box-config"})
		if err != nil {
			t.Fatal(err)
		}
		var config struct {
			Outbounds []Object `json:"outbounds"`
		}
		if err := json.Unmarshal(output.Artifact.Data, &config); err != nil {
			t.Fatal(err)
		}
		for _, node := range config.Outbounds {
			if node["type"] == "vless" {
				outputs[id] = text(node["uuid"])
			}
		}
	}
	if outputs["alice"] == "" || outputs["alice"] == outputs["bob"] {
		t.Fatal("accounts share a credential")
	}
	page, err := store.AccountPage(t.Context(), application.PageQuery{Limit: 20}, time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}
	listing, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(listing), outputs["alice"]) || strings.Contains(string(listing), "password") {
		t.Fatal("default account listing leaks a credential")
	}
}

func TestAccountBatchBindingIsIndependentAndAtomic(t *testing.T) {
	root, _, app := appFixture(t)
	store, err := accounting.Open(filepath.Join(root, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	config := PlatformConfig{App: app}
	svc := AccountService{Root: root, Store: store, Config: &config}
	for _, id := range []string{"alice", "bob"} {
		if err := store.SaveAccount(t.Context(), domain.Account{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := svc.accountEntries(t.Context(), "normal")
	if err != nil {
		t.Fatal(err)
	}
	command := application.AccountCommand{Operation: "bind", Scope: "normal", AccountIDs: []string{"alice", "bob"}, InboundIDs: []string{entries[0].ID, "missing"}}
	if err = svc.Execute(t.Context(), command, new(bytes.Buffer)); err == nil {
		t.Fatal("unknown inbound accepted")
	}
	credentials, err := store.Credentials(t.Context(), "normal")
	if err != nil || len(credentials) != 0 {
		t.Fatal("partial binding", err)
	}
	command.InboundIDs = []string{entries[0].ID}
	if err = svc.Execute(t.Context(), command, new(bytes.Buffer)); err != nil {
		t.Fatal(err)
	}
	credentials, err = store.Credentials(t.Context(), "normal")
	if err != nil || len(credentials) != 2 || credentials[0].Identity() == credentials[1].Identity() {
		t.Fatal("batch did not create independent credentials", err)
	}
	one := credentials[0]
	one.ID = "new-id"
	one.InboundID = "another"
	two := credentials[1]
	two.ID = "conflict-id"
	if err = store.BindBatch(t.Context(), []domain.Credential{one, two}); err == nil {
		t.Fatal("duplicate authentication accepted")
	}
	credentials, err = store.Credentials(t.Context(), "normal")
	if err != nil || len(credentials) != 2 {
		t.Fatal("batch was not atomic", err)
	}
}
