package manage

import (
	"context"
	"errors"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
	"path/filepath"
)

// Capture a coherent generation, including interrupted publication recovery.
// The caller owns these values and performs remote I/O after releasing the lock.
func captureSubscriptionDelivery(ctx context.Context, root string, services ...AccountService) (config sub.Config, state sub.Runtime, snapshot sub.Snapshot, err error) {
	err = withStartupLock(ctx, root, func() error {
		var err error
		if found, err := ReadJSON(filepath.Join(root, subscriptionsConfig), &config); err != nil {
			return err
		} else if !found {
			return errors.New("subscription configuration is missing")
		}
		operations, err := loadSubscriptionOperations(root)
		if err != nil {
			return err
		}
		state.Deliveries, state.Runs = operations.Deliveries, operations.Runs
		if _, _, err = confirmedSubscriptionConfig(ctx, root, config, services...); err != nil {
			return err
		}
		_, err = ReadJSON(filepath.Join(root, subscriptionsSnapshot), &snapshot)
		if err == nil && (snapshot.Version != sub.SchemaVersion || snapshot.Revision != config.Revision) {
			return errors.New("subscription publication does not match the current configuration")
		}
		return err
	})
	return
}
