package manage

import (
	"context"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

// Completed source fetches survive task cancellation. This never publishes an
// artifact or an unvalidated template/rules update, and rejects stale revisions.
func checkpointSubscriptionSources(ctx context.Context, root string, expected sub.Config, sources map[string]sub.SourceCache) error {
	if len(sources) == 0 {
		return nil
	}
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return withStartupLock(finish, root, func() error {
		config, state, err := loadSubscriptionState(root)
		if err != nil {
			return err
		}
		if err = validateSubscriptionsRevision(config, expected); err != nil {
			return err
		}
		state = sub.PruneRuntime(config, state)
		if state.Sources == nil {
			state.Sources = map[string]sub.SourceCache{}
		}
		for id, cache := range sources {
			state.Sources[id] = cache
		}
		// Completed fetches can have a future Next even though no artifact was
		// published. Schedule validation again without downloading them again.
		state.RefreshPending = true
		files := fileBundle{}
		if err = files.json(subscriptionsRuntime, state); err != nil {
			return err
		}
		if err = files.json(subscriptionsSchedule, subscriptionDeadlines(config, state)); err != nil {
			return err
		}
		return publish(root, files)
	})
}
