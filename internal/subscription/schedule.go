package subscription

import (
	"errors"
	"time"
)

type Deadline struct {
	Next time.Time `json:"next"`
}
type ScheduledSource struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	AccountID string `json:"account_id,omitempty"`
}
type ScheduledResource struct {
	ID     string `json:"id"`
	Remote bool   `json:"remote"`
}
type ScheduledDelivery struct {
	ID        string `json:"id"`
	Automatic bool   `json:"automatic"`
}
type ScheduleConfig struct {
	Version    int                 `json:"version"`
	Revision   uint64              `json:"revision"`
	Sources    []ScheduledSource   `json:"sources"`
	Templates  []ScheduledResource `json:"templates"`
	RuleSets   []ScheduledResource `json:"rule_sets"`
	Deliveries []ScheduledDelivery `json:"deliveries"`
}

// Schedule is a disposable projection of deadlines, without cached nodes or
// resource bodies. Its policy is independent of files, SQL and task execution.
type Schedule struct {
	RefreshPending bool                          `json:"refresh_pending,omitempty"`
	Config         ScheduleConfig                `json:"config"`
	Sources        map[string]Deadline           `json:"sources"`
	Resources      map[string]Deadline           `json:"resources"`
	Deliveries     map[string]Deadline           `json:"deliveries"`
	Accounts       map[string]AccountSourceState `json:"accounts"`
}

func NewSchedule(c Config, r Runtime) Schedule {
	s := Schedule{RefreshPending: r.RefreshPending, Config: ScheduleConfig{Version: c.Version, Revision: c.Revision}, Sources: map[string]Deadline{}, Resources: map[string]Deadline{}, Deliveries: map[string]Deadline{}, Accounts: r.Accounts}
	for _, source := range c.Sources {
		s.Config.Sources = append(s.Config.Sources, ScheduledSource{ID: source.ID, Kind: source.Kind, AccountID: source.AccountID})
		s.Sources[source.ID] = Deadline{Next: r.Sources[source.ID].Next}
	}
	for _, template := range c.Templates {
		s.Config.Templates = append(s.Config.Templates, ScheduledResource{ID: template.ID, Remote: template.URL != ""})
	}
	for _, rules := range c.RuleSets {
		s.Config.RuleSets = append(s.Config.RuleSets, ScheduledResource{ID: rules.ID, Remote: rules.URL != ""})
	}
	for id, resource := range r.Resources {
		s.Resources[id] = Deadline{Next: resource.Next}
	}
	for _, delivery := range c.Deliveries {
		s.Config.Deliveries = append(s.Config.Deliveries, ScheduledDelivery{ID: delivery.ID, Automatic: delivery.Automatic})
		s.Deliveries[delivery.ID] = Deadline{Next: r.Deliveries[delivery.ID].Next}
	}
	return s
}

func (s Schedule) Validate() error {
	c := s.Config
	if c.Version != SchemaVersion || len(c.Sources) > MaxObjects || len(c.Templates) > MaxObjects || len(c.RuleSets) > MaxObjects || len(c.Deliveries) > MaxObjects {
		return errors.New("invalid subscription schedule")
	}
	return nil
}

func (s Schedule) Due(now time.Time, accounts map[string]AccountSourceState) (refresh, deliver bool) {
	refresh = s.RefreshPending
	for _, source := range s.Config.Sources {
		if source.Kind == "remote" && !now.Before(s.Sources[source.ID].Next) {
			refresh = true
		}
		if source.Kind == "account" {
			previous, found := s.Accounts[source.AccountID]
			current, available := accounts[source.AccountID]
			refresh = refresh || !found || !available || current != previous
		}
	}
	for _, template := range s.Config.Templates {
		if template.Remote && !now.Before(s.Resources["template:"+template.ID].Next) {
			refresh = true
		}
	}
	for _, rules := range s.Config.RuleSets {
		if rules.Remote && !now.Before(s.Resources["rules:"+rules.ID].Next) {
			refresh = true
		}
	}
	for _, delivery := range s.Config.Deliveries {
		if delivery.Automatic && !now.Before(s.Deliveries[delivery.ID].Next) {
			deliver = true
			break
		}
	}
	return
}
