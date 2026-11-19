package subscription

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/sck03/sing-box-vpn/internal/clientprofile"
)

func Capabilities() map[string]clientprofile.Client { return clientprofile.Catalog() }

type Artifact struct {
	Name      string `json:"name"`
	Extension string `json:"extension,omitempty"`
	MIME      string `json:"mime"`
	Data      []byte `json:"data"`
	ETag      string `json:"etag"`
}

func capabilityError(n Node, client string, cap clientprofile.Profile) error {
	connection, ok := clientprofile.ConnectionFor(client, n.Protocol)
	if !ok {
		return fmt.Errorf("%s does not support %s", client, n.Protocol)
	}
	if !slices.Contains(connection.Transports, fallback(n.Transport.Type, "tcp")) {
		return errors.New("target adapter cannot preserve this transport")
	}
	if len(connection.Ciphers) > 0 && !slices.Contains(connection.Ciphers, fallback(n.Cipher, "auto")) {
		return errors.New("target adapter cannot preserve this cipher")
	}
	if n.Plugin != nil && !slices.Contains(connection.Plugins, n.Plugin.Type) {
		return errors.New("target adapter cannot preserve this SIP003 plugin")
	}
	if n.Transport.Type == "grpc" && (!slices.Contains(connection.GRPCModes, fallback(n.Transport.Mode, "gun")) || n.Transport.Authority != "" && !connection.GRPCAuthority) {
		return errors.New("target adapter cannot preserve gRPC mode or authority")
	}
	if (n.Transport.EarlyData != 0 || n.Transport.EarlyDataHeader != "") && !connection.EarlyData {
		return errors.New("target adapter cannot preserve WebSocket early data")
	}
	if n.Transport.XHTTP != nil && !connection.XHTTPExtra {
		return errors.New("target adapter cannot preserve XHTTP extra options")
	}
	if n.TLS.RealityKey != "" && (client == "stash" && n.Transport.Type != "" && n.Transport.Type != "tcp" || (client == "surge" || client == "surge-mac") && n.Transport.Type == "ws") {
		return errors.New("target adapter cannot combine Reality with this transport")
	}
	if n.Obfs != "" && !cap.Features.HysteriaObfs {
		return errors.New("target adapter cannot preserve Hysteria2 obfuscation")
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
	if n.TLS.Fingerprint != "" && !connection.UTLS {
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
	if !slices.Contains([]string{"sing-box", "mihomo", "openclash", "stash", "egern", "uri", "base64", "shadowrocket"}, client) {
		fields := []string{n.Name, n.Server, n.Password, n.Transport.Path, n.Transport.Host, n.Transport.Authority, n.Transport.ServiceName, n.TLS.ServerName}
		fields = append(fields, n.TLS.ALPN...)
		if n.Plugin != nil {
			fields = append(fields, n.Plugin.Host, n.Plugin.Path)
		}
		for _, field := range fields {
			if strings.ContainsAny(field, ",\"=\r\n|[]") {
				return errors.New("target text format cannot safely represent a field delimiter")
			}
		}
	}
	return nil
}

func Render(nodes []Node, t Template, skipUnsupported bool) (Artifact, []Diagnostic, error) {
	plan, err := PrepareOutput(nodes, t, skipUnsupported)
	if err != nil {
		return Artifact{Name: t.ID}, plan.Diagnostics(), err
	}
	return plan.Render()
}

// Select is shared by rendering and probes so a skipped node is never tested
// or presented as part of a published output.
func Select(nodes []Node, t Template, skipUnsupported bool) ([]Node, []Diagnostic, error) {
	diagnostics := []Diagnostic{}
	if err := t.Validate(); err != nil {
		return nil, diagnostics, err
	}
	profile, _ := clientprofile.Lookup(t.Client)
	selected := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		n = normalizeNode(n)
		if err := n.Validate(); err != nil {
			return nil, diagnostics, err
		}
		if err := capabilityError(n, t.Client, profile); err != nil {
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
	if len(t.Groups) > 0 {
		names := policyDisplayNames(t, selected)
		for _, group := range t.Groups {
			if names[group.ID] != fallback(group.Name, group.ID) {
				diagnostics = append(diagnostics, Diagnostic{NodeID: group.ID, Name: group.Name, Stage: "rename", Message: "策略组显示名称调整为 " + names[group.ID]})
			}
		}
	}
	return selected, diagnostics, nil
}

func reservedName(name string) bool {
	return slices.Contains([]string{"PROXY", "DIRECT", "AUTO", "REJECT", "VPNGATE"}, strings.ToUpper(name))
}

func renderSelected(selected []Node, t Template, a Artifact, diagnostics []Diagnostic) (Artifact, []Diagnostic, error) {
	t = renderedTemplate(t, selected)
	a.Extension = clientprofile.Extension(t.Client)
	if t.Mode == "rules" {
		a.Extension = "conf"
	}
	encode, exists := renderers[t.Client]
	if !exists {
		return a, diagnostics, errors.New("client renderer is not registered")
	}
	var err error
	a.Data, a.MIME, err = encode(selected, t)
	if err != nil {
		return a, diagnostics, err
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
	if gateway && !normal && (t.Preset == "split" || t.PrivateNetwork) {
		return errors.New("VPNGate-only output cannot use direct split routing")
	}
	if gateway && !normal && len(t.Native) > 0 {
		return errors.New("VPNGate-only output requires the managed template")
	}
	if t.Final == "VPNGate" && !gateway {
		return errors.New("final policy references empty VPNGate group")
	}
	byName := map[string]Node{}
	for _, node := range nodes {
		byName[node.Name] = node
	}
	groups := map[string]PolicyGroup{}
	for _, group := range t.Groups {
		groups[group.ID] = group
	}
	masks := map[string]uint8{}
	var scopeMask func(string) uint8
	scopeMask = func(id string) uint8 {
		if mask, found := masks[id]; found {
			return mask
		}
		g := groups[id]
		var mask uint8
		if len(g.Members) > 0 {
			for _, member := range g.Members {
				mask |= scopeMask(member)
			}
		} else {
			for _, name := range policyMembers(g, nodes) {
				if byName[name].Gateway {
					mask |= 2
				} else {
					mask |= 1
				}
			}
		}
		masks[id] = mask
		return mask
	}
	expanded := 0
	for _, group := range t.Groups {
		members := policyMembers(group, nodes)
		expanded += len(members)
		if expanded > MaxNodes*8 {
			return errors.New("policy member budget exceeded")
		}
		mask := scopeMask(group.ID)
		if group.Scope == "vpngate" && mask&1 != 0 || group.Scope == "normal" && mask&2 != 0 {
			return errors.New("nested policy group crosses its declared runtime scope")
		}
		empty := len(members) == 0
		if empty && !group.Optional {
			return errors.New("empty policy group")
		}
		if empty {
			if t.Final == group.ID {
				return errors.New("final policy is empty")
			}
			for _, rule := range t.Rules {
				if rule.Target == group.ID {
					return errors.New("rule references an empty optional policy")
				}
			}
			for _, provider := range t.Providers {
				if provider.Target == group.ID {
					return errors.New("provider references an empty optional policy")
				}
			}
			for _, g := range t.Groups {
				if slices.Contains(g.Members, group.ID) {
					return errors.New("policy references an empty optional policy")
				}
			}
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
