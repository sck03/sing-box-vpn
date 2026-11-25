package subscription

import (
	"testing"
	"time"
)

func TestScheduleDueTracksResourcesAccountsAndDeliveries(t *testing.T) {
	now := time.Now()
	config := DefaultConfig()
	config.Sources = []Source{{ID: "remote", Kind: "remote"}, {ID: "account", Kind: "account", AccountID: "a"}}
	config.Templates = []Template{{ID: "template", URL: "https://example.test"}}
	config.RuleSets = []RuleSet{{ID: "rules", URL: "https://example.test/rules"}}
	config.Deliveries = []Delivery{{ID: "manual"}, {ID: "auto", Automatic: true}}
	s := NewSchedule(config, Runtime{})
	later := Deadline{Next: now.Add(time.Hour)}
	s.Sources["remote"] = later
	s.Resources["template:template"], s.Resources["rules:rules"] = later, later
	s.Deliveries["auto"] = later
	s.Accounts = map[string]AccountSourceState{"a": {Revision: 1, Available: true}}
	accounts := map[string]AccountSourceState{"a": {Revision: 1, Available: true}}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if refresh, deliver := s.Due(now, accounts); refresh || deliver {
		t.Fatal("idle schedule queued work")
	}
	accounts["a"] = AccountSourceState{Revision: 1, Available: false}
	if refresh, deliver := s.Due(now, accounts); !refresh || deliver {
		t.Fatal("authorization change was ignored")
	}
	accounts["a"] = s.Accounts["a"]
	s.RefreshPending = true
	if refresh, _ := s.Due(now, accounts); !refresh {
		t.Fatal("pending refresh was lost")
	}
	s.RefreshPending = false
	for _, key := range []string{"template:template", "rules:rules"} {
		s.Resources[key] = Deadline{}
		if refresh, _ := s.Due(now, accounts); !refresh {
			t.Fatal("due resource ignored", key)
		}
		s.Resources[key] = later
	}
	if refresh, deliver := s.Due(now.Add(time.Hour), accounts); !refresh || !deliver {
		t.Fatal("deadline boundary ignored")
	}
}
