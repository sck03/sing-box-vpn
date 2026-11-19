package subscription

import (
	"errors"
	"fmt"
	"slices"

	"github.com/sck03/sing-box-vpn/internal/clientprofile"
)

type AccountExportPolicy struct {
	Template        string `json:"template"`
	SkipUnsupported bool   `json:"skip_unsupported"`
}

type AccountExportOverride struct {
	Account string `json:"account_id"`
	Client  string `json:"client"`
	AccountExportPolicy
}

func validateAccountExports(c Config) error {
	templates := map[string]Template{}
	for _, t := range c.Templates {
		templates[t.ID] = t
	}
	check := func(client string, p AccountExportPolicy) error {
		profile, ok := clientprofile.Lookup(client)
		t, exists := templates[p.Template]
		if !ok || !exists || t.Client != client || t.Mode == "rules" || !slices.Contains(profile.Modes, t.Mode) {
			return fmt.Errorf("invalid account template for %s", client)
		}
		return nil
	}
	for client, p := range c.AccountDefaults {
		if err := check(client, p); err != nil {
			return err
		}
	}
	if len(c.AccountOverrides) > 4096 {
		return errors.New("account template override limit exceeded")
	}
	seen := map[string]bool{}
	for _, override := range c.AccountOverrides {
		key := override.Account + ":" + override.Client
		if !ValidID(override.Account) || seen[key] {
			return errors.New("invalid or duplicate account template override")
		}
		seen[key] = true
		if err := check(override.Client, override.AccountExportPolicy); err != nil {
			return err
		}
	}
	return nil
}

// AccountTemplate is shared by availability, direct exports and public links.
// Effective templates include fetched rules and are supplied by the publisher.
func (c Config) AccountTemplate(account, client string, effective map[string]Template) (Template, bool, error) {
	p, ok := c.AccountDefaults[client]
	for _, override := range c.AccountOverrides {
		if override.Account == account && override.Client == client {
			p, ok = override.AccountExportPolicy, true
			break
		}
	}
	if !ok {
		profile, found := clientprofile.Lookup(client)
		if !found {
			return Template{}, false, errors.New("unknown account client")
		}
		return Template{ID: "account-" + client, Name: profile.Label, Client: client, Version: 1, Mode: profile.AccountMode, Preset: "proxy"}, true, nil
	}
	t, found := effective[p.Template]
	if !found || t.Client != client || t.Mode == "rules" {
		return Template{}, false, errors.New("account template unavailable")
	}
	return t, p.SkipUnsupported, nil
}

func EffectiveTemplates(c Config) map[string]Template {
	rules := map[string][]Rule{}
	for _, r := range c.RuleSets {
		rules[r.ID] = r.Rules
	}
	result := make(map[string]Template, len(c.Templates))
	for _, t := range c.Templates {
		t.Rules = append([]Rule(nil), t.Rules...)
		for _, id := range t.RuleSets {
			t.Rules = append(t.Rules, rules[id]...)
		}
		result[t.ID] = t
	}
	return result
}
