package subscription

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

type Capability struct {
	Protocols    []string `json:"protocols"`
	Modes        []string `json:"modes"`
	Verification string   `json:"verification"`
}

var Clients = map[string]Capability{
	"sing-box":     {protocols, []string{"nodes", "config"}, "core-check"},
	"mihomo":       {protocols, []string{"nodes", "provider", "config"}, "core-check"},
	"openclash":    {protocols, []string{"provider", "config"}, "Mihomo core-check; router UI untested"},
	"surge":        {protocols, []string{"nodes", "config"}, "syntax-only"},
	"surge-mac":    {protocols, []string{"nodes", "config"}, "syntax-only"},
	"loon":         {[]string{"vless", "vmess", "hysteria2", "anytls", "trojan", "ss"}, []string{"nodes", "config"}, "syntax-only"},
	"quantumult-x": {[]string{"vless", "vmess", "anytls", "trojan", "ss"}, []string{"nodes", "config"}, "syntax-only"},
	"surfboard":    {[]string{"vmess", "hysteria2", "tuic", "anytls", "trojan", "ss"}, []string{"nodes", "config"}, "syntax-only"},
	"stash":        {protocols, []string{"provider", "config"}, "syntax-only"},
	"egern":        {protocols, []string{"nodes", "config"}, "syntax-only"},
	"shadowrocket": {protocols, []string{"nodes", "rules"}, "syntax-only"},
	"uri":          {protocols, []string{"nodes"}, "parser-roundtrip"},
	"base64":       {protocols, []string{"nodes"}, "parser-roundtrip"},
}

type Artifact struct {
	Name string `json:"name"`
	MIME string `json:"mime"`
	Data []byte `json:"data"`
	ETag string `json:"etag"`
}

func capabilityError(n Node, client string) error {
	cap, ok := Clients[client]
	if !ok || !slices.Contains(cap.Protocols, n.Protocol) {
		return fmt.Errorf("%s does not support %s", client, n.Protocol)
	}
	if len(n.TLS.PublicKeyPins) > 0 && n.TLS.CertificateHash == "" && client != "sing-box" {
		return errors.New("target cannot preserve public-key certificate pin")
	}
	if n.TLS.CertificateHash != "" && !n.TLS.Insecure && !slices.Contains([]string{"sing-box", "mihomo", "openclash", "surge", "surge-mac", "surfboard"}, client) {
		return errors.New("target cannot preserve certificate verification pin")
	}
	if len(n.Ports) > 0 && slices.Contains([]string{"loon", "stash", "quantumult-x"}, client) {
		return errors.New("target adapter cannot preserve port hopping")
	}
	if client == "sing-box" && n.TLS.CertificateHash != "" && len(n.TLS.PublicKeyPins) == 0 {
		return errors.New("sing-box requires a public-key pin, not a certificate hash")
	}
	if n.Transport.Type == "ws" && n.Protocol != "vmess" && n.Protocol != "vless" && n.Protocol != "trojan" {
		return errors.New("unsupported protocol transport")
	}
	if !slices.Contains([]string{"sing-box", "mihomo", "openclash", "stash", "egern", "uri", "base64", "shadowrocket"}, client) {
		for _, field := range []string{n.Name, n.Password, n.Transport.Path, n.Transport.Host, n.TLS.ServerName} {
			if strings.ContainsAny(field, ",\"=\r\n|[]") {
				return errors.New("target text format cannot safely represent a field delimiter")
			}
		}
	}
	return nil
}

