package manage

import (
	"context"
	"path/filepath"
	"time"

	"github.com/sck03/sing-box-vpn/internal/application"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

type scheduleDeadline struct {
	Next time.Time `json:"next"`
}
type subscriptionSchedule struct {
	Sources    map[string]scheduleDeadline       `json:"sources"`
	Resources  map[string]scheduleDeadline       `json:"resources"`
	Deliveries map[string]scheduleDeadline       `json:"deliveries"`
	Accounts   map[string]sub.AccountSourceState `json:"accounts"`
}

// Decode only deadlines and account revisions. Cached nodes, resource bodies
// and local projections never allocate business objects during idle scheduling.
func loadSubscriptionSchedule(root string) (sub.Config, subscriptionSchedule, error) {
	var config sub.Config
	var schedule subscriptionSchedule
	if found, err := ReadJSON(filepath.Join(root, subscriptionsConfig), &config); err != nil {
		return config, schedule, err
	} else if !found {
		config = sub.DefaultConfig()
	}
	if err := config.Validate(); err != nil {
		return config, schedule, err
	}
	_, err := ReadJSON(filepath.Join(root, subscriptionsRuntime), &schedule)
	return config, schedule, err
}

// Only queue due work. Empty installations and backoff windows need no writes.
func (s *PlatformService) scheduleSubscriptions(ctx context.Context) error {
	c, r, err := loadSubscriptionSchedule(s.Root)
	if err != nil {
		return err
	}
	now := time.Now()
	refresh := false
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
		if t.URL != "" && !now.Before(r.Resources["template:"+t.ID].Next) {
			refresh = true
		}
	}
	for _, rules := range c.RuleSets {
		if rules.URL != "" && !now.Before(r.Resources["rules:"+rules.ID].Next) {
			refresh = true
		}
	}
	if refresh {
		if _, err = s.Queue(ctx, PlatformAction{Operation: "subscription", Automatic: true, Subscription: application.SubscriptionCommand{Operation: "refresh"}}, "scheduler"); err != nil {
			return err
		}
	}
	for _, d := range c.Deliveries {
		if d.Automatic && !now.Before(r.Deliveries[d.ID].Next) {
			_, err = s.Queue(ctx, PlatformAction{Operation: "subscription", Automatic: true, Subscription: application.SubscriptionCommand{Operation: "deliver"}}, "scheduler")
			return err
		}
	}
	return nil
}
