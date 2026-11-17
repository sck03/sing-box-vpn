package manage

import (
	"context"
	"time"

	"github.com/sck03/sing-box-vpn/internal/application"
)

// Only queue due work. Empty installations and backoff windows need no writes.
func (s *PlatformService) scheduleSubscriptions(ctx context.Context) error {
	c, r, _, err := loadSubscriptions(s.Root)
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
