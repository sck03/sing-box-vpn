package manage

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

type scheduleDeadline struct {
	Next time.Time `json:"next"`
}
type scheduledSource struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	AccountID string `json:"account_id,omitempty"`
}
type scheduledResource struct {
	ID     string `json:"id"`
	Remote bool   `json:"remote"`
}
type scheduledDelivery struct {
	ID        string `json:"id"`
	Automatic bool   `json:"automatic"`
}
type scheduleConfig struct {
	Version    int                 `json:"version"`
	Revision   uint64              `json:"revision"`
	Sources    []scheduledSource   `json:"sources"`
	Templates  []scheduledResource `json:"templates"`
	RuleSets   []scheduledResource `json:"rule_sets"`
	Deliveries []scheduledDelivery `json:"deliveries"`
}
type subscriptionSchedule struct {
	RefreshPending bool                              `json:"refresh_pending,omitempty"`
	Config         scheduleConfig                    `json:"config"`
	Sources        map[string]scheduleDeadline       `json:"sources"`
	Resources      map[string]scheduleDeadline       `json:"resources"`
	Deliveries     map[string]scheduleDeadline       `json:"deliveries"`
	Accounts       map[string]sub.AccountSourceState `json:"accounts"`
}

const subscriptionsSchedule = "subscriptions/schedule.json"

func subscriptionDeadlines(c sub.Config, r sub.Runtime) subscriptionSchedule {
	s := subscriptionSchedule{Config: scheduleConfig{Version: c.Version, Revision: c.Revision}, Sources: map[string]scheduleDeadline{}, Resources: map[string]scheduleDeadline{}, Deliveries: map[string]scheduleDeadline{}, Accounts: r.Accounts}
	s.RefreshPending = r.RefreshPending
	for _, source := range c.Sources {
		s.Config.Sources = append(s.Config.Sources, scheduledSource{ID: source.ID, Kind: source.Kind, AccountID: source.AccountID})
		s.Sources[source.ID] = scheduleDeadline{Next: r.Sources[source.ID].Next}
	}
	for _, template := range c.Templates {
		s.Config.Templates = append(s.Config.Templates, scheduledResource{ID: template.ID, Remote: template.URL != ""})
	}
	for _, rules := range c.RuleSets {
		s.Config.RuleSets = append(s.Config.RuleSets, scheduledResource{ID: rules.ID, Remote: rules.URL != ""})
	}
	for id, resource := range r.Resources {
		s.Resources[id] = scheduleDeadline{Next: resource.Next}
	}
	for _, delivery := range c.Deliveries {
		s.Config.Deliveries = append(s.Config.Deliveries, scheduledDelivery{ID: delivery.ID, Automatic: delivery.Automatic})
		s.Deliveries[delivery.ID] = scheduleDeadline{Next: r.Deliveries[delivery.ID].Next}
	}
	return s
}

// Decode only deadlines and account revisions. Cached nodes, resource bodies
// and local projections never allocate business objects during idle scheduling.
func loadSubscriptionSchedule(root string) (scheduleConfig, subscriptionSchedule, error) {
	var config sub.Config
	var metadata scheduleConfig
	var schedule subscriptionSchedule
	if found, err := ReadJSON(filepath.Join(root, subscriptionsSchedule), &schedule); err != nil {
		return metadata, schedule, err
	} else if found {
		if schedule.Config.Version != sub.SchemaVersion || len(schedule.Config.Sources) > sub.MaxObjects || len(schedule.Config.Templates) > sub.MaxObjects {
			return metadata, schedule, errors.New("invalid subscription schedule")
		}
		return schedule.Config, schedule, nil
	}
	if found, err := ReadJSON(filepath.Join(root, subscriptionsConfig), &config); err != nil {
		return metadata, schedule, err
	} else if !found {
		config = sub.DefaultConfig()
	}
	if err := config.Validate(); err != nil {
		return metadata, schedule, err
	}
	// A missing derived projection is recoverable; it is never an authority.
	_, err := ReadJSON(filepath.Join(root, subscriptionsRuntime), &schedule)
	return subscriptionDeadlines(config, sub.Runtime{}).Config, schedule, err
}

// Only queue due work. Empty installations and backoff windows need no writes.
func (s *PlatformService) scheduleSubscriptions(ctx context.Context) error {
	c, r, err := loadSubscriptionSchedule(s.Root)
	if err != nil {
		return err
	}
	now := time.Now()
	refresh := r.RefreshPending
	checked := map[string]bool{}
	for _, source := range c.Sources {
		if source.Kind == "remote" && !now.Before(r.Sources[source.ID].Next) {
			refresh = true
		}
		if source.Kind == "account" && !checked[source.AccountID] {
			checked[source.AccountID] = true
			_, state, err := accountSourceState(ctx, s.Accounts, source.AccountID, now)
			if err != nil {
				return err
			}
			previous, found := r.Accounts[source.AccountID]
			refresh = refresh || !found || state != previous
		}
	}
	for _, t := range c.Templates {
		if t.Remote && !now.Before(r.Resources["template:"+t.ID].Next) {
			refresh = true
		}
	}
	for _, rules := range c.RuleSets {
		if rules.Remote && !now.Before(r.Resources["rules:"+rules.ID].Next) {
			refresh = true
		}
	}
	if refresh {
		if _, err = s.queueAutomaticSubscription(ctx, "refresh"); err != nil {
			return err
		}
	}
	for _, d := range c.Deliveries {
		if d.Automatic && !now.Before(r.Deliveries[d.ID].Next) {
			_, err = s.queueAutomaticSubscription(ctx, "deliver")
			return err
		}
	}
	return nil
}
