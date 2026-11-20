package subscription

import (
	"errors"
	"fmt"
	"github.com/sck03/sing-box-vpn/internal/clientprofile"
	"regexp"
	"slices"
	"strings"
)

type PolicyGroup struct {
	ID              string   `json:"id"`
	Name            string   `json:"name,omitempty"`
	Optional        bool     `json:"optional,omitempty"`
	Type            string   `json:"type"`
	Scope           string   `json:"scope,omitempty"`
	Include         string   `json:"include,omitempty"`
	Members         []string `json:"members,omitempty"`
	URL             string   `json:"url,omitempty"`
	IntervalSeconds int      `json:"interval_seconds,omitempty"`
	Tolerance       int      `json:"tolerance,omitempty"`
}

type RuleProvider struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Type   string `json:"type"`
	Format string `json:"format"`
	Target string `json:"target"`
}

func validateTemplateExtensions(t Template) error {
	cap, _ := clientprofile.Lookup(t.Client)
	if len(t.Groups)+len(t.Providers) > MaxObjects {
		return errors.New("template resource limit exceeded")
	}
	if (len(t.Groups) > 0 || len(t.Providers) > 0 || t.DNSPolicy != nil || len(t.Native) > 0) && t.Mode != "config" {
		return errors.New("template extensions require complete config mode")
	}
	fields := clientprofile.TemplateFields(t.Client)
	for _, key := range fields.Managed {
		if _, exists := t.Native[key]; exists {
			return fmt.Errorf("native.%s is managed by the template compiler; use the corresponding template setting", key)
		}
	}
	for _, key := range fields.Forbidden {
		if _, exists := t.Native[key]; exists {
			return fmt.Errorf("native.%s is forbidden; declare sources and rules in the subscription configuration", key)
		}
	}
	if len(t.Groups) > 0 && !cap.Features.PolicyGroups || len(t.Providers) > 0 && !cap.Features.RuleProviders || t.DNSPolicy != nil && !cap.Features.AdvancedDNS || len(t.Native) > 0 && !cap.Features.NativeTemplate {
		return errors.New("target adapter cannot preserve template extensions")
	}
	groups := map[string]PolicyGroup{}
	names := map[string]bool{}
	for _, g := range t.Groups {
		if !ValidID(g.ID) || reservedName(g.ID) || g.ID == "cn-domains" || g.ID == "cn-ips" {
			return errors.New("invalid policy group ID")
		}
		name := fallback(g.Name, g.ID)
		if !slices.Contains([]string{"sing-box", "mihomo", "openclash", "stash", "egern"}, t.Client) && textCommentName(name) {
			return errors.New("target text format cannot represent a comment-prefixed policy name")
		}
		if len(name) > 256 || names[name] || reservedName(name) || strings.ContainsAny(name, ",\"=\r\n\x00|[]") {
			return errors.New("invalid or duplicate policy display name")
		}
		names[name] = true
		if _, exists := groups[g.ID]; exists {
			return errors.New("duplicate policy group")
		}
		if !slices.Contains([]string{"select", "urltest", "fallback"}, g.Type) || g.Type == "fallback" && t.Client == "sing-box" {
			return errors.New("unsupported policy group type")
		}
		if !slices.Contains([]string{"", "normal", "vpngate"}, g.Scope) {
			return errors.New("invalid group scope")
		}
		if len(g.Include) > 1024 || g.IntervalSeconds < 0 || g.IntervalSeconds > 86400 || g.Tolerance < 0 || g.Tolerance > 10000 || len(g.Members) > MaxObjects {
			return errors.New("invalid policy group limits")
		}
		if _, err := regexp.Compile(g.Include); err != nil {
			return err
		}
		if g.URL != "" {
			if err := HTTPURL(g.URL); err != nil {
				return err
			}
		}
		groups[g.ID] = g
	}
	visiting, done := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if done[id] {
			return nil
		}
		if visiting[id] {
			return errors.New("cyclic policy groups")
		}
		g, exists := groups[id]
		if !exists {
			return errors.New("unknown policy group member")
		}
		visiting[id] = true
		for _, member := range g.Members {
			if err := visit(member); err != nil {
				return err
			}
		}
		visiting[id], done[id] = false, true
		return nil
	}
	for id := range groups {
		if err := visit(id); err != nil {
			return err
		}
	}
	checkTarget := func(target string) error {
		if !slices.Contains([]string{"PROXY", "VPNGate", "DIRECT", "REJECT"}, target) {
			if _, exists := groups[target]; !exists {
				return fmt.Errorf("unknown policy target %s", target)
			}
		}
		return nil
	}
	for _, rule := range t.Rules {
		if err := checkTarget(rule.Target); err != nil {
			return err
		}
	}
	if t.Final != "" {
		if !slices.Contains([]string{"PROXY", "VPNGate"}, t.Final) {
			if _, exists := groups[t.Final]; !exists {
				return errors.New("final policy must reference a selector or policy group")
			}
		}
	}
	seen := map[string]bool{}
	for _, p := range t.Providers {
		if !ValidID(p.ID) || p.ID == "cn-domains" || p.ID == "cn-ips" || seen[p.ID] {
			return errors.New("invalid rule provider ID")
		}
		seen[p.ID] = true
		if err := HTTPURL(p.URL); err != nil {
			return err
		}
		if !slices.Contains([]string{"domain", "ip_cidr", "classical"}, p.Type) {
			return errors.New("invalid rule provider type")
		}
		if t.Client == "sing-box" {
			if !slices.Contains([]string{"srs", "json"}, p.Format) {
				return errors.New("sing-box rule providers require srs or json")
			}
		} else if !slices.Contains([]string{"text", "mrs"}, p.Format) || p.Type == "classical" && p.Format == "mrs" {
			return errors.New("unsupported rule provider format")
		}
		if err := checkTarget(p.Target); err != nil {
			return err
		}
	}
	if t.DNSPolicy != nil {
		if t.Client == "stash" && t.DNSPolicy.FakeIP != nil && !*t.DNSPolicy.FakeIP {
			return errors.New("Stash uses fake-IP; configure domain exclusions instead of disabling it")
		}
		if t.Client == "sing-box" && (t.DNSPolicy.FakeIP != nil && *t.DNSPolicy.FakeIP || t.DNSPolicy.IPv6 || len(t.DNSPolicy.FakeIPFilter) > 0 || len(t.DNSPolicy.Local) > 1 || len(t.DNSPolicy.Remote) > 1 || len(t.DNSPolicy.Bootstrap) > 1) {
			return errors.New("sing-box DNS policy accepts one resolver per role and uses native address handling")
		}
		return t.DNSPolicy.Validate()
	}
	return nil
}

