// Package clientprofile describes output formats without depending on storage,
// HTTP, or renderers. Forms, download grants and subscription adapters share it.
package clientprofile

import "slices"

type Features struct {
	TUN            bool `json:"tun"`
	DNS            bool `json:"dns"`
	Probe          bool `json:"probe"`
	PortHopping    bool `json:"port_hopping"`
	CertificatePin bool `json:"certificate_pin"`
	PublicKeyPin   bool `json:"public_key_pin"`
	UTLS           bool `json:"utls"`
	GRPC           bool `json:"grpc"`
	HTTPUpgrade    bool `json:"http_upgrade"`
	HysteriaObfs   bool `json:"hysteria_obfs"`
	PolicyGroups   bool `json:"policy_groups"`
	AdvancedDNS    bool `json:"advanced_dns"`
	RuleProviders  bool `json:"rule_providers"`
	NativeTemplate bool `json:"native_template"`
}

type Profile struct {
	Label         string   `json:"label"`
	Protocols     []string `json:"protocols"`
	Modes         []string `json:"modes"`
	AccountMode   string   `json:"account_mode"`
	Verification  string   `json:"verification"`
	TestedVersion string   `json:"tested_version,omitempty"`
	Experimental  bool     `json:"experimental"`
	Features      Features `json:"features"`
}

type Connection struct {
	Transports    []string `json:"transports"`
	Ciphers       []string `json:"ciphers,omitempty"`
	Plugins       []string `json:"plugins,omitempty"`
	GRPCModes     []string `json:"grpc_modes,omitempty"`
	GRPCAuthority bool     `json:"grpc_authority"`
	EarlyData     bool     `json:"early_data"`
	XHTTPExtra    bool     `json:"xhttp_extra"`
	Reality       bool     `json:"reality"`
	UTLS          bool     `json:"utls"`
}

type Client struct {
	Profile
	Extension   string                `json:"extension"`
	Platforms   []string              `json:"platforms"`
	Connections map[string]Connection `json:"connections"`
}

var protocols = []string{"vless", "vmess", "hysteria2", "tuic", "anytls", "trojan", "ss"}

// Experimental adapters describe this project's encoders, not a claim that
// every release of the named application supports every declared combination.
var catalog = map[string]Profile{
	"sing-box":     {"sing-box", protocols, []string{"nodes", "config"}, "config", "core-check", "1.14.4", false, Features{TUN: true, DNS: true, PortHopping: true, PublicKeyPin: true, UTLS: true}},
	"mihomo":       {"Mihomo", protocols, []string{"nodes", "provider", "config"}, "config", "core-check", "1.19.33", false, Features{TUN: true, DNS: true, Probe: true, PortHopping: true, CertificatePin: true, UTLS: true}},
	"openclash":    {"OpenClash", protocols, []string{"provider", "config"}, "provider", "mihomo-core; router untested", "", true, Features{TUN: true, DNS: true, Probe: true, PortHopping: true, CertificatePin: true, UTLS: true}},
	"surge":        {"Surge", protocols, []string{"nodes", "config"}, "config", "encoder-only", "", true, Features{DNS: true, PortHopping: true, CertificatePin: true}},
	"surge-mac":    {"Surge Mac", protocols, []string{"nodes", "config"}, "config", "encoder-only", "", true, Features{DNS: true, PortHopping: true, CertificatePin: true}},
	"loon":         {"Loon", []string{"vless", "vmess", "hysteria2", "anytls", "trojan", "ss"}, []string{"nodes", "config"}, "config", "encoder-only", "", true, Features{DNS: true}},
	"quantumult-x": {"Quantumult X", []string{"vless", "vmess", "anytls", "trojan", "ss"}, []string{"nodes", "config"}, "config", "encoder-only", "", true, Features{DNS: true}},
	"surfboard":    {"Surfboard", []string{"vmess", "hysteria2", "tuic", "anytls", "trojan", "ss"}, []string{"nodes", "config"}, "config", "encoder-only", "", true, Features{DNS: true, PortHopping: true, CertificatePin: true}},
	"stash":        {"Stash", protocols, []string{"provider", "config"}, "provider", "encoder-only", "", true, Features{DNS: true, Probe: true}},
	"egern":        {"Egern", protocols, []string{"nodes", "config"}, "config", "encoder-only", "", true, Features{PortHopping: true}},
	"shadowrocket": {"Shadowrocket", protocols, []string{"nodes", "rules"}, "nodes", "uri-roundtrip; app untested", "", true, Features{PortHopping: true, UTLS: true}},
	"uri":          {"通用 URI", protocols, []string{"nodes"}, "nodes", "parser-roundtrip", "", false, Features{PortHopping: true, UTLS: true}},
	"base64":       {"Base64 URI", protocols, []string{"nodes"}, "nodes", "parser-roundtrip", "", false, Features{PortHopping: true, UTLS: true}},
}

