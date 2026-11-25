package manage

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

func TestPublicationFieldsRejectedBeforePreviewOrSave(t *testing.T) {
	root, core, _ := appFixture(t)
	service, config, session := platformFixture(t, root, core)
	handler := service.Handler(PanelOptions{PublicURL: "http://example.com"})
	for _, tc := range []struct {
		name, field string
		edit        func(*PlatformConfig)
	}{
		{"CDN URL", "app.cdn_address", func(c *PlatformConfig) { c.App.CDNAddress = "https://cdn.example.test/path" }},
		{"CDN port", "app.cdn_address", func(c *PlatformConfig) { c.App.CDNAddress = "cdn.example.test:443" }},
		{"CDN control", "app.cdn_address", func(c *PlatformConfig) { c.App.CDNAddress = "bad\x00host" }},
		{"SNI control", "tls.server_name", func(c *PlatformConfig) {
			for i := range c.App.Nodes {
				if c.App.Nodes[i].Protocol == "vmess" {
					c.App.Nodes[i].TLS.ServerName = "bad\nname"
				}
			}
		}},
		{"SNI URL", "tls.server_name", func(c *PlatformConfig) { c.App.Nodes[0].TLS.ServerName = "https://example.com" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate, err := decodePlatform(mustJSON(t, config))
			if err != nil {
				t.Fatal(err)
			}
			tc.edit(&candidate)
			for _, path := range []string{"/api/config/preview", "/api/config"} {
				body := mustJSON(t, candidate)
				if path == "/api/config" {
					body = mustJSON(t, ConfigurationSave{Revision: 1, Config: candidate})
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, panelRequest("POST", path, body, session))
				var result struct{ Error APIError }
				if err = json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if response.Code != 400 || result.Error.Code != "invalid_field" || !strings.Contains(result.Error.Field, tc.field) {
					t.Fatalf("%s: %d %s", path, response.Code, response.Body.String())
				}
			}
		})
	}
	state, err := service.Store.State(t.Context())
	if err != nil || state.Revision != 1 || state.AppliedRevision != 1 {
		t.Fatal(state.Revision, err)
	}
}

func TestPublicationValidCDNPreviewApplyAndAccountExport(t *testing.T) {
	root, core, _ := appFixture(t)
	service, config, _ := platformFixture(t, root, core)
	ctx := t.Context()
	if err := service.Accounts.SaveAccount(ctx, domain.Account{ID: "cdn-account", Name: "CDN account"}); err != nil {
		t.Fatal(err)
	}
	for _, node := range config.App.Nodes {
		if node.Protocol == "vmess" {
			credential := fixtureCredential("vmess")
			credential.ID = "cdn-credential"
			credential.AccountID = "cdn-account"
			credential.RuntimeID = normalScope
			credential.InboundID = node.ID
			if err := service.Accounts.BindBatch(ctx, []domain.Credential{credential}); err != nil {
				t.Fatal(err)
			}
		}
	}
	config.App.CDNAddress = "cdn.example.test"
	preview, err := service.PreviewConfiguration(ctx, config)
	if err != nil || !preview.Valid {
		t.Fatal(preview, err)
	}
	if len(preview.Deployment.Changes) != 0 || preview.Deployment.RuntimeObserved {
		t.Fatal("publication-only edit planned restarts", preview.Deployment)
	}
	id, err := service.QueueApply(ctx, config, 1, "admin")
	if err != nil {
		t.Fatal(err)
	}
	task, err := service.Store.ClaimLane(ctx, "control")
	if err != nil || task.ID != id {
		t.Fatal(task, err)
	}
	if err = service.runTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	task, err = service.Store.Task(ctx, id)
	if err != nil || task.State != "succeeded" {
		t.Fatal(task, err)
	}
	usage, err := service.Accounts.DownloadAccount(ctx, "cdn-account", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := accountNodes(ctx, service.Accounts, config, usage, "normal", 0)
	if err != nil || len(nodes) != 1 || nodes[0].Server != "cdn.example.test" {
		t.Fatal(nodes, err)
	}
	config.App.CDNAddress = "https://bad.example/path"
	if _, err = accountNodes(ctx, service.Accounts, config, usage, "normal", 0); err == nil {
		t.Fatal("transformed node escaped final validation")
	}
}
