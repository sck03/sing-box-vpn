package manage

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/servicecatalog"
)

func deploymentFixture() PlatformConfig {
	p := Profile{Ports: map[string]int{"vless": 2443, "vmess": 2080, "hysteria2": 2444, "tuic": 2445, "anytls": 2446}}
	return PlatformConfig{
		App:        App{Nodes: []ManagedNode{{ID: "vmess", Protocol: "vmess", Port: 8080, Transport: ManagedTransport{Type: "ws", Path: "/ws"}}}},
		Argo:       TunnelSettings{Mode: "temporary", Inbound: "vmess"},
		VPN:        VPNSettings{ActiveCountry: "JP", ProxyPort: 7928, Profiles: map[string]Profile{"JP": p, "US": p}},
		VPNSecrets: map[string]Secrets{"JP": {SocksUser: "jp", SocksPass: strings.Repeat("a", 32)}, "US": {SocksUser: "us", SocksPass: strings.Repeat("b", 32)}},
		Gateway:    Object{"enabled": true, "country": "JP", "health_interval": 60},
	}
}

func TestDeploymentPlanEffectiveDependencies(t *testing.T) {
	old := deploymentFixture()
	for _, tc := range []struct {
		name string
		edit func(*PlatformConfig)
		want []string
	}{
		{"core logging", func(c *PlatformConfig) { c.App.Advanced.Log = Object{"level": "debug"} }, []string{servicecatalog.Core}},
		{"unrelated inbound", func(c *PlatformConfig) {
			c.App.Nodes = append(c.App.Nodes, ManagedNode{ID: "other", Protocol: "vmess", Port: 8081, Transport: ManagedTransport{Type: "ws", Path: "/other"}})
		}, []string{servicecatalog.Core}},
		{"published address", func(c *PlatformConfig) { c.App.CDNAddress = "cdn.example.test" }, nil},
		{"inactive country", func(c *PlatformConfig) { p := c.VPN.Profiles["US"]; p.Ports["vmess"]++; c.VPN.Profiles["US"] = p }, nil},
		{"inactive credentials", func(c *PlatformConfig) {
			secret := c.VPNSecrets["US"]
			secret.SocksPass = strings.Repeat("c", 32)
			c.VPNSecrets["US"] = secret
		}, nil},
		{"gateway policy", func(c *PlatformConfig) { c.Gateway["health_interval"] = 120 }, []string{servicecatalog.Gateway}},
		{"active country", func(c *PlatformConfig) { p := c.VPN.Profiles["JP"]; p.Ports["vmess"]++; c.VPN.Profiles["JP"] = p }, []string{servicecatalog.VPNCore}},
		{"tunnel port", func(c *PlatformConfig) { c.App.Nodes[0].Port++ }, []string{servicecatalog.Core, servicecatalog.Argo}},
		{"tunnel websocket path", func(c *PlatformConfig) { c.App.Nodes[0].Transport.Path = "/new" }, []string{servicecatalog.Core}},
		{"tunnel TLS", func(c *PlatformConfig) {
			c.App.Nodes[0].TLS = ManagedTLS{Enabled: true, Certificate: "cert.pem", Key: "key.pem"}
		}, []string{servicecatalog.Core, servicecatalog.Argo}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(old)
			if err != nil {
				t.Fatal(err)
			}
			var candidate PlatformConfig
			if err = json.Unmarshal(raw, &candidate); err != nil {
				t.Fatal(err)
			}
			tc.edit(&candidate)
			plan, err := deploymentPlan(candidate, old, servicecatalog.Names())
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, change := range plan {
				names = append(names, change.Name)
				if change.Reason == "" {
					t.Fatal("missing reason")
				}
			}
			if !slices.Equal(names, tc.want) {
				t.Fatalf("got %+v, want %v", plan, tc.want)
			}
		})
	}
}

func TestFixedTunnelUsesTokenAndPropagatesInvalidInput(t *testing.T) {
	old := deploymentFixture()
	old.Argo = TunnelSettings{Mode: "fixed", Token: strings.Repeat("a", 24), Domain: "old.example", Inbound: "vmess"}
	candidate := old
	candidate.Argo.Domain = "new.example"
	plan, err := deploymentPlan(candidate, old, servicecatalog.Names())
	if err != nil || len(plan) != 0 {
		t.Fatal(plan, err)
	}
	candidate.Argo.Token = strings.Repeat("b", 24)
	plan, err = deploymentPlan(candidate, old, servicecatalog.Names())
	if err != nil || len(plan) != 1 || plan[0].Name != servicecatalog.Argo {
		t.Fatal(plan, err)
	}
	candidate.App.Nodes = []ManagedNode{{ID: "bad", Protocol: "vmess", Port: 0}}
	if _, err = deploymentPlan(candidate, old, servicecatalog.Names()); err == nil {
		t.Fatal("invalid input silently ignored")
	}
}
