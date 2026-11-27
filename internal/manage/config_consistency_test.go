package manage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
	"github.com/sck03/sing-box-vpn/internal/domain"
	"github.com/sck03/sing-box-vpn/internal/hostservice"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestTunnelCandidateApplyAndDownloadAgree(t *testing.T) {
	root, core, _ := appFixture(t)
	service, config, _ := platformFixture(t, root, core)
	ctx := t.Context()
	if err := service.Accounts.SaveAccount(ctx, domain.Account{ID: "tunnel", Name: "Tunnel"}); err != nil {
		t.Fatal(err)
	}
	inbound := ""
	for _, node := range config.App.Nodes {
		if node.Protocol == "vmess" {
			inbound = node.ID
		}
	}
	if err := service.accountService().Execute(ctx, application.AccountCommand{Operation: "bind", ID: "tunnel", Scope: normalScope, InboundIDs: []string{inbound}}, new(bytes.Buffer)); err != nil {
		t.Fatal(err)
	}
	config.Subscriptions = sub.DefaultConfig()
	config.Subscriptions.Sources = []sub.Source{{ID: "account", Kind: "account", AccountID: "tunnel", Scope: normalScope, Required: true}}
	config.Subscriptions.Collections = []sub.Collection{{ID: "account", Sources: []string{"account"}}}
	config.Subscriptions.Outputs = []sub.Output{{ID: "account", Collection: "account", Template: "normal-uri"}}
	revision := int64(1)
	for _, domain := range []string{"first.example.test", "second.example.test", ""} {
		config.Argo = TunnelSettings{Mode: "fixed", Domain: domain, Token: strings.Repeat("a", 32), Inbound: inbound}
		expected := 2
		if domain == "" {
			config.Argo = TunnelSettings{Mode: "off"}
			expected = 1
		}
		preview, err := service.PreviewOutput(ctx, config, "account")
		if err != nil {
			t.Fatal(err)
		}
		if preview.Nodes != expected {
			t.Fatalf("candidate %s: %v", domain, preview)
		}
		whole, err := service.PreviewConfiguration(ctx, config)
		if err != nil {
			t.Fatal(err)
		}
		if whole.Outputs[0].Nodes != expected {
			t.Fatal("whole/output previews disagree")
		}
		if err := applyFixture(t, service, config, revision); err != nil {
			t.Fatal(err)
		}
		revision++
		raw, err := service.Store.AppliedConfig(ctx)
		if err != nil {
			t.Fatal(err)
		}
		applied, err := decodePlatform(raw)
		if err != nil {
			t.Fatal(err)
		}
		after, err := service.PreviewOutput(ctx, applied, "account")
		if err != nil {
			t.Fatal(err)
		}
		if after.Preview != preview.Preview {
			t.Fatal("preview differs after apply", preview, after)
		}
		nodes, err := service.accountService().accountNodes(ctx, "tunnel", normalScope, nil)
		if err != nil || len(nodes) != expected {
			t.Fatal("download differs", len(nodes), err)
		}
		if expected == 2 && nodes[1].Server != domain {
			t.Fatal("stale tunnel domain", nodes[1].Server)
		}
		if bytes.Contains(raw, []byte("\"argo\"")) {
			t.Fatal("derived tunnel was persisted")
		}
	}
}

