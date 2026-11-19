package main

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/domain"
	"github.com/sck03/sing-box-vpn/internal/manage"
	managed "github.com/sck03/sing-box-vpn/internal/runtime"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func boundProtocolServer(t *testing.T, scope string, server manage.Object) manage.Object {
	t.Helper()
	var credentials []domain.Credential
	for _, entry := range server["inbounds"].([]manage.Object) {
		kind, tag := entry["type"].(string), entry["tag"].(string)
		id, password := protocolSecrets(scope, kind)
		credentials = append(credentials, domain.Credential{ID: "fixture-" + kind, AccountID: "fixture", RuntimeID: scope, InboundID: tag, Protocol: kind, Generation: 1, UUID: id, Password: password})
	}
	raw, err := json.Marshal(server)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = managed.BindUsers(raw, credentials)
	if err != nil {
		t.Fatal(err)
	}
	var result manage.Object
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

// The protocol matrix obtains client material through the current account
// exporter. It does not depend on a removed shared-credential configuration API.
func publishProtocolFixture(t *testing.T, root, core string, app *manage.App) {
	t.Helper()
	var err error
	app.Version, err = manage.CheckCore(t.Context(), core)
	if err != nil {
		t.Fatal(err)
	}
	settings := manage.VPNSettings{ProxyPort: 7928, Profiles: map[string]manage.Profile{}}
	secrets := map[string]manage.Secrets{}
	if _, err = manage.ReadJSON(filepath.Join(root, "vpngate", "settings.json"), &settings); err != nil {
		t.Fatal(err)
	}
	if _, err = manage.ReadJSON(filepath.Join(root, "vpngate", "secrets.json"), &secrets); err != nil {
		t.Fatal(err)
	}
	config := manage.PlatformConfig{Schema: manage.PlatformSchema, App: *app, VPN: settings, VPNSecrets: secrets}
	store, err := accounting.Open(filepath.Join(root, "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.SaveAccount(t.Context(), domain.Account{ID: "export-fixture", Name: "Protocol"}); err != nil {
		t.Fatal(err)
	}
	service := manage.AccountService{Root: root, Core: core, Store: store, Config: &config}
	snapshot := sub.Snapshot{Version: sub.SchemaVersion, Revision: 1, Artifacts: map[string]sub.Artifact{}}
	subscriptions := sub.DefaultConfig()
	for _, scope := range []string{"normal", "vpngate"} {
		entries := app.Inbounds()
		if scope == "vpngate" {
			if settings.ActiveCountry == "" {
				continue
			}
			profile := settings.Profiles[settings.ActiveCountry]
			profile.Secrets = secrets[settings.ActiveCountry]
			entries = manage.MakeInbounds(profile, "vg-"+settings.ActiveCountry+"-")
		}
		credentials := []domain.Credential{}
		for _, entry := range entries {
			kind := entry["type"].(string)
			credential := domain.Credential{ID: "fixture-" + scope + "-" + kind, AccountID: "export-fixture", RuntimeID: scope, InboundID: entry["tag"].(string), Protocol: kind, Generation: 1}
			credential.UUID, credential.Password = protocolSecrets(scope, kind)
			if err = store.Bind(t.Context(), credential); err != nil {
				t.Fatal(err)
			}
			credentials = append(credentials, credential)
		}
		subscriptions.Sources = append(subscriptions.Sources, sub.Source{ID: scope, Kind: "inline", Format: "nodes", Content: "[]"})
		subscriptions.Collections = append(subscriptions.Collections, sub.Collection{ID: scope, Sources: []string{scope}})
		for _, target := range []struct{ client, suffix, file string }{{"sing-box", "sing-box", "sbox.json"}, {"mihomo", "mihomo", "clmi.yaml"}, {"openclash", "provider", "proxies.yaml"}, {"uri", "uri", "jhsub.txt"}, {"base64", "base64", "jhsub-base64.txt"}} {
			artifact, err := service.Export(t.Context(), application.AccountCommand{ID: "export-fixture", Scope: scope, Client: target.client})
			if err != nil {
				t.Fatal(err)
			}
			id := scope + "-" + target.suffix
			snapshot.Artifacts[id] = artifact
			subscriptions.Outputs = append(subscriptions.Outputs, sub.Output{ID: id, Collection: scope, Template: id})
			directory := root
			if scope == "vpngate" {
				directory = filepath.Join(root, "vpngate")
			}
			if err = manage.AtomicBytes(filepath.Join(directory, target.file), artifact.Data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		for _, credential := range credentials {
			if err = store.Unbind(t.Context(), credential.ID, credential.Generation); err != nil {
				t.Fatal(err)
			}
		}
	}
	for name, value := range map[string]any{"app.json": app, "subscriptions/config.json": subscriptions, "subscriptions/published.json": snapshot} {
		if err = manage.AtomicJSON(filepath.Join(root, name), value); err != nil {
			t.Fatal(err)
		}
	}
}

func protocolEntry(nodes []manage.Object, kind string) manage.Object {
	for _, node := range nodes {
		if node["type"] == kind {
			return node
		}
	}
	return nil
}

func protocolSecrets(scope, kind string) (string, string) {
	if scope == "vpngate" {
		if kind == "tuic" {
			return "23456789-abcd-4ef0-8123-456789abcdef", "independent TUIC @:/?# password"
		}
		return "12345678-9abc-4def-8012-3456789abcde", "12345678-9abc-4def-8012-3456789abcde"
	}
	return "01234567-89ab-4cde-8f01-23456789abcd", "01234567-89ab-4cde-8f01-23456789abcd"
}
