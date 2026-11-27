package application

import (
	"context"
	"time"
)

// HistoryRepository owns its domain's reclamation rules and transactions.
// The use case owns independent deadlines, not HTTP, storage or logging details.
type HistoryRepository interface {
	PruneHistory(context.Context, time.Time) error
}

type HistoryRetention struct {
	Accounts, Tasks, Certificates HistoryRepository
}

func (r HistoryRetention) Run(ctx context.Context, now time.Time, report func(string, error)) (failed bool) {
	for _, job := range []struct {
		name  string
		store HistoryRepository
	}{
		{"account-retention", r.Accounts},
		{"task-retention", r.Tasks},
		{"certificate-retention", r.Certificates},
	} {
		if ctx.Err() != nil {
			break
		}
		maintenance, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := job.store.PruneHistory(maintenance, now)
		cancel()
		report(job.name, err)
		failed = failed || err != nil
	}
	return failed
}