func TestTunnelObservationBelongsToCandidate(t *testing.T) {
	store, err := controlstore.Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	c := PlatformConfig{Argo: TunnelSettings{Mode: "temporary", Inbound: "vmess"}}
	observation := tunnelObservation{Settings: c.Argo, Runtime: hostservice.Tunnel{Domain: "live.trycloudflare.com"}}
	if err = store.Observe(t.Context(), "argo-tunnel", string(mustJSON(t, observation))); err != nil {
		t.Fatal(err)
	}
	got, err := observedConfig(t.Context(), store, c)
	if err != nil || got.tunnelDomain != observation.Runtime.Domain {
		t.Fatal(got.tunnelDomain, err)
	}
	if err = store.Observe(t.Context(), "argo-tunnel", "invalid observation"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := (AccountService{Control: store, Config: &got}).configSnapshot(t.Context())
	if err != nil || snapshot.tunnelDomain != observation.Runtime.Domain {
		t.Fatal("explicit snapshot reread live observation", err)
	}
	if err = store.Observe(t.Context(), "argo-tunnel", string(mustJSON(t, observation))); err != nil {
		t.Fatal(err)
	}
	c.Argo.Inbound = "other"
	got, err = observedConfig(t.Context(), store, c)
	if err != nil || got.tunnelDomain != "" {
		t.Fatal("reused foreign observation", err)
	}
	c.Argo = TunnelSettings{Mode: "fixed", Domain: "fixed.example.test"}
	got, err = observedConfig(t.Context(), store, c)
	if err != nil || got.tunnelDomain != c.Argo.Domain {
		t.Fatal(err)
	}
	got.Argo.Mode = "off"
	got, err = observedConfig(t.Context(), store, got)
	if err != nil || got.tunnelDomain != "" {
		t.Fatal("stale derived state", err)
	}
}

func TestBindingCacheTracksOnlyConfirmedConfiguration(t *testing.T) {
	store, err := controlstore.Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	config := PlatformConfig{Schema: PlatformSchema, App: App{Nodes: []ManagedNode{{ID: "node", Protocol: "vmess", Port: 443}}}}
	if err = store.Initialize(t.Context(), mustJSON(t, config)); err != nil {
		t.Fatal(err)
	}
	service := AccountService{Control: store, bindings: &accountBindingCache{}}
	first, err := service.availableBindings(t.Context())
	if err != nil || len(first) != 1 {
		t.Fatal(first, err)
	}
	unchanged, err := store.AppliedStateSince(t.Context(), 1)
	if err != nil || len(unchanged.Config) != 0 {
		t.Fatal("unchanged config copied", err)
	}
	config.App.Nodes[0].Disabled = true
	task, err := store.Queue(t.Context(), "apply", domain.TaskSubmission{Actor: "test", Source: domain.TaskUser}, []byte("{}"), 1, mustJSON(t, config))
	if err != nil {
		t.Fatal(err)
	}
	pending, err := service.availableBindings(t.Context())
	if err != nil || len(pending) != 1 {
		t.Fatal("pending config leaked", err)
	}
	if err = store.Applied(t.Context(), 2); err != nil {
		t.Fatal(err)
	}
	if err = store.Finish(t.Context(), task, nil); err != nil {
		t.Fatal(err)
	}
	next, err := service.availableBindings(t.Context())
	if err != nil || len(next) != 0 || len(first) != 1 {
		t.Fatal("cache stale or previous snapshot mutated", err)
	}
}

func TestRemovedFieldsRejected(t *testing.T) {
	for _, field := range []string{"argo", "socks_port", "routing", "country"} {
		raw, _ := json.Marshal(map[string]any{"schema": AppSchema, "nodes": []any{}, field: map[string]any{}})
		var app App
		if err := json.Unmarshal(raw, &app); err == nil {
			t.Fatal("removed field accepted", field)
		}
	}
}

func BenchmarkBindingAvailability(b *testing.B) {
	store, err := controlstore.Open(filepath.Join(b.TempDir(), "control.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	config := PlatformConfig{Schema: PlatformSchema, App: App{Nodes: make([]ManagedNode, 256)}, Subscriptions: sub.DefaultConfig()}
	for i := range config.App.Nodes {
		config.App.Nodes[i] = ManagedNode{ID: string(rune(i + 1000)), Protocol: "vmess", Port: 10000 + i}
	}
	data, err := json.Marshal(config)
	if err != nil {
		b.Fatal(err)
	}
	if err = store.Initialize(context.Background(), data); err != nil {
		b.Fatal(err)
	}
	for _, cached := range []bool{false, true} {
		name := "decode-every-request"
		s := AccountService{Control: store}
		if cached {
			name = "confirmed-revision-cache"
			s.bindings = &accountBindingCache{}
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := s.availableBindings(b.Context()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestCandidatePortConflictsDoNotPublish(t *testing.T) {
	root, core, _ := appFixture(t)
	service, base, _ := platformFixture(t, root, core)
	before := readBytes(t, filepath.Join(root, subscriptionsSnapshot))
	for _, kind := range []string{"inbounds", "gateway", "download", "forwarding"} {
		t.Run(kind, func(t *testing.T) {
			config, err := decodePlatform(mustJSON(t, base))
			if err != nil {
				t.Fatal(err)
			}
			port := config.App.Nodes[0].Port
			switch kind {
			case "inbounds":
				config.App.Nodes[1].Port = port
			case "gateway":
				config.VPN.ProxyPort = port
			case "download":
				config.Listener.Enabled = true
				config.Listener.Port = port
			case "forwarding":
				setFixtureUDPPorts(&config.App, "tuic", []string{fmt.Sprint(port)})
			}
			if _, err := service.QueueApply(t.Context(), config, 1, domain.TaskSubmission{Actor: "test", Source: domain.TaskUser}); err == nil {
				t.Fatal("accepted conflicting ports")
			}
			revision, err := service.Store.Revisions(t.Context())
			if err != nil || revision.Revision != 1 {
				t.Fatal("invalid candidate persisted", err)
			}
			if !bytes.Equal(before, readBytes(t, filepath.Join(root, subscriptionsSnapshot))) {
				t.Fatal("invalid candidate published")
			}
		})
	}
}

func TestStructuredResolversValidateWithBothCores(t *testing.T) {
	root, core, _ := appFixture(t)
	service, config, _ := platformFixture(t, root, core)
	resolver := Object{"server": "dns-local", "strategy": "prefer_ipv4"}
	config.App.Advanced.Route = Object{"final": "direct", "rules": []Object{}, "default_domain_resolver": resolver}
	config.App.Advanced.DNS = Object{"servers": []Object{{"type": "local", "tag": "dns-local"}, {"type": "https", "tag": "dns-remote", "server": "dns.example.test", "domain_resolver": resolver}}, "final": "dns-local"}
	preview, err := service.PreviewConfiguration(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := object(preview.OrdinaryServer["route"])["default_domain_resolver"].(Object); !ok {
		t.Fatal("resolver lost its object shape")
	}
}
