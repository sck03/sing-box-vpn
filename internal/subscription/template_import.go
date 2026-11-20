package subscription

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	strict "github.com/sck03/sing-box-vpn/internal/encoding"
	"go.yaml.in/yaml/v3"
)

// Import extracts declarative routing rules, never nodes, secrets or executable scripts.
type TemplateImport struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Client        string `json:"client"`
	Format        string `json:"format"`
	Content       string `json:"content"`
	SourceURL     string `json:"source_url"`
	Revision      string `json:"revision"`
	DefaultTarget string `json:"default_target"`
}
type TemplateImportResult struct {
	Template Template `json:"template"`
	Warnings []string `json:"warnings"`
}

func ImportTemplate(input TemplateImport) (TemplateImportResult, error) {
	result := TemplateImportResult{Warnings: []string{}}
	if len(input.Content) == 0 || len(input.Content) > 512<<10 || !ValidID(input.ID) {
		return result, errors.New("a valid ID and 1–512 KiB template are required")
	}
	t := Template{ID: input.ID, Name: input.Name, Client: input.Client, Version: 1, Mode: "config", Preset: "proxy"}
	target := input.DefaultTarget
	if target == "" {
		target = "PROXY"
	}
	if _, err := importTarget(target); err != nil {
		return result, err
	}
	var err error
	switch input.Format {
	case "project-json":
		if err = strict.JSON([]byte(input.Content), &t, 512<<10); err != nil {
			return result, err
		}
		if t.Client != input.Client {
			return result, errors.New("template client does not match the selected target")
		}
		t.ID, t.Name, t.URL = input.ID, input.Name, ""
	case "mihomo-yaml":
		var tree yaml.Node
		decoder := yaml.NewDecoder(strings.NewReader(input.Content))
		if err = decoder.Decode(&tree); err != nil {
			return result, err
		}
		if err = decoder.Decode(new(yaml.Node)); !errors.Is(err, io.EOF) {
			return result, errors.New("one YAML document is required")
		}
		if err = validateYAMLTree(&tree, 0); err != nil {
			return result, err
		}
		var config struct {
			Rules []string `yaml:"rules"`
		}
		if err = tree.Decode(&config); err != nil {
			return result, err
		}
		for i, rule := range config.Rules {
			if err = importRuleLine(&t, rule, target); err != nil {
				return result, fmt.Errorf("rules[%d]: %w", i, err)
			}
		}
		result.Warnings = append(result.Warnings, "仅提取 rules；节点、策略组、DNS、脚本和监听参数不导入。仅接受 DIRECT / PROXY / REJECT 目标。")
	case "rule-list":
		for i, line := range strings.Split(input.Content, "\n") {
			if err = importRuleLine(&t, line, target); err != nil {
				return result, fmt.Errorf("line %d: %w", i+1, err)
			}
		}
	case "sing-box-json":
		var config struct {
			Route struct {
				Rules []json.RawMessage `json:"rules"`
				Final string            `json:"final"`
			} `json:"route"`
		}
		// Decode the entire document but retain only explicitly supported routing rules.
		decoder := json.NewDecoder(bytes.NewBufferString(input.Content))
		if err = decoder.Decode(&config); err != nil {
			return result, err
		}
		if err = decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
			return result, errors.New("one JSON document is required")
		}
		for i, raw := range config.Route.Rules {
			if err = importSingBoxRule(&t, raw); err != nil {
				return result, fmt.Errorf("route.rules[%d]: %w", i, err)
			}
		}
		if config.Route.Final != "" {
			final, e := importTarget(config.Route.Final)
			if e != nil || final != "PROXY" {
				return result, errors.New("import supports only a PROXY default route")
			}
		}
		result.Warnings = append(result.Warnings, "仅提取简单路由规则；入站、出站、DNS、规则集和设备参数由本项目重新生成。多条件与逻辑规则需手动配置。")
	default:
		return result, errors.New("unsupported template format; use project-json, mihomo-yaml, sing-box-json or rule-list")
	}
	if input.Format != "project-json" && len(t.Rules) == 0 {
		return result, errors.New("no supported routing rules found")
	}
	t.Origin = &TemplateOrigin{SourceURL: input.SourceURL, Revision: input.Revision, Format: input.Format, Digest: Digest([]byte(input.Content))}
	if err = t.Validate(); err != nil {
		return result, err
	}
	result.Template = t
	return result, nil
}
func importTarget(value string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "PROXY":
		return "PROXY", nil
	case "DIRECT":
		return "DIRECT", nil
	case "REJECT", "BLOCK":
		return "REJECT", nil
	default:
		return "", fmt.Errorf("unsupported policy target %q; map it to PROXY, DIRECT or REJECT first", value)
	}
}
func importRuleLine(t *Template, line, defaultTarget string) error {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "//") {
		return nil
	}
	if t.Final != "" {
		return errors.New("rules after MATCH/FINAL cannot be imported without changing behavior")
	}
	parts := strings.Split(line, ",")
	if len(parts) < 2 || len(parts) > 3 {
		return errors.New("rule must be TYPE,value[,target]; INI and script templates require conversion")
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	if strings.EqualFold(parts[0], "MATCH") || strings.EqualFold(parts[0], "FINAL") {
		target, err := importTarget(parts[1])
		if err != nil || target != "PROXY" || len(parts) != 2 {
			return errors.New("only a PROXY final route is supported")
		}
		t.Final = "PROXY"
		return nil
	}
	kind := map[string]string{"DOMAIN": "domain", "DOMAIN-SUFFIX": "domain_suffix", "IP-CIDR": "ip_cidr", "IP-CIDR6": "ip_cidr"}[strings.ToUpper(parts[0])]
	if kind == "" {
		return fmt.Errorf("unsupported rule type %q", parts[0])
	}
	target := defaultTarget
	if len(parts) == 3 {
		target = parts[2]
	}
	target, err := importTarget(target)
	if err != nil {
		return err
	}
	t.Rules = append(t.Rules, Rule{Type: kind, Value: parts[1], Target: target})
	return nil
}
func importSingBoxRule(t *Template, raw json.RawMessage) error {
	var rule struct {
		Domain   []string `json:"domain"`
		Suffix   []string `json:"domain_suffix"`
		CIDR     []string `json:"ip_cidr"`
		Action   string   `json:"action"`
		Outbound string   `json:"outbound"`
	}
	if err := strict.JSON(raw, &rule, 512<<10); err != nil {
		return err
	}
	if rule.Action != "" && rule.Action != "route" && rule.Action != "reject" {
		return errors.New("unsupported route action")
	}
	count := 0
	for _, values := range [][]string{rule.Domain, rule.Suffix, rule.CIDR} {
		if len(values) > 0 {
			count++
		}
	}
	if count != 1 {
		return errors.New("exactly one match field is required to preserve routing semantics")
	}
	destination := rule.Outbound
	if rule.Action == "reject" {
		if destination != "" {
			return errors.New("reject cannot specify an outbound")
		}
		destination = "REJECT"
	}
	target, err := importTarget(destination)
	if err != nil {
		return err
	}
	for _, item := range []struct {
		kind   string
		values []string
	}{{"domain", rule.Domain}, {"domain_suffix", rule.Suffix}, {"ip_cidr", rule.CIDR}} {
		for _, value := range item.values {
			t.Rules = append(t.Rules, Rule{Type: item.kind, Value: value, Target: target})
		}
	}
	return nil
}
