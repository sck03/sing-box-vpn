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
