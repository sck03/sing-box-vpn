package subscription

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/sck03/sing-box-vpn/internal/clientprofile"
)

func Capabilities() map[string]clientprofile.Profile { return clientprofile.All() }

type Artifact struct {
	Name string `json:"name"`
	MIME string `json:"mime"`
	Data []byte `json:"data"`
	ETag string `json:"etag"`
}

func capabilityError(n Node, client string) error {
	cap, ok := clientprofile.Lookup(client)
	if !ok || !slices.Contains(cap.Protocols, n.Protocol) {
		return fmt.Errorf("%s does not support %s", client, n.Protocol)
	}
	if n.Transport.Type == "grpc" && !cap.Features.GRPC || n.Transport.Type == "httpupgrade" && !cap.Features.HTTPUpgrade {
		return errors.New("target adapter cannot preserve this transport")
	}
	if n.Obfs != "" && !cap.Features.HysteriaObfs {
		return errors.New("target adapter cannot preserve Hysteria2 obfuscation")
	}
	if client == "quantumult-x" && n.Protocol == "vmess" && !slices.Contains([]string{"", "auto", "none", "chacha20-poly1305"}, n.Cipher) {
		return errors.New("Quantumult X cannot preserve this VMess security")
	}
	if len(n.TLS.PublicKeyPins) > 0 && n.TLS.CertificateHash == "" && client != "sing-box" {
		return errors.New("target cannot preserve public-key certificate pin")
	}
	uri := slices.Contains([]string{"uri", "base64", "shadowrocket"}, client)
	// URI formats can carry a Hysteria2 hash; other URI protocols explicitly
	// report their import limitation below. Native adapters never drop a pin.
	if n.TLS.CertificateHash != "" && !cap.Features.CertificatePin && client != "sing-box" && !(uri && (n.Protocol == "hysteria2" || n.TLS.Insecure)) {
		return errors.New("target cannot preserve certificate verification pin")
	}
	if len(n.Ports) > 0 && !cap.Features.PortHopping {
		return errors.New("target adapter cannot preserve port hopping")
	}
	if n.TLS.Fingerprint != "" && !cap.Features.UTLS {
		return errors.New("target adapter cannot preserve the requested TLS client fingerprint")
	}
	if client == "egern" && len(n.TLS.ALPN) > 0 && n.Protocol != "tuic" {
		return errors.New("Egern adapter cannot preserve ALPN for this protocol")
	}
	if n.Protocol == "tuic" && (n.Congestion != "" || n.UDPRelay != "") && slices.Contains([]string{"egern", "surge", "surge-mac", "surfboard"}, client) {
		return errors.New("target adapter cannot preserve explicit TUIC congestion or UDP relay settings")
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
	selected, diagnostics, err := Select(nodes, t, skipUnsupported)
	if err != nil {
		return a, diagnostics, err
	}
	return renderSelected(selected, t, a, diagnostics)
}

// Select is shared by rendering and probes so a skipped node is never tested
// or presented as part of a published output.
func Select(nodes []Node, t Template, skipUnsupported bool) ([]Node, []Diagnostic, error) {
	diagnostics := []Diagnostic{}
	if err := t.Validate(); err != nil {
		return nil, diagnostics, err
	}
	selected := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		n = normalizeNode(n)
		if err := n.Validate(); err != nil {
			return nil, diagnostics, err
		}
		if err := capabilityError(n, t.Client); err != nil {
			diagnostics = append(diagnostics, Diagnostic{n.ID, n.Name, "capability", err.Error()})
			if !skipUnsupported {
				return nil, diagnostics, err
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
		return nil, diagnostics, errors.New("cannot publish an empty node selection")
	}
	names := make(map[string]bool, len(selected))
	for _, n := range selected {
		if names[n.Name] || reservedName(n.Name) {
			return nil, diagnostics, fmt.Errorf("duplicate or reserved node name %q", n.Name)
		}
		names[n.Name] = true
	}
	if err := validatePolicy(selected, t); err != nil {
		return nil, diagnostics, err
	}
	return selected, diagnostics, nil
}

func reservedName(name string) bool {
	return slices.Contains([]string{"PROXY", "DIRECT", "AUTO", "REJECT", "VPNGATE"}, strings.ToUpper(name))
}

func renderSelected(selected []Node, t Template, a Artifact, diagnostics []Diagnostic) (Artifact, []Diagnostic, error) {
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
	if gateway && !normal && len(t.Native) > 0 {
		return errors.New("VPNGate-only output requires the managed template")
	}
	names := map[string]bool{}
	for _, node := range nodes {
		names[node.Name] = true
	}
	for _, group := range t.Groups {
		if names[group.ID] || len(policyMembers(group, nodes)) == 0 {
			return errors.New("empty policy group or group/node name conflict")
		}
		if gateway && !normal && group.Scope == "normal" {
			return errors.New("VPNGate-only output cannot use a normal group")
		}
	}
	for _, provider := range t.Providers {
		if gateway && !normal && provider.Target == "DIRECT" {
			return errors.New("VPNGate-only output cannot route providers to DIRECT")
		}
		if provider.Target == "VPNGate" && !gateway {
			return errors.New("provider references empty VPNGate group")
		}
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
