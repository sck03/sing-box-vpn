package application

import (
	"context"
	"errors"
	"testing"
	"time"
)

type historyFunc func(context.Context, time.Time) error

func (f historyFunc) PruneHistory(ctx context.Context, now time.Time) error { return f(ctx, now) }

func TestHistoryRetentionIsolatesDeadlinesAndReportsRecovery(t *testing.T) {
	var previous context.Context
	fail := true
	seen := 0
	store := historyFunc(func(ctx context.Context, now time.Time) error {
		if ctx.Err() != nil {
			t.Fatal("inherited an expired cleanup context")
		}
		if previous != nil && previous.Err() == nil {
			t.Fatal("previous context was not released")
		}
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 30*time.Second {
			t.Fatal("unbounded cleanup")
		}
		previous = ctx
		seen++
		if fail && seen == 1 {
			return context.DeadlineExceeded
		}
		return nil
	})
	retention := HistoryRetention{Accounts: store, Tasks: store, Certificates: store}
	reported := map[string]error{}
	report := func(name string, err error) { reported[name] = err }
	if !retention.Run(t.Context(), time.Now(), report) || seen != 3 || !errors.Is(reported["account-retention"], context.DeadlineExceeded) {
		t.Fatal(seen, reported)
	}
	fail = false
	if retention.Run(t.Context(), time.Now(), report) || reported["account-retention"] != nil {
		t.Fatal(reported)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	retention.Run(canceled, time.Now(), report)
	if seen != 6 {
		t.Fatal("shutdown started more cleanup", seen)
	}
}
