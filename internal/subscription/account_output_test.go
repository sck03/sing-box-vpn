package subscription

import (
	"encoding/json"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/clientprofile"
)

func TestAccountOutputProfilesKeepPurpose(t *testing.T) {
	c := DefaultConfig()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	nodes := []Node{{ID: "one", Name: "One", Protocol: "trojan", Server: "example.test", Port: 443, Password: "a-password", TLS: TLS{Enabled: true, ServerName: "example.test"}}}
	for _, profile := range clientprofile.Outputs() {
		t.Run(profile.ID, func(t *testing.T) {
			option, plan, err := c.PlanAccountOutput(nodes, EffectiveTemplates(c), "account", profile.ID)
			if err != nil {
				t.Fatal(err)
			}
			if option.Profile != profile.ID || option.Mode != profile.Mode || option.Nodes != 1 {
				t.Fatal(option)
			}
			artifact, _, err := plan.Render()
			if err != nil {
				t.Fatal(err)
			}
			if profile.Mode != "config" && (profile.Client == "mihomo" || profile.Client == "openclash" || profile.Client == "sing-box" || profile.Client == "stash" || profile.Client == "egern") {
				var value map[string]json.RawMessage
				if err := json.Unmarshal(artifact.Data, &value); err != nil {
					t.Fatal(err)
				}
				if len(value) != 1 {
					t.Fatalf("node output leaked device policies: %s", artifact.Data)
				}
				key := "proxies"
				if profile.Client == "sing-box" {
					key = "outbounds"
				}
				if _, ok := value[key]; !ok {
					t.Fatalf("missing %s", key)
				}
			}
		})
	}
	c.Templates = append(c.Templates, Template{ID: "complete", Client: "mihomo", Mode: "config", Version: 1})
	c.AccountDefaults = map[string]AccountExportPolicy{"mihomo-provider": {Template: "complete"}}
	if err := c.Validate(); err == nil {
		t.Fatal("template changed an authorized output purpose")
	}
	if _, _, err := c.AccountTemplate("account", "mihomo-provider", EffectiveTemplates(c)); err == nil {
		t.Fatal("runtime accepted a mismatched template")
	}
	delete(c.AccountDefaults, "mihomo-provider")
	template, _, err := c.AccountTemplate("account", "mihomo-provider", EffectiveTemplates(c))
	if err != nil || template.Mode != "provider" {
		t.Fatal("reset did not restore built-in", template, err)
	}
	if _, _, err := c.AccountTemplate("account", "mihomo", EffectiveTemplates(c)); err == nil {
		t.Fatal("old ambiguous client ID accepted")
	}
}

func TestNodePresetHasNoDevicePolicies(t *testing.T) {
	for _, preset := range Presets() {
		if preset.ID != "nodes" {
			continue
		}
		for client, template := range preset.Templates {
			if template.Mode == "config" || template.TUN || len(template.Groups) > 0 || len(template.DNS) > 0 || template.DNSPolicy != nil {
				t.Fatal(client, template)
			}
		}
		return
	}
	t.Fatal("missing nodes preset")
}
