package accounting

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// Low-level ledger fixtures bypass runtime authentication only inside this package.
func (s *Store) Reserve(ctx context.Context, id, account, runtime string, want int64, now time.Time) (int64, error) {
	u, err := s.Advance(ctx, account, now, false)
	if err != nil {
		return 0, err
	}
	return s.reservePeriod(ctx, fixtureLeaseID(id), account, runtime, want, now, u.PeriodID)
}

// Named fixtures use the same scoped ID format as the production meter.
func fixtureLeaseID(name string) string {
	if strings.Contains(name, "/") {
		return name
	}
	hash := sha256.Sum256([]byte(name))
	return hex.EncodeToString(hash[:16]) + "/1"
}

func (s *Store) Checkpoint(ctx context.Context, id string, upload, download int64, closeLease bool) error {
	return s.CheckpointBatch(ctx, []UsageCheckpoint{{fixtureLeaseID(id), upload, download, closeLease}})
}

func (s *Store) Advance(ctx context.Context, id string, now time.Time, activate bool) (Usage, error) {
	u, err := s.Account(ctx, id)
	if err != nil {
		return u, err
	}
	return s.advanceUsage(ctx, u, now, activate)
}
