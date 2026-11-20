package manage

import (
	"context"
	"errors"
	"time"
)

// History reclamation owns a separate bounded worker. Slow SQLite maintenance
// must not delay claiming interactive tasks; certificate deletion keeps its
// existing configuration and publication locks.
func (s *PlatformService) runRetention(ctx context.Context) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-timer.C:
			maintenance, stop := context.WithTimeout(ctx, 30*time.Second)
			err := errors.Join(s.Accounts.PruneHistory(maintenance, now), s.Store.PruneHistory(maintenance, now), s.pruneCertificates(maintenance, now))
			if err != nil {
				_ = s.Auth.Audit(maintenance, "scheduler", "history-retention", "", "failed")
			}
			stop()
			timer.Reset(time.Hour)
		}
	}
}
