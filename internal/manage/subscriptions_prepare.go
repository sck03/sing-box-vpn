package manage

import (
	"context"
	"os"
	"path/filepath"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

// Fetch newly configured required sources before taking the commit lock. This
// permits adding a required remote subscription in one validated operation.
func prepareSubscriptionSources(ctx context.Context, root string, candidate sub.Config) (map[string]sub.SourceCache, error) {
	if err := candidate.Validate(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(root, "subscriptions"), 0700); err != nil {
		return nil, err
	}
	// Candidate preparation only reads published state. Revision/input checks at
	// commit prevent stale publication; background fetching must not block edits.
	var runtime sub.Runtime
	if err := withStartupLock(ctx, root, func() error { var err error; _, runtime, err = loadSubscriptionState(root); return err }); err != nil {
		return nil, err
	}
	client := newHTTPClient(20 * time.Second)
	defer client.CloseIdleConnections()
	runtime = sub.PruneRuntime(candidate, runtime)
	now := time.Now()
	var due []sub.Source
	for _, source := range candidate.Sources {
		if source.Kind != "remote" {
			continue
		}
		cache := runtime.Sources[source.ID]
		limit := source.MaxStaleSeconds
		if limit == 0 {
			limit = 86400
		}
		if cache.SourceHash != sub.SourceHash(source) || cache.Success.IsZero() || now.Sub(cache.Success) > time.Duration(limit)*time.Second {
			due = append(due, source)
		}
	}
	result, _, err := refreshSubscriptionSources(ctx, client, due, runtime.Sources, now, true)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	for _, source := range candidate.Sources {
		if source.Kind == "remote" {
			if _, fetched := result[source.ID]; !fetched {
				result[source.ID] = runtime.Sources[source.ID]
			}
		}
	}
	return result, nil
}
