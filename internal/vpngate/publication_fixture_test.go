package vpngate

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"testing"
	"time"

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
	if _, err = manage.CheckCore(t.Context(), core); err != nil {
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
	if country := settings.ActiveCountry; country != "" {
		secret := secrets[country]
		config.Gateway = manage.Object{"country": country, "listen": fmt.Sprintf("127.0.0.1:%d", settings.ProxyPort), "proxy_user": secret.SocksUser, "proxy_pass": secret.SocksPass}
	}
	platform, err := manage.OpenPlatform(root, core, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer platform.Close()
	store := platform.Accounts
	if err = store.SaveAccount(t.Context(), domain.Account{ID: "export-fixture", Name: "Protocol"}); err != nil {
		t.Fatal(err)
	}
	service := manage.AccountService{Root: root, Core: core, Store: store, Config: &config}
	subscriptions := sub.DefaultConfig()
	var credentials []domain.Credential
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
		for _, entry := range entries {
			kind := entry["type"].(string)
			credential := domain.Credential{ID: "fixture-" + scope + "-" + kind, AccountID: "export-fixture", RuntimeID: scope, InboundID: entry["tag"].(string), Protocol: kind, Generation: 1}
			credential.UUID, credential.Password = protocolSecrets(scope, kind)
			if err = store.BindBatch(t.Context(), []domain.Credential{credential}); err != nil {
				t.Fatal(err)
			}
			credentials = append(credentials, credential)
		}
		subscriptions.Sources = append(subscriptions.Sources, sub.Source{ID: scope, Kind: "account", AccountID: "export-fixture", Scope: scope, Required: true})
		subscriptions.Collections = append(subscriptions.Collections, sub.Collection{ID: scope, Sources: []string{scope}})
		for _, target := range []struct{ profile, suffix, file string }{{"sing-box-config", "sing-box", "sbox.json"}, {"mihomo-config", "mihomo", "clmi.yaml"}, {"openclash-provider", "provider", "proxies.yaml"}, {"uri", "uri", "jhsub.txt"}, {"base64", "base64", "jhsub-base64.txt"}} {
			result, err := service.ExportDetailed(t.Context(), application.AccountExportRequest{ID: "export-fixture", Scope: scope, Profile: target.profile})
			if err != nil {
				t.Fatal(err)
			}
			id := scope + "-" + target.suffix
			artifact := result.Artifact
			subscriptions.Outputs = append(subscriptions.Outputs, sub.Output{ID: id, Collection: scope, Template: id})
			directory := root
			if scope == "vpngate" {
				directory = filepath.Join(root, "vpngate")
			}
			if err = manage.AtomicBytes(filepath.Join(directory, target.file), artifact.Data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	config.Subscriptions = subscriptions
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err = platform.Store.Initialize(t.Context(), data); err != nil {
		t.Fatal(err)
	}
	// Rebuild through the production recovery/compiler path. It owns the
	// confirmed generation, contributor records and all publication projections.
	if err = manage.RunPanel(t.Context(), []string{"--root", root, "--core", core, "--no-system", "recover"}, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	var snapshot sub.Snapshot
	if _, err = manage.ReadJSON(filepath.Join(root, "subscriptions", "published.json"), &snapshot); err != nil {
		t.Fatal(err)
	}
	for _, output := range subscriptions.Outputs {
		if err := sub.PolicyForOutput(subscriptions, snapshot, output.ID).Validate(time.Now()); err != nil {
			t.Fatalf("fixture publication %s: %v; diagnostics: %v", output.ID, err, snapshot.Diagnostics[output.ID])
		}
	}
	// Runtime isolation tests bind the same fixture identities to their own
	// accounts after capturing the published client material.
	for _, credential := range credentials {
		if err = store.Unbind(t.Context(), credential.ID, credential.Generation); err != nil {
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
