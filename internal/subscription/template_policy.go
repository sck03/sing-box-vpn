package subscription

import (
	"errors"
	"fmt"
	"github.com/sck03/sing-box-vpn/internal/clientprofile"
	"regexp"
	"slices"
)

type PolicyGroup struct {
	ID              string   `json:"id"`
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
	if len(t.Groups) > 0 && !cap.Features.PolicyGroups || len(t.Providers) > 0 && !cap.Features.RuleProviders || t.DNSPolicy != nil && !cap.Features.AdvancedDNS || len(t.Native) > 0 && !cap.Features.NativeTemplate {
		return errors.New("target adapter cannot preserve template extensions")
	}
	groups := map[string]PolicyGroup{}
	for _, g := range t.Groups {
		if !ValidID(g.ID) || reservedName(g.ID) || g.ID == "cn-domains" || g.ID == "cn-ips" {
			return errors.New("invalid policy group ID")
		}
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
		if t.Client == "sing-box" && (t.DNSPolicy.FakeIP || t.DNSPolicy.IPv6 || len(t.DNSPolicy.FakeIPFilter) > 0 || len(t.DNSPolicy.Local) > 1 || len(t.DNSPolicy.Remote) > 1 || len(t.DNSPolicy.Bootstrap) > 1) {
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
	for key, value := range base {
		result[key] = value
	}
	// Generated connection, routing and DNS sections remain authoritative.
	for key, value := range generated {
		result[key] = value
	}
	return result
}
