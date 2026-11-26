package manage

import (
	"context"
	"path/filepath"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

const subscriptionsSchedule = "subscriptions/schedule.json"

// A missing projection is reconstructed from the current configuration and
// deadline fields only; idle scheduling never decodes cached node bodies.
func loadSubscriptionSchedule(root string) (sub.Schedule, error) {
	var schedule sub.Schedule
	if found, err := ReadJSON(filepath.Join(root, subscriptionsSchedule), &schedule); err != nil {
		return schedule, err
	} else if found {
		return schedule, schedule.Validate()
	}
	var config sub.Config
	if found, err := ReadJSON(filepath.Join(root, subscriptionsConfig), &config); err != nil {
		return schedule, err
	} else if !found {
		config = sub.DefaultConfig()
	}
	if err := config.Validate(); err != nil {
		return schedule, err
	}
	if _, err := ReadJSON(filepath.Join(root, subscriptionsRuntime), &schedule); err != nil {
		return schedule, err
	}
	operations, err := loadSubscriptionOperations(root)
	schedule.Config = sub.NewSchedule(config, sub.Runtime{}).Config
	schedule.Deliveries = sub.NewSchedule(config, sub.Runtime{Deliveries: operations.Deliveries}).Deliveries
	return schedule, err
}

func (s *PlatformService) scheduleSubscriptions(ctx context.Context) error {
	schedule, err := loadSubscriptionSchedule(s.Root)
	if err != nil {
		return err
	}
	now := time.Now()
	accounts := map[string]sub.AccountSourceState{}
	for _, source := range schedule.Config.Sources {
		if _, checked := accounts[source.AccountID]; source.Kind != "account" || checked {
			continue
		}
		_, state, err := accountSourceState(ctx, s.Accounts, source.AccountID, now)
		if err != nil {
			return err
		}
		accounts[source.AccountID] = state
	}
	refresh, deliver := schedule.Due(now, accounts)
	if refresh {
		if _, err = s.queueAutomaticSubscription(ctx, "refresh"); err != nil {
			return err
		}
	}
	if deliver {
		_, err = s.queueAutomaticSubscription(ctx, "deliver")
	}
	return err
}
