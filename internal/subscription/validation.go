package subscription

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

type Validation struct {
	State               string `json:"state"`
	Engine              string `json:"engine"`
	Scope               string `json:"scope"`
	Cached              bool   `json:"cached"`
	ApplicationVerified bool   `json:"application_verified"`
}

func (v Validation) String() string {
	text := v.Engine + " " + v.Scope + ": " + v.State
	if v.Cached {
		text += " (cached)"
	}
	return text
}

// ValidateArtifact checks encoded nodes without an external client binary.
// Template policy validation is performed by Select before rendering. Parsing
// cannot certify a client's complete configuration or application import.
func ValidateArtifact(t Template, a Artifact) (Validation, error) {
	result := Validation{State: "encoded", Engine: t.Client, Scope: "encoder"}
	if t.Mode == "rules" {
		return result, nil
	}
	format := artifactParsers[t.Client]
	if format == "" {
		return result, nil
	}
	result.Scope = "parser"
	nodes, err := Parse(a.Data, format, "verification")
	if err != nil {
		result.State = "failed"
		return result, fmt.Errorf("generated output failed node parser validation: %w", err)
	}
	if len(nodes) != a.NodeCount || a.ConnectionDigest == "" || connectionDigest(nodes, t.Client) != a.ConnectionDigest {
		result.State = "failed"
		return result, fmt.Errorf("generated output changed node count or connection parameters (expected %d, parsed %d)", a.NodeCount, len(nodes))
	}
	result.State = "passed"
	return result, nil
}

var artifactParsers = map[string]string{
	"sing-box": "sing-box", "stash": "stash",
	"mihomo": "mihomo", "openclash": "mihomo",
	"uri": "uri", "base64": "base64", "shadowrocket": "base64",
	"surge": "surge", "surge-mac": "surge", "surfboard": "surge",
	"loon": "loon", "quantumult-x": "quantumult-x",
}

// Compare connection semantics independently of the encoder. Defaults are
// normalized here; secrets never appear in diagnostics or persisted summaries.
func connectionDigest(nodes []Node, client string) string {
	keys := make([]string, 0, len(nodes))
	for _, node := range nodes {
		n := normalizeNode(node)
		n.ID, n.SourceID, n.Tags, n.Gateway = "", "", nil, false
		n.UUID = strings.ToLower(n.UUID)
		if n.Transport.Type == "tcp" {
			n.Transport.Type = ""
		}
		if n.Transport.EarlyData == 0 {
			n.Transport.EarlyDataHeader = ""
		}
		if n.Protocol == "vmess" {
			n.Cipher = vmessMethod(n.Cipher, client)
		}
		if n.Protocol == "tuic" {
			n.Congestion = fallback(n.Congestion, "bbr")
			n.UDPRelay = fallback(n.UDPRelay, "native")
		}
		if !n.TLS.Enabled {
			n.TLS = TLS{}
		}
		if n.TLS.RealityKey != "" && slices.Contains([]string{"uri", "base64", "shadowrocket", "sing-box"}, client) {
			n.TLS.Fingerprint = fallback(n.TLS.Fingerprint, "chrome")
		}
		// URI omission is allowed only for the documented insecure TLS case.
		if client != "sing-box" && n.TLS.CertificateHash != "" {
			n.TLS.PublicKeyPins = nil
			if client == "mihomo" || client == "openclash" {
				n.TLS.Insecure = false
			}
		}
		if client == "sing-box" {
			n.TLS.CertificateHash = ""
			if len(n.TLS.PublicKeyPins) > 0 {
				n.TLS.Insecure = false
			}
		}
		if slices.Contains([]string{"uri", "base64", "shadowrocket"}, client) && n.Protocol != "hysteria2" && n.TLS.Insecure {
			n.TLS.CertificateHash = ""
		}
		n.Ports = slices.Clone(n.Ports)
		for i, port := range n.Ports {
			n.Ports[i] = strings.ReplaceAll(port, ":", "-")
			if first, last, ok := strings.Cut(n.Ports[i], "-"); ok && first == last {
				n.Ports[i] = first
			}
		}
		data, _ := json.Marshal(n)
		keys = append(keys, Digest(data))
	}
	slices.Sort(keys)
	return Digest([]byte(strings.Join(keys, ":")))
}
