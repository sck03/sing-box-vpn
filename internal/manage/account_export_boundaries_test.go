package manage

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/domain"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestAccountExportAcceptsMaxResourceIDs(t *testing.T) {
	root, core, _ := appFixture(t)
	service, config, _ := platformFixture(t, root, core)
	var entry ManagedNode
	for _, node := range config.App.Nodes {
		if node.Protocol == "vmess" {
			entry = node
		}
	}
	entry.ID = strings.Repeat("n", 64)
	config.App.Nodes = []ManagedNode{entry}
	config.Argo.Inbound, config.tunnelDomain = entry.ID, "tunnel.example.test"
	account := domain.Account{ID: strings.Repeat("a", 64), Name: strings.Repeat("名", 42) + "ab"}
	ctx := t.Context()
	if err := service.Accounts.SaveAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	credential := domain.Credential{ID: strings.Repeat("c", 64), AccountID: account.ID, RuntimeID: normalScope, InboundID: entry.ID, Protocol: entry.Protocol, Generation: 1, UUID: randomUUID()}
	if err := service.Accounts.BindBatch(ctx, []domain.Credential{credential}); err != nil {
		t.Fatal(err)
	}
	usage, err := service.Accounts.DownloadAccount(ctx, account.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var previous []sub.Node
	for range 2 {
		nodes, err := accountNodes(ctx, service.Accounts, config, usage, normalScope, nil)
		if err != nil || len(nodes) != 2 {
			t.Fatal("valid maximum-length identities must export both endpoints", nodes, err)
		}
		for i, node := range nodes {
			if err := node.Validate(); err != nil {
				t.Fatal(err)
			}
			if previous != nil && previous[i].ID != node.ID {
				t.Fatal("export identity changed between identical reads")
			}
		}
		if nodes[0].ID == nodes[1].ID || nodes[1].Server != config.tunnelDomain {
			t.Fatal("Argo lost its independent identity", nodes)
		}
		previous = nodes
	}
}

func TestAccountNameErrorsIdentifySaveField(t *testing.T) {
	for _, name := range []string{"", "Line\nBreak", "Line\rBreak", "NUL\x00", strings.Repeat("名", 43)} {
		err := (domain.Account{ID: "account", Name: name}).Validate()
		var field *domain.FieldError
		if !errors.As(err, &field) || field.Field != "account.name" {
			t.Fatalf("%q: %v", name, err)
		}
		response := httptest.NewRecorder()
		platformError(response, err)
		if response.Code != 400 || !strings.Contains(response.Body.String(), `"field":"account.name"`) {
			t.Fatal(response.Code, response.Body.String())
		}
	}
}
