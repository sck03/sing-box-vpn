package manage

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

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

func TestAccountHTTPAuthenticationAndRevision(t *testing.T) {
	root, core, _ := appFixture(t)
	token := strings.Repeat("a", 43)
	handler := subscriptionAdminHandler(root, core, token)
	r := httptest.NewRequest("GET", "/api/accounts", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	command := application.AccountCommand{Operation: "save", Account: domain.Account{ID: "alice", Name: "Alice"}}
	for _, rev := range []int64{0, 1, 1} {
		command.Account.Revision = rev
		r = httptest.NewRequest("POST", "/api/accounts", bytes.NewReader(mustJSON(t, command)))
		r.Header.Set("Authorization", "Bearer "+token)
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		expected := 200
		if rev == 1 && command.Account.Name == "updated" {
			expected = 409
		}
		if w.Code != expected {
			t.Fatal(w.Code, w.Body.String())
		}
		if rev == 1 {
			command.Account.Name = "updated"
		}
	}
}
