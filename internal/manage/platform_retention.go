package manage

import (
	"context"
	"time"

	"github.com/sck03/sing-box-vpn/internal/application"
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
			retry := s.retainHistory(ctx, now)
			timer.Reset(retry)
		}
	}
}

type certificateHistory struct{ service *PlatformService }

func (c certificateHistory) PruneHistory(ctx context.Context, now time.Time) error {
	return c.service.pruneCertificates(ctx, now)
}

func (s *PlatformService) retainHistory(ctx context.Context, now time.Time) time.Duration {
	retention := application.HistoryRetention{Accounts: s.Accounts, Tasks: s.Store, Certificates: certificateHistory{s}}
	if retention.Run(ctx, now, func(operation string, err error) { s.schedulerResult(ctx, operation, err) }) {
		return 5 * time.Minute
	}
	return time.Hour
}