func Lookup(id string) (Profile, bool) {
	p, ok := catalog[id]
	if slices.Contains([]string{"sing-box", "mihomo", "openclash", "uri", "base64", "shadowrocket"}, id) {
		p.Features.GRPC, p.Features.HTTPUpgrade = true, true
	}
	if slices.Contains([]string{"stash", "surge", "surge-mac"}, id) {
		p.Features.GRPC = true
	}
	if slices.Contains([]string{"sing-box", "mihomo", "openclash", "stash", "uri", "base64", "shadowrocket"}, id) {
		p.Features.HysteriaObfs = true
	}
	if slices.Contains([]string{"sing-box", "mihomo", "openclash", "stash"}, id) {
		p.Features.PolicyGroups, p.Features.AdvancedDNS, p.Features.RuleProviders, p.Features.NativeTemplate = true, true, true, true
	}
	if id == "egern" {
		p.Features.NativeTemplate = true
	}
	p.Protocols, p.Modes = slices.Clone(p.Protocols), slices.Clone(p.Modes)
	return p, ok
}

func All() map[string]Profile {
	result := make(map[string]Profile, len(catalog))
	for id := range catalog {
		result[id], _ = Lookup(id)
	}
	return result
}

func SupportsAccount(id string) bool { _, ok := catalog[id]; return ok }

func Describe(id string) (Client, bool) {
	p, ok := Lookup(id)
	c := Client{Profile: p, Extension: Extension(id), Connections: map[string]Connection{}}
	for _, protocol := range p.Protocols {
		c.Connections[protocol] = connection(id, protocol)
	}
	switch id {
	case "sing-box", "mihomo":
		c.Platforms = []string{"Windows", "macOS", "Linux", "Android", "iOS"}
	case "openclash":
		c.Platforms = []string{"OpenWrt"}
	case "surge-mac":
		c.Platforms = []string{"macOS"}
	case "surfboard":
		c.Platforms = []string{"Android"}
	case "uri", "base64":
		c.Platforms = []string{"通用导入"}
	default:
		c.Platforms = []string{"iOS", "macOS"}
	}
	return c, ok
}

func Catalog() map[string]Client {
	result := make(map[string]Client, len(catalog))
	for id := range catalog {
		result[id], _ = Describe(id)
	}
	return result
}

func Extension(id string) string {
	switch id {
	case "sing-box":
		return "json"
	case "mihomo", "openclash", "stash", "egern":
		return "yaml"
	case "surge", "surge-mac", "loon", "quantumult-x", "surfboard":
		return "conf"
	default:
		return "txt"
	}
}

func connection(client, protocol string) Connection {
	c := Connection{Transports: []string{"tcp"}}
	uri := slices.Contains([]string{"uri", "base64", "shadowrocket"}, client)
	native := slices.Contains([]string{"sing-box", "mihomo", "openclash", "stash"}, client)
	if slices.Contains([]string{"vless", "vmess", "trojan"}, protocol) {
		c.Transports = append(c.Transports, "ws")
		if native || uri {
			c.Transports = append(c.Transports, "grpc")
			c.GRPCModes = []string{"gun"}
		}
		if client != "stash" && (native || uri) {
			c.Transports = append(c.Transports, "httpupgrade")
		}
		if (client == "surge" || client == "surge-mac") && protocol == "vless" {
			c.Transports = append(c.Transports, "grpc")
			c.GRPCModes = []string{"gun", "multi"}
			c.GRPCAuthority = true
		}
		if uri && protocol != "vmess" {
			c.GRPCModes = []string{"gun", "multi"}
			c.GRPCAuthority = true
		}
		c.EarlyData = client == "sing-box" || client == "mihomo" || client == "openclash" || client == "stash" || uri
	}
	if protocol == "vless" {
		c.Reality = true
		if client == "mihomo" || client == "openclash" || client == "stash" || client == "surge" || client == "surge-mac" || uri {
			c.Transports = append(c.Transports, "xhttp")
		}
		c.XHTTPExtra = client == "mihomo" || client == "openclash" || client == "stash" || uri
	}
	c.UTLS = (client == "sing-box" || client == "mihomo" || client == "openclash" || uri) && slices.Contains([]string{"vless", "vmess", "trojan", "anytls"}, protocol)
	if protocol == "ss" {
		c.Ciphers = []string{"aes-128-gcm", "aes-256-gcm", "chacha20-ietf-poly1305", "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm"}
		if client == "sing-box" || client == "mihomo" || client == "openclash" || client == "uri" || client == "base64" {
			c.Ciphers = append(c.Ciphers, "2022-blake3-chacha20-poly1305")
		}
		if native || uri {
			c.Plugins = []string{"obfs", "v2ray-plugin"}
		} else if slices.Contains([]string{"surge", "surge-mac", "loon", "quantumult-x", "surfboard"}, client) {
			c.Plugins = []string{"obfs"}
		}
	}
	if protocol == "vmess" {
		c.Ciphers = []string{"auto", "none", "zero", "aes-128-gcm", "chacha20-poly1305"}
		if client == "quantumult-x" {
			c.Ciphers = []string{"auto", "none", "chacha20-poly1305"}
		}
	}
	return c
}

var connectionMatrix = func() map[string]map[string]Connection {
	result := make(map[string]map[string]Connection, len(catalog))
	for client, profile := range catalog {
		result[client] = make(map[string]Connection, len(profile.Protocols))
		for _, protocol := range profile.Protocols {
			result[client][protocol] = connection(client, protocol)
		}
	}
	return result
}()

// ConnectionFor is the read-only compiler view; UI metadata is independently
// constructed by Describe so callers cannot mutate this shared registry.
func ConnectionFor(client, protocol string) (Connection, bool) {
	value, ok := connectionMatrix[client][protocol]
	return value, ok
}
