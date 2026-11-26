package manage

import (
	"errors"
	"path/filepath"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

const subscriptionsOperations = "subscriptions/operations.json"

// Delivery acknowledgements and run history change independently of cached
// nodes and resource bodies. All files still commit through the publication log.
type subscriptionOperationsState struct {
	Deliveries map[string]sub.DeliveryState `json:"deliveries"`
	Runs       []sub.Run                    `json:"runs"`
}

func loadSubscriptionOperations(root string) (subscriptionOperationsState, error) {
	var state subscriptionOperationsState
	_, err := ReadJSON(filepath.Join(root, subscriptionsOperations), &state)
	return state, err
}

func (files fileBundle) subscriptionJSON(name string, value any) error {
	if err := files.json(name, value); err != nil {
		return err
	}
	if len(files[name]) > maxJSONFile {
		return errors.New("subscription state exceeds 16 MiB")
	}
	return nil
}

// The caller holds the publication lock. Preserve source deadlines/activity
// without loading or rewriting the potentially large input cache.
func writeSubscriptionOperations(root string, config sub.Config, state sub.Runtime) error {
	var schedule sub.Schedule
	var activity subscriptionActivityProjection
	if found, err := ReadJSON(filepath.Join(root, subscriptionsSchedule), &schedule); err != nil {
		return err
	} else if !found {
		return errors.New("subscription schedule is unavailable")
	}
	if found, err := ReadJSON(filepath.Join(root, subscriptionsActivity), &activity); err != nil {
		return err
	} else if !found {
		return errors.New("subscription activity is unavailable")
	}
	if schedule.Config.Revision != config.Revision || activity.Revision != config.Revision {
		latest, err := loadSubscriptionRuntime(root)
		if err != nil {
			return err
		}
		latest.Deliveries, latest.Runs = state.Deliveries, state.Runs
		return writeSubscriptionRuntime(root, config, latest)
	}
	schedule.Deliveries = sub.NewSchedule(config, state).Deliveries
	activity.Deliveries = subscriptionActivityStatus(config, state).Deliveries
	files := fileBundle{}
	for name, value := range map[string]any{subscriptionsOperations: subscriptionOperationsState{state.Deliveries, state.Runs}, subscriptionsSchedule: schedule, subscriptionsActivity: activity} {
		if err := files.subscriptionJSON(name, value); err != nil {
			return err
		}
	}
	return publish(root, files)
}
