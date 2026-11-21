package manage

import (
	"context"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
	"path/filepath"
)

// Capture a coherent generation, including interrupted publication recovery.
// The caller owns these values and performs remote I/O after releasing the lock.
func captureSubscriptionDelivery(ctx context.Context, root string) (config sub.Config, state sub.Runtime, snapshot sub.Snapshot, err error) {
	err = withStartupLock(ctx, root, func() error {
		var err error
		config, state, err = loadSubscriptionState(root)
		if err != nil {
			return err
		}
		_, err = ReadJSON(filepath.Join(root, subscriptionsSnapshot), &snapshot)
		return err
	})
	return
}
