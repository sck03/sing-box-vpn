package subscription

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

type Source struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Kind            string `json:"kind"` // local, inline, remote
	Format          string `json:"format,omitempty"`
	URL             string `json:"url,omitempty"`
	Content         string `json:"content,omitempty"`
	Scope           string `json:"scope,omitempty"`
	AccountID       string `json:"account_id,omitempty"`
	Required        bool   `json:"required"`
	IntervalSeconds int    `json:"interval_seconds,omitempty"`
	MaxStaleSeconds int    `json:"max_stale_seconds,omitempty"`
}

type Collection struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Sources   []string  `json:"sources"`
	Processor Processor `json:"processor"`
}

type Rule struct {
	Type   string `json:"type"` // domain, domain_suffix, ip_cidr
	Value  string `json:"value"`
	Target string `json:"target"` // PROXY, VPNGate, DIRECT, REJECT
}

type RuleSet struct {
	ID    string `json:"id"`
	URL   string `json:"url,omitempty"` // remote JSON []Rule
	Rules []Rule `json:"rules,omitempty"`
}

type Template struct {
	ID             string   `json:"id"`
	Client         string   `json:"client"`
	Version        int      `json:"version"`
	MinimumVersion string   `json:"minimum_version,omitempty"`
	Mode           string   `json:"mode"`   // nodes, provider, config, rules
	Preset         string   `json:"preset"` // proxy, split
	DNS            []string `json:"dns,omitempty"`
	ProbeURL       string   `json:"probe_url,omitempty"`
	TUN            bool     `json:"tun,omitempty"`
	Rules          []Rule   `json:"rules,omitempty"`
	RuleSets       []string `json:"rule_sets,omitempty"`
	URL            string   `json:"url,omitempty"` // remote declarative JSON template
}

type Output struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Collection      string `json:"collection"`
	Template        string `json:"template"`
	SkipUnsupported bool   `json:"skip_unsupported,omitempty"`
}

type Grant struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	TokenHash    string    `json:"token_hash"`
	Outputs      []string  `json:"outputs"`
	ExpiresAt    time.Time `json:"expires_at,omitzero"`
	MaxDownloads uint64    `json:"max_downloads,omitempty"`
	Revoked      bool      `json:"revoked,omitempty"`
}

type Config struct {
	Version     int          `json:"version"`
	Revision    uint64       `json:"revision"`
	Sources     []Source     `json:"sources"`
	Collections []Collection `json:"collections"`
	Templates   []Template   `json:"templates"`
	RuleSets    []RuleSet    `json:"rule_sets"`
	Outputs     []Output     `json:"outputs"`
	Grants      []Grant      `json:"grants"`
	Webhook     string       `json:"webhook,omitempty"`
	Deliveries  []Delivery   `json:"deliveries,omitempty"`
}

type Delivery struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"` // http-put, webdav, github-gist
	Output    string `json:"output"`
	URL       string `json:"url"`
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
	Token     string `json:"token,omitempty"`
	Filename  string `json:"filename,omitempty"`
	Automatic bool   `json:"automatic,omitempty"`
}

var domainRulePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
var tokenHashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var gistPathPattern = regexp.MustCompile(`^/gists/[a-fA-F0-9]+$`)
var telegramTokenPattern = regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]+$`)

func HTTPURL(value string) error {
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Fragment != "" {
		return errors.New("URL must be HTTP(S), without credentials or fragment")
	}
	return nil
}

func (r Rule) Validate() error {
	if !slices.Contains([]string{"PROXY", "VPNGate", "DIRECT", "REJECT"}, r.Target) {
		return errors.New("unknown rule target")
	}
	if r.Value == "" || strings.ContainsAny(r.Value, ",\r\n\t =#") {
		return errors.New("invalid rule value")
	}
	switch r.Type {
	case "domain", "domain_suffix":
		if len(r.Value) > 253 || !domainRulePattern.MatchString(r.Value) {
			return errors.New("invalid domain rule")
		}
	case "ip_cidr":
		if _, err := netip.ParsePrefix(r.Value); err != nil {
			return err
		}
	default:
		return errors.New("unsupported rule type")
	}
	return nil
}

func (t Template) Validate() error {
	cap, ok := Clients[t.Client]
	if !ok {
		return fmt.Errorf("unknown client %q", t.Client)
	}
	if t.Version != 1 || !slices.Contains(cap.Modes, t.Mode) {
		return errors.New("unsupported template version or output mode")
	}
	if t.Preset != "" && t.Preset != "proxy" && t.Preset != "split" {
		return errors.New("unknown template preset")
	}
	if t.TUN && !slices.Contains([]string{"sing-box", "mihomo", "openclash"}, t.Client) {
		return errors.New("TUN parameter is not supported by this client template")
	}
	if t.Client == "sing-box" && len(t.DNS) > 1 {
		return errors.New("sing-box template accepts one DNS resolver")
	}
	if t.Mode != "config" && (len(t.DNS) > 0 || t.ProbeURL != "" || t.TUN) {
		return errors.New("DNS, probe and TUN settings require a complete config output")
	}
	if len(t.DNS) > 0 && t.Client == "egern" {
		return errors.New("Egern template uses client-managed DNS")
	}
	if t.ProbeURL != "" && !slices.Contains([]string{"mihomo", "openclash", "stash"}, t.Client) {
		return errors.New("automatic probe URL is unsupported by this template")
	}
	if t.Mode != "config" && t.Mode != "rules" && (len(t.Rules) > 0 || len(t.RuleSets) > 0 || t.TUN) {
		return errors.New("node/provider output cannot contain routing or TUN settings")
	}
	if len(t.Rules) > 4096 || len(t.RuleSets) > MaxObjects {
		return errors.New("too many template rules")
	}
	for _, rule := range t.Rules {
		if err := rule.Validate(); err != nil {
			return err
		}
	}
	if t.ProbeURL != "" {
		if err := HTTPURL(t.ProbeURL); err != nil {
			return err
		}
	}
	if t.URL != "" {
		if err := HTTPURL(t.URL); err != nil {
			return err
		}
	}
	for _, dns := range t.DNS {
		if _, err := netip.ParseAddr(dns); err != nil {
			return errors.New("template DNS must be an IP address")
		}
	}
	return nil
}

func (c Config) Validate() error {
	if c.Version != SchemaVersion {
		return errors.New("unsupported subscription schema; use a new configuration")
	}
	sources, collections, templates, outputs, rules := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	add := func(set map[string]bool, id string) error {
		if !ValidID(id) || set[id] {
			return fmt.Errorf("invalid or duplicate ID %q", id)
		}
		set[id] = true
		if len(set) > MaxObjects {
			return errors.New("object limit exceeded")
		}
		return nil
	}
	contentBytes, ruleCount := 0, 0
	for _, s := range c.Sources {
		contentBytes += len(s.Content)
		if contentBytes > MaxBody {
			return errors.New("combined inline source size limit exceeded")
		}
		if err := add(sources, s.ID); err != nil {
			return err
		}
		switch s.Kind {
		case "local":
			if s.Scope != "normal" && s.Scope != "vpngate" {
				return errors.New("unknown local scope")
			}
		case "account":
			if !ValidID(s.AccountID) || (s.Scope != "normal" && s.Scope != "vpngate" && s.Scope != "") {
				return errors.New("account source requires an account ID and valid scope")
			}
		case "inline", "remote":
			if !slices.Contains([]string{"uri", "base64", "mihomo", "sing-box", "nodes"}, s.Format) {
				return errors.New("unsupported input format")
			}
			if s.Kind == "remote" {
				if err := HTTPURL(s.URL); err != nil {
					return err
				}
			}
		default:
			return errors.New("unknown source kind")
		}
		if len(s.Content) > MaxBody || s.IntervalSeconds < 0 || (s.IntervalSeconds > 0 && s.IntervalSeconds < 60) || s.MaxStaleSeconds < 0 || s.MaxStaleSeconds > 30*86400 {
			return errors.New("invalid source size, refresh interval or stale lifetime")
		}
	}
	for _, r := range c.RuleSets {
		ruleCount += len(r.Rules)
		if ruleCount > 4096 {
			return errors.New("combined rule limit exceeded")
		}
		if err := add(rules, r.ID); err != nil {
			return err
		}
		for _, rule := range r.Rules {
			if err := rule.Validate(); err != nil {
				return err
			}
		}
		if r.URL != "" {
			if err := HTTPURL(r.URL); err != nil {
				return err
			}
		}
	}
	for _, set := range c.Collections {
		if err := add(collections, set.ID); err != nil {
			return err
		}
		if len(set.Sources) == 0 || len(set.Sources) > MaxObjects {
			return errors.New("source selection must contain 1 to 128 distinct references")
		}
		seen := make(map[string]bool, len(set.Sources))
		for _, id := range set.Sources {
			if seen[id] {
				return fmt.Errorf("collection %s repeats source %s", set.ID, id)
			}
			seen[id] = true
			if !sources[id] {
				return fmt.Errorf("unknown source %s", id)
			}
		}
		if _, err := Process(nil, set.Processor); err != nil {
			return err
		}
	}
	expandedRuleCount := 0
	for _, t := range c.Templates {
		ruleCount += len(t.Rules)
		if ruleCount > 4096 {
			return errors.New("combined rule limit exceeded")
		}
		if err := add(templates, t.ID); err != nil {
			return err
		}
		if err := t.Validate(); err != nil {
			return err
		}
		seen := make(map[string]bool, len(t.RuleSets))
		expandedRules := len(t.Rules)
		for _, id := range t.RuleSets {
			if seen[id] {
				return fmt.Errorf("template %s repeats rule set %s", t.ID, id)
			}
			seen[id] = true
			if !rules[id] {
				return fmt.Errorf("unknown rule set %s", id)
			}
			for _, r := range c.RuleSets {
				if r.ID == id {
					expandedRules += len(r.Rules)
				}
			}
		}
		if expandedRules > 4096 {
			return errors.New("expanded template rule limit exceeded")
		}
		expandedRuleCount += expandedRules
		if expandedRuleCount > 16384 {
			return errors.New("combined expanded rule budget exceeded")
		}
	}
	for _, o := range c.Outputs {
		if err := add(outputs, o.ID); err != nil {
			return err
		}
		if !collections[o.Collection] || !templates[o.Template] {
			return fmt.Errorf("output %s has a missing collection or template", o.ID)
		}
	}
	grants, tokens := map[string]bool{}, map[string]bool{}
	for _, g := range c.Grants {
		if err := add(grants, g.ID); err != nil {
			return err
		}
		if !tokenHashPattern.MatchString(g.TokenHash) || tokens[g.TokenHash] {
			return errors.New("invalid or duplicate grant token hash")
		}
		tokens[g.TokenHash] = true
		if len(g.Outputs) == 0 {
			return errors.New("empty grant")
		}
		for _, id := range g.Outputs {
			if !g.Revoked && !outputs[id] {
				return fmt.Errorf("grant references unknown output %s", id)
			}
		}
	}
	if c.Webhook != "" {
		if err := HTTPURL(c.Webhook); err != nil {
			return err
		}
	}
	deliveries := map[string]bool{}
	for _, d := range c.Deliveries {
		if len(c.Deliveries) > 16 {
			return errors.New("at most 16 delivery destinations are supported")
		}
		if err := add(deliveries, d.ID); err != nil {
			return err
		}
		if !outputs[d.Output] {
			return errors.New("delivery references unknown output")
		}
		if err := HTTPURL(d.URL); err != nil {
			return err
		}
		if !slices.Contains([]string{"http-put", "webdav", "github-gist", "telegram", "gitlab"}, d.Kind) {
			return errors.New("unsupported delivery kind")
		}
		if strings.ContainsAny(d.Username+d.Token, "\r\n") || len(d.Token)+len(d.Password) > 8192 {
			return errors.New("invalid delivery credentials")
		}
		if d.Kind == "github-gist" {
			u, _ := url.Parse(d.URL)
			if u.Scheme != "https" || u.Host != "api.github.com" || !gistPathPattern.MatchString(u.Path) || u.RawQuery != "" || d.Token == "" || d.Filename == "" || strings.ContainsAny(d.Filename, "/\\\r\n") {
				return errors.New("GitHub delivery requires an existing Gist API URL, token and filename")
			}
		}
		if d.Kind == "telegram" {
			if d.URL != "https://api.telegram.org" || d.Username == "" || d.Token == "" || !telegramTokenPattern.MatchString(d.Token) {
				return errors.New("Telegram requires api.telegram.org, chat ID and a bot token")
			}
		}
		if d.Kind == "gitlab" {
			u, _ := url.Parse(d.URL)
			if u.Scheme != "https" || !strings.Contains(u.Path, "/api/v4/projects/") || u.RawQuery != "" || d.Token == "" || d.Filename == "" || strings.ContainsAny(d.Filename, "\\\r\n") || strings.HasPrefix(d.Filename, "/") || strings.Contains(d.Filename, "..") {
				return errors.New("GitLab requires a private project API URL, token and relative filename")
			}
		}
	}
	return nil
}
