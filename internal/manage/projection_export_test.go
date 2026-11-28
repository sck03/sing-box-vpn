package manage

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/domain"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestGatewaySourceProjectionAndDiagnostics(t *testing.T) {
	secret := "https://source.example/nodes?token=synthetic-source-secret"
	config := PlatformConfig{Schema: PlatformSchema, Gateway: Object{"sources": []string{secret}}}
	data, err := encodePlatform(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, privileged := range []bool{false, true} {
		view, err := configurationView(data, privileged)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(view)
		if bytes.Contains(encoded, []byte("synthetic-source-secret")) != privileged {
			t.Fatal("source URL projection did not respect the role")
		}
	}
	if diagnostic := validationDiagnostic([]byte("failed to load "+secret), data); strings.Contains(diagnostic, secret) || !strings.Contains(diagnostic, "[redacted]") {
		t.Fatal("source URL leaked into validation diagnostics")
	}
	value := map[string]any{"config": map[string]any{"gateway": map[string]any{"sources": []any{secret}}}, "collection": map[string]any{"sources": []any{"source-id"}}}
	redactPlatform(value)
	encoded, _ := json.Marshal(value)
	if bytes.Contains(encoded, []byte(secret)) || !bytes.Contains(encoded, []byte("source-id")) {
		t.Fatal("nested projection leaked a URL or erased non-secret source references")
	}
}

func TestAccountExportOverridesAreScopedToBoundInbound(t *testing.T) {
	root, core, _ := appFixture(t)
	s, config, _ := platformFixture(t, root, core)
	var base ManagedNode
	for _, node := range config.App.Nodes {
		if node.Protocol == "tuic" {
			base = node
		}
	}
	first, second := base, base
	first.ID, first.Port, first.UDPPorts = "tuic-a", 61001, []string{"62001"}
	second.ID, second.Port, second.UDPPorts = "tuic-b", 61002, []string{"62002"}
	config.App.Nodes = []ManagedNode{first, second}
	if err := s.Accounts.SaveAccount(t.Context(), domain.Account{ID: "ports", Name: "Ports"}); err != nil {
		t.Fatal(err)
	}
	for _, node := range config.App.Nodes {
		if err := s.Accounts.BindBatch(t.Context(), []domain.Credential{{ID: node.ID, AccountID: "ports", RuntimeID: normalScope, InboundID: node.ID, Protocol: "tuic", Generation: 1, UUID: randomUUID(), Password: randomSecret()}}); err != nil {
			t.Fatal(err)
		}
	}
	u, err := s.Accounts.DownloadAccount(t.Context(), "ports", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, overrides := range []map[string]int{nil, {"normal/tuic-a": 62001}, {"normal/tuic-a": 62001, "normal/tuic-b": 62002}} {
		nodes, err := accountNodes(t.Context(), s.Accounts, config, u, normalScope, overrides)
		if err != nil || len(nodes) != 2 {
			t.Fatal(nodes, err)
		}
		for _, node := range nodes {
			id := strings.TrimPrefix(node.Name, "Ports-")
			want := 61001
			if id == "tuic-b" {
				want = 61002
			}
			if override, ok := overrides["normal/"+id]; ok {
				want = override
			}
			if node.Port != want {
				t.Fatalf("%s: got %d want %d", id, node.Port, want)
			}
		}
	}
	for _, overrides := range []map[string]int{{"normal/tuic-a": 62002}, {"normal/missing": 62001}, {"vpngate/tuic-a": 62001}, {"normal/tuic-a": 0}} {
		if _, err := accountNodes(t.Context(), s.Accounts, config, u, normalScope, overrides); err == nil {
			t.Fatal("invalid override accepted", overrides)
		}
	}
	service := AccountService{Root: root, Store: s.Accounts, Config: &config}
	options, err := service.ExportPortOptions(t.Context(), u.ID)
	if err != nil || len(options) != 2 {
		t.Fatal(options, err)
	}
}

func TestCredentialPublicationBuildOnlyAffectedRuntime(t *testing.T) {
	root, core, _ := appFixture(t)
	s, config, _ := platformFixture(t, root, core)
	// An unrelated broken input cache must not block a credential-only publish.
	if err := os.WriteFile(filepath.Join(root, subscriptionsRuntime), []byte("invalid cache"), 0600); err != nil {
		t.Fatal(err)
	}
	s.Core = filepath.Join(root, "unused-core")
	files, err := s.buildCredentialPublication(t.Context(), config, []domain.CredentialPublication{{Runtime: normalScope, Revision: 2, Published: 1}, {Runtime: vpngateScope, Revision: 1, Published: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || len(files["sb.json"]) == 0 {
		t.Fatal("unrelated configurations were rebuilt", len(files))
	}
	if err = publishPlatformFiles(root, files); err != nil {
		t.Fatal(err)
	}
}

func TestCredentialPublicationRefreshesDependentAccountSources(t *testing.T) {
	root, core, _ := appFixture(t)
	s, config, _ := platformFixture(t, root, core)
	var node ManagedNode
	for _, entry := range config.App.Nodes {
		if entry.Protocol == "vless" {
			node = entry
			break
		}
	}
	if err := s.Accounts.SaveAccount(t.Context(), domain.Account{ID: "a", Name: "A"}); err != nil {
		t.Fatal(err)
	}
	credential := domain.Credential{ID: "cred", AccountID: "a", RuntimeID: normalScope, InboundID: node.ID, Protocol: "vless", UUID: randomUUID(), Generation: 1}
	if err := s.Accounts.BindBatch(t.Context(), []domain.Credential{credential}); err != nil {
		t.Fatal(err)
	}
	config.Subscriptions = sub.DefaultConfig()
	config.Subscriptions.Sources = []sub.Source{{ID: "account", Kind: "account", AccountID: "a", Scope: normalScope, Required: true}}
	config.Subscriptions.Collections = []sub.Collection{{ID: "account", Sources: []string{"account"}}}
	config.Subscriptions.Outputs = []sub.Output{{ID: "account", Collection: "account", Template: "normal-uri"}}
	files, err := s.buildCredentialPublication(t.Context(), config, []domain.CredentialPublication{{Runtime: normalScope, Revision: 1}})
	if err != nil {
		t.Fatal(err)
	}
	var snapshot sub.Snapshot
	if err := json.Unmarshal(files[subscriptionsSnapshot], &snapshot); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(snapshot.Artifacts["account"].Data, []byte(credential.UUID)) {
		t.Fatal("account subscription omitted the published credential")
	}
	if err := publishPlatformFiles(root, files); err != nil {
		t.Fatal(err)
	}
	// The same normal-only source is unrelated to a VPNGate credential change.
	files, err = s.buildCredentialPublication(t.Context(), config, []domain.CredentialPublication{{Runtime: vpngateScope, Revision: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, rebuilt := files[subscriptionsConfig]; rebuilt {
		t.Fatal("unrelated subscription was rebuilt")
	}
}
