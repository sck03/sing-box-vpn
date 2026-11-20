package subscription

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

type artifactEncoder func([]Node, Template) ([]byte, string, error)

// Client schemas meet at this registry; no new client silently falls back to
// another client's text syntax. Connection checks remain in the shared planner.
var renderers = map[string]artifactEncoder{
	"sing-box":  nativeEncoder("outbounds", "application/json", Node.SingBox, singBoxTemplate),
	"mihomo":    nativeEncoder("proxies", "application/yaml", Node.Mihomo, clashTemplate),
	"openclash": nativeEncoder("proxies", "application/yaml", Node.Mihomo, clashTemplate),
	"stash":     nativeEncoder("proxies", "application/yaml", stashNode, stashTemplate),
	"egern":     nativeEncoder("proxies", "application/yaml", egernNode, egernTemplate),
	"uri":       uriEncoder, "base64": uriEncoder, "shadowrocket": uriEncoder,
	"surge": textEncoder, "surge-mac": textEncoder, "loon": textEncoder, "quantumult-x": textEncoder, "surfboard": textEncoder,
}

func nativeEncoder(key, mime string, node func(Node) Object, config func([]Node, []Object, Template) Object) artifactEncoder {
	return func(nodes []Node, t Template) ([]byte, string, error) {
		native := make([]Object, 0, len(nodes))
		for _, n := range nodes {
			native = append(native, node(n))
		}
		value := Object{key: native}
		if t.Mode == "config" {
			value = config(nodes, native, t)
		}
		// JSON is YAML 1.2 and preserves deterministic scalar and key encoding.
		data, err := json.MarshalIndent(value, "", "  ")
		return data, mime, err
	}
}

func uriEncoder(nodes []Node, t Template) ([]byte, string, error) {
	if t.Mode == "rules" {
		return []byte(shadowrocketRules(t)), "text/plain", nil
	}
	lines := make([]string, 0, len(nodes))
	for _, n := range nodes {
		lines = append(lines, n.URI())
	}
	data := strings.Join(lines, "\n")
	if t.Client == "base64" || t.Client == "shadowrocket" {
		data = base64.StdEncoding.EncodeToString([]byte(data))
	}
	return []byte(data + "\n"), "text/plain", nil
}

func textEncoder(nodes []Node, t Template) ([]byte, string, error) {
	data, err := textTemplate(nodes, t)
	return data, "text/plain", err
}

type PreparedOutput struct {
	nodes       []Node
	template    Template
	diagnostics []Diagnostic
	valid       bool
}

func PrepareOutput(nodes []Node, t Template, skip bool) (PreparedOutput, error) {
	selected, diagnostics, err := Select(nodes, t, skip)
	return PreparedOutput{selected, t, diagnostics, err == nil}, err
}

func (p PreparedOutput) NodeCount() int { return len(p.nodes) }
func (p PreparedOutput) Diagnostics() []Diagnostic {
	return append([]Diagnostic(nil), p.diagnostics...)
}
func (p PreparedOutput) Render() (Artifact, []Diagnostic, error) {
	if !p.valid {
		return Artifact{}, p.Diagnostics(), errors.New("output plan is not valid")
	}
	return renderSelected(p.nodes, p.template, Artifact{Name: fallback(p.template.Name, p.template.ID)}, p.diagnostics)
}
