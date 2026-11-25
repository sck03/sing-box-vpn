package accounting

import (
	"context"
	"time"
)

// Low-level ledger fixtures bypass runtime authentication only inside this package.
func (s *Store) Reserve(ctx context.Context, id, account, runtime string, want int64, now time.Time) (int64, error) {
	u, err := s.Advance(ctx, account, now, false)
	if err != nil {
		return 0, err
	}
	return s.reservePeriod(ctx, id, account, runtime, want, now, u.PeriodID)
}

func (s *Store) Advance(ctx context.Context, id string, now time.Time, activate bool) (Usage, error) {
	u, err := s.Account(ctx, id)
	if err != nil {
		return u, err
	}
	return s.advanceUsage(ctx, u, now, activate)
}
