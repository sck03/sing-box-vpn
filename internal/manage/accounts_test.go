package manage

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/domain"
)

func TestAccountCommandsAndExportsKeepIndependentCredentials(t *testing.T) {
	root, _, _ := appFixture(t)
	service := AccountService{Root: root}
	outputs := map[string]string{}
	for _, id := range []string{"alice", "bob"} {
		for _, c := range []application.AccountCommand{{Operation: "save", Account: domain.Account{ID: id, Name: id}}, {Operation: "bind", ID: id, Scope: "normal"}} {
			if err := service.Execute(t.Context(), c, new(bytes.Buffer)); err != nil {
				t.Fatal(err)
			}
		}
		var output bytes.Buffer
		if err := service.Execute(t.Context(), application.AccountCommand{Operation: "export", ID: id, Scope: "normal", Client: "sing-box"}, &output); err != nil {
			t.Fatal(err)
		}
		var config struct {
			Outbounds []Object `json:"outbounds"`
		}
		if err := json.Unmarshal(output.Bytes(), &config); err != nil {
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
	var listing bytes.Buffer
	if err := service.Execute(t.Context(), application.AccountCommand{Operation: "list"}, &listing); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(listing.String(), outputs["alice"]) || strings.Contains(listing.String(), "password") {
		t.Fatal("default account listing leaks a credential")
	}
}

func TestAccountBatchBindingIsIndependentAndAtomic(t *testing.T) {
	root, _, _ := appFixture(t)
	svc := AccountService{Root: root}
	for _, id := range []string{"alice", "bob"} {
		if err := svc.Execute(t.Context(), application.AccountCommand{Operation: "save", Account: domain.Account{ID: id, Name: id}}, new(bytes.Buffer)); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := svc.accountEntries("normal")
	if err != nil {
		t.Fatal(err)
	}
	command := application.AccountCommand{Operation: "bind", Scope: "normal", AccountIDs: []string{"alice", "bob"}, InboundIDs: []string{entries[0].ID, "missing"}}
	if err = svc.Execute(t.Context(), command, new(bytes.Buffer)); err == nil {
		t.Fatal("unknown inbound accepted")
	}
	store, err := accounting.Open(filepath.Join(root, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	credentials, err := store.Credentials(t.Context(), "normal")
	if err != nil || len(credentials) != 0 {
		t.Fatal("partial binding", err)
	}
	command.InboundIDs = []string{entries[0].ID}
	if err = svc.Execute(t.Context(), command, new(bytes.Buffer)); err != nil {
		t.Fatal(err)
	}
	credentials, err = store.Credentials(t.Context(), "normal")
	if err != nil || len(credentials) != 2 || credentials[0].UUID == credentials[1].UUID || credentials[0].Password == credentials[1].Password {
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
