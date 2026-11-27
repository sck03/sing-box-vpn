package manage

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/domain"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestCandidatePreparationHTTPRequiresCurrentConfig(t *testing.T) {
	service := &PlatformService{Root: t.TempDir()}
	access := panelAccess{admin: true, require: func(allowed bool) bool { return allowed }}
	for _, tc := range []struct {
		name   string
		body   any
		status int
	}{
		{"current", InboundPreparation{ID: "entry", Protocol: "vless", Config: candidateFixture()}, http.StatusOK},
		{"missing candidate", Object{"id": "entry", "protocol": "vless"}, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.body)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest("POST", "/api/preset", bytes.NewReader(raw))
			response := httptest.NewRecorder()
			if !service.serveConfiguration(response, request, access) || response.Code != tc.status {
				t.Fatalf("status %d: %s", response.Code, response.Body.String())
			}
		})
	}
}

func candidateFixture() PlatformConfig {
	return PlatformConfig{Schema: PlatformSchema, App: App{Name: "Test", Address: "example.com"},
		VPN:           VPNSettings{ProxyPort: 7928, Profiles: map[string]Profile{}},
		Subscriptions: sub.DefaultConfig(), Argo: TunnelSettings{Mode: "off"}}
}

func TestCandidatePresetReservesDraftPorts(t *testing.T) {
	root := t.TempDir()
	c := candidateFixture()
	node, err := prepareInbound(root, c, "vless", "new-entry")
	if err != nil {
		t.Fatal(err)
	}
	c.App.Nodes = append(c.App.Nodes, node)
	if _, err = prepareInbound(root, c, "vless", node.ID); err == nil {
		t.Fatal("duplicate draft ID accepted")
	}
	// Occupy the complete allocator range in a new, unsaved UDP reservation.
	cert, key := filepath.Join(root, "cert.pem"), filepath.Join(root, "key.pem")
	if err = EnsureCertificate(cert, key, "example.com"); err != nil {
		t.Fatal(err)
	}
	c.App.Nodes = []ManagedNode{{ID: "udp", Protocol: "tuic", Port: 60000,
		UDPPorts: []string{"20000:59999"}, TLS: ManagedTLS{Enabled: true, ServerName: "example.com", Certificate: cert, Key: key}}}
	if err = c.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err = prepareInbound(root, c, "vless", "new-entry"); err == nil {
		t.Fatal("draft UDP reservations ignored")
	}
	if _, err = prepareCountry(root, c, "JP"); err == nil {
		t.Fatal("country allocation ignored draft reservations")
	}
	if _, err = prepareInbound(root, PlatformConfig{}, "vless", "new-entry"); err == nil {
		t.Fatal("missing candidate accepted")
	}
	// The candidate listener, not a stale subscription.json, reserves its port.
	if err = AtomicJSON(filepath.Join(root, "subscription.json"), subscriptionSettings{Enabled: true, Port: 29999}); err != nil {
		t.Fatal(err)
	}
	used, _, err := allocationPorts(root, App{}, VPNSettings{}, subscriptionSettings{Enabled: true, Port: 30000})
	if err != nil || !used[30000] || used[29999] {
		t.Fatal(used, err)
	}
}

func TestCandidateRejectsOrphanedInboundBeforeCompile(t *testing.T) {
	store, err := accounting.Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := t.Context()
	if err = store.SaveAccount(ctx, domain.Account{ID: "account", Name: "Account"}); err != nil {
		t.Fatal(err)
	}
	if err = store.BindBatch(ctx, []domain.Credential{{ID: "credential", AccountID: "account", RuntimeID: "normal", InboundID: "entry", Protocol: "vless", Generation: 1, UUID: "aaaaaaaa-1111-4111-8111-111111111111"}}); err != nil {
		t.Fatal(err)
	}
	service := &PlatformService{Accounts: store}
	if _, _, err = service.prepareBuild(ctx, candidateFixture()); err == nil {
		t.Fatal("orphaned binding reached compiler")
	}
	c := candidateFixture()
	c.App.Nodes = []ManagedNode{{ID: "entry", Protocol: "vless", Disabled: true}}
	if err = service.validateInboundBindings(ctx, c); err != nil {
		t.Fatal("disabled inbound lost membership", err)
	}
}