func policyMembers(g PolicyGroup, nodes []Node) []string {
	members := append([]string(nil), g.Members...)
	if len(members) > 0 {
		return members
	}
	pattern := regexp.MustCompile(g.Include)
	for _, node := range nodes {
		if (g.Scope == "normal" && node.Gateway) || (g.Scope == "vpngate" && !node.Gateway) || !pattern.MatchString(node.Name) {
			continue
		}
		members = append(members, node.Name)
	}
	return members
}

func policyDefaults(g PolicyGroup) (string, int) {
	interval := g.IntervalSeconds
	if interval == 0 {
		interval = 300
	}
	return fallback(g.URL, "https://www.gstatic.com/generate_204"), interval
}

func applyNativeTemplate(base Object, generated Object) Object {
	if len(base) == 0 {
		return generated
	}
	result := make(Object, len(base)+len(generated))
	// Validation excludes managed sections. User base fields override defaults.
	for key, value := range generated {
		result[key] = value
	}
	for key, value := range base {
		result[key] = value
	}
	return result
}

// Resolve display names only after validating stable IDs and references.
func policyDisplayNames(t Template, nodes []Node) map[string]string {
	names := map[string]string{}
	used := newNodeNames()
	for _, node := range nodes {
		used.used[node.Name] = true
	}
	for _, g := range t.Groups {
		names[g.ID] = used.unique(fallback(g.Name, g.ID))
	}
	return names
}

func renderedTemplate(t Template, nodes []Node) Template {
	if len(t.Groups) == 0 {
		return t
	}
	names := policyDisplayNames(t, nodes)
	reference := func(id string) string {
		if name := names[id]; name != "" {
			return name
		}
		return id
	}
	groups := make([]PolicyGroup, 0, len(t.Groups))
	for _, g := range t.Groups {
		if g.Optional && len(policyMembers(g, nodes)) == 0 {
			continue
		}
		g.ID = reference(g.ID)
		g.Members = append([]string(nil), g.Members...)
		for i, id := range g.Members {
			g.Members[i] = reference(id)
		}
		groups = append(groups, g)
	}
	t.Groups = groups
	t.Rules = append([]Rule(nil), t.Rules...)
	for i := range t.Rules {
		t.Rules[i].Target = reference(t.Rules[i].Target)
	}
	t.Providers = append([]RuleProvider(nil), t.Providers...)
	for i := range t.Providers {
		t.Providers[i].Target = reference(t.Providers[i].Target)
	}
	t.Final = reference(t.Final)
	return t
}
