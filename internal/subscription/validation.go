package subscription

import "fmt"

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
	if _, err := Parse(a.Data, format, "verification"); err != nil {
		result.State = "failed"
		return result, fmt.Errorf("generated output failed node parser validation: %w", err)
	}
	result.State = "passed"
	return result, nil
}

var artifactParsers = map[string]string{
	"mihomo": "mihomo", "openclash": "mihomo",
	"uri": "uri", "base64": "base64", "shadowrocket": "base64",
	"surge": "surge", "surge-mac": "surge", "surfboard": "surge",
	"loon": "loon", "quantumult-x": "quantumult-x",
}