func Render(nodes []Node, t Template, skipUnsupported bool) (Artifact, []Diagnostic, error) {
	a := Artifact{Name: t.ID}
	diagnostics := []Diagnostic{}
	if err := t.Validate(); err != nil {
		return a, diagnostics, err
	}
	selected := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		if err := n.Validate(); err != nil {
			return a, diagnostics, err
		}
		if err := capabilityError(n, t.Client); err != nil {
			diagnostics = append(diagnostics, Diagnostic{n.ID, n.Name, "capability", err.Error()})
			if !skipUnsupported {
				return a, diagnostics, err
			}
			continue
		}
		selected = append(selected, n)
		if n.TLS.Insecure && (len(n.TLS.PublicKeyPins) > 0 || n.TLS.CertificateHash != "") && !slices.Contains([]string{"sing-box", "mihomo", "openclash", "surge", "surge-mac", "surfboard"}, t.Client) {
			diagnostics = append(diagnostics, Diagnostic{n.ID, n.Name, "tls", "this output may omit the self-signed certificate pin; use a native sing-box/Mihomo output for verified TLS"})
		}
		if slices.Contains([]string{"uri", "base64", "shadowrocket"}, t.Client) && n.TLS.Insecure && (n.Protocol == "vmess" || n.Protocol == "tuic") {
			diagnostics = append(diagnostics, Diagnostic{n.ID, n.Name, "tls", "Mihomo URI import ignores this protocol's self-signed TLS flag; use its native provider"})
		}
	}
	if len(selected) == 0 {
		return a, diagnostics, errors.New("cannot publish an empty node selection")
	}
	if err := validatePolicy(selected, t); err != nil {
		return a, diagnostics, err
	}
	var value any
	var err error
	switch t.Client {
	case "sing-box":
		native := make([]Object, 0, len(selected))
		for _, n := range selected {
			native = append(native, n.SingBox())
		}
		value = Object{"outbounds": native}
		if t.Mode == "config" {
			value = singBoxTemplate(selected, native, t)
		}
		a.MIME = "application/json"
	case "mihomo", "openclash", "stash":
		native := make([]Object, 0, len(selected))
		for _, n := range selected {
			m := n.Mihomo()
			if t.Client == "stash" {
				m = stashNode(n)
			}
			native = append(native, m)
		}
		value = Object{"proxies": native}
		if t.Mode == "config" {
			value = clashTemplate(selected, native, t)
		}
		a.MIME = "application/yaml"
	case "egern":
		native := make([]Object, 0, len(selected))
		for _, n := range selected {
			native = append(native, egernNode(n))
		}
		value = Object{"proxies": native}
		if t.Mode == "config" {
			value = egernTemplate(selected, native, t)
		}
		a.MIME = "application/yaml"
	case "uri", "base64", "shadowrocket":
		var lines []string
		for _, n := range selected {
			lines = append(lines, n.URI())
		}
		data := strings.Join(lines, "\n") + "\n"
		if t.Client == "base64" || t.Client == "shadowrocket" {
			data = base64.StdEncoding.EncodeToString([]byte(strings.TrimSuffix(data, "\n"))) + "\n"
		}
		if t.Mode == "rules" {
			data = shadowrocketRules(t)
		}
		a.Data, a.MIME = []byte(data), "text/plain"
	default:
		a.Data, err = textTemplate(selected, t)
		a.MIME = "text/plain"
	}
	if err != nil {
		return a, diagnostics, err
	}
	if value != nil {
		// JSON is also YAML 1.2. One deterministic encoder avoids ordering and
		// scalar-typing differences between the two native delivery formats.
		a.Data, err = json.MarshalIndent(value, "", "  ")
		if err != nil {
			return a, diagnostics, err
		}
	}
	if len(a.Data) > MaxBody {
		return a, diagnostics, errors.New("output size limit exceeded")
	}
	a.ETag = `"` + Digest(a.Data) + `"`
	return a, diagnostics, nil
}

func validatePolicy(nodes []Node, t Template) error {
	normal, gateway := false, false
	for _, n := range nodes {
		if n.Gateway {
			gateway = true
		} else {
			normal = true
		}
	}
	if gateway && !normal && t.Preset == "split" {
		return errors.New("VPNGate-only output cannot use direct split routing")
	}
	for _, r := range t.Rules {
		if r.Target == "VPNGate" && !gateway {
			return errors.New("rule references empty VPNGate group")
		}
		if gateway && !normal && r.Target == "DIRECT" {
			return errors.New("VPNGate-only output cannot route to DIRECT")
		}
	}
	return nil
}
