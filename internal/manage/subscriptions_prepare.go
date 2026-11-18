package manage

import (
	"context"
	"errors"
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
	lock, err := os.OpenFile(filepath.Join(root, "subscriptions/.refresh.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if err := lockFile(lock); err != nil {
		return nil, err
	}
	var runtime sub.Runtime
	if err := withStartupLock(ctx, root, func() error { var err error; _, runtime, _, err = loadSubscriptions(root); return err }); err != nil {
		return nil, err
	}
	client := newHTTPClient(20 * time.Second)
	defer client.CloseIdleConnections()
	result := map[string]sub.SourceCache{}
	total, count := 0, 0
	for _, source := range candidate.Sources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if source.Kind != "remote" {
			continue
		}
		cache := runtime.Sources[source.ID]
		limit := source.MaxStaleSeconds
		if limit == 0 {
			limit = 86400
		}
		if cache.SourceHash != sub.SourceHash(source) || cache.Success.IsZero() || time.Since(cache.Success) > time.Duration(limit)*time.Second {
			cache = refreshSource(ctx, client, source, cache, time.Now())
		}
		data, err := JSONBytes(cache)
		if err != nil {
			return nil, err
		}
		total += len(data)
		count += len(cache.Nodes)
		if total > sub.MaxBody || count > sub.MaxNodes {
			return nil, errors.New("combined source cache limit exceeded")
		}
		result[source.ID] = cache
	}
	return result, nil
}
