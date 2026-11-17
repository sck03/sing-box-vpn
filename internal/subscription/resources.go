package subscription

import (
	"errors"
	strict "github.com/sck03/sing-box-vpn/internal/encoding"
	"maps"
)

// Resource bodies are runtime cache, never edits to the configured fallback.
// A changed URL invalidates both the body and its conditional request validator.
const MaxResourceBytes = MaxBody / 2

// PruneRuntime bounds caches by the current configuration and invalidates URLs
// together with their ETags. Clone maps so preview never mutates its input.
func PruneRuntime(c Config, r Runtime) Runtime {
	sources, resources, deliveries := map[string]bool{}, map[string]string{}, map[string]bool{}
	accounts := map[string]bool{}
	for _, s := range c.Sources {
		sources[s.ID] = r.Sources[s.ID].SourceHash == SourceHash(s)
		if s.Kind == "account" {
			accounts[s.AccountID] = true
		}
	}
	for _, t := range c.Templates {
		if t.URL != "" {
			resources["template:"+t.ID] = t.URL
		}
	}
	for _, rule := range c.RuleSets {
		if rule.URL != "" {
			resources["rules:"+rule.ID] = rule.URL
		}
	}
	for _, d := range c.Deliveries {
		deliveries[d.ID] = true
	}
	r.Sources, r.Resources, r.Deliveries = maps.Clone(r.Sources), maps.Clone(r.Resources), maps.Clone(r.Deliveries)
	r.Accounts = maps.Clone(r.Accounts)
	for id := range r.Accounts {
		if !accounts[id] {
			delete(r.Accounts, id)
		}
	}
	for id := range r.Sources {
		if !sources[id] {
			delete(r.Sources, id)
		}
	}
	for id, entry := range r.Resources {
		if resources[id] == "" || resources[id] != entry.URL {
			delete(r.Resources, id)
		}
	}
	for id := range r.Deliveries {
		if !deliveries[id] {
			delete(r.Deliveries, id)
		}
	}
	return r
}

func ResolveResources(c Config, cache map[string]ResourceCache) (Config, error) {
	c.Templates = append([]Template(nil), c.Templates...)
	c.RuleSets = append([]RuleSet(nil), c.RuleSets...)
	total := 0
	for i, t := range c.Templates {
		r := cache["template:"+t.ID]
		if t.URL == "" || r.URL != t.URL || len(r.Body) == 0 {
			continue
		}
		total += len(r.Body)
		var updated Template
		if err := strict.JSON(r.Body, &updated, MaxResourceBytes); err != nil {
			return c, err
		}
		if updated.ID != t.ID || updated.Client != t.Client {
			return c, errors.New("remote template cannot change ID or client")
		}
		updated.URL = t.URL
		if err := updated.Validate(); err != nil {
			return c, err
		}
		c.Templates[i] = updated
	}
	for i, r := range c.RuleSets {
		entry := cache["rules:"+r.ID]
		if r.URL == "" || entry.URL != r.URL || len(entry.Body) == 0 {
			continue
		}
		total += len(entry.Body)
		var rules []Rule
		if err := strict.JSON(entry.Body, &rules, MaxResourceBytes); err != nil {
			return c, err
		}
		if len(rules) > 4096 {
			return c, errors.New("rule limit exceeded")
		}
		for _, rule := range rules {
			if err := rule.Validate(); err != nil {
				return c, err
			}
		}
		c.RuleSets[i].Rules = rules
	}
	if total > MaxResourceBytes {
		return c, errors.New("combined resource cache limit exceeded")
	}
	return c, nil
}
