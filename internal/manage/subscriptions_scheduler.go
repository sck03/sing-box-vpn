package manage

import (
	"context"
	"io"
	"path/filepath"
	"sync"
	"time"
)

// Each class has one worker and an overall deadline. A blocked destination
// cannot postpone cache expiry/rebuilding, and no tick queues overlapping work.
func runSubscriptionScheduler(ctx context.Context, root string) {
	runScheduledTasks(ctx, time.Minute, 2*time.Minute,
		func(ctx context.Context) { _ = refreshSubscriptions(ctx, root, filepath.Join(root, "sing-box"), false) },
		func(ctx context.Context) { _ = deliverSubscriptions(ctx, root, "", true, io.Discard) },
	)
}

func runScheduledTasks(ctx context.Context, interval, timeout time.Duration, tasks ...func(context.Context)) {
	var workers sync.WaitGroup
	for _, task := range tasks {
		workers.Go(func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				if ctx.Err() != nil {
					return
				}
				child, cancel := context.WithTimeout(ctx, timeout)
				task(child)
				cancel()
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		})
	}
	workers.Wait()
}
