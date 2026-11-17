package manage

import (
	"context"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func subscriptionPreviewSummary(snapshot sub.Snapshot, previews map[string]sub.Preview) Object {
	collections, outputs := Object{}, Object{}
	for id, preview := range previews {
		collections[id] = Object{"input": preview.Input, "output": preview.Output, "diagnostics": preview.Diagnostics}
	}
	for id, artifact := range snapshot.Artifacts {
		outputs[id] = Object{"name": artifact.Name, "mime": artifact.MIME, "bytes": len(artifact.Data), "etag": artifact.ETag}
	}
	return Object{"collections": collections, "outputs": outputs, "diagnostics": snapshot.Diagnostics}
}

// Preview only captures inputs and compiles. Persistent changes go through the
// platform apply use case; there is no second file-based configuration writer.
func (s SubscriptionService) Preview(ctx context.Context, data []byte) (Object, error) {
	var candidate *sub.Config
	var prepared map[string]sub.SourceCache
	if len(data) > 0 {
		candidate = new(sub.Config)
		if err := strictSubscriptionJSON(data, candidate); err != nil {
			return nil, err
		}
		var err error
		prepared, err = prepareSubscriptionSources(ctx, s.Root, *candidate)
		if err != nil {
			return nil, err
		}
	}
	var c sub.Config
	var r sub.Runtime
	var local map[string][]sub.Node
	err := withStartupLock(ctx, s.Root, func() error {
		var err error
		c, r, local, err = loadSubscriptions(s.Root)
		if err != nil {
			return err
		}
		if candidate != nil {
			if err = validateSubscriptionsRevision(c, *candidate); err != nil {
				return err
			}
			c = *candidate
		}
		c, local, r.Accounts, err = captureSubscriptionSources(ctx, s.Root, c, local)
		return err
	})
	if err != nil {
		return nil, err
	}
	if r.Sources == nil {
		r.Sources = map[string]sub.SourceCache{}
	}
	for id, cache := range prepared {
		r.Sources[id] = cache
	}
	_, snapshot, previews, err := compilePreparedSubscriptions(ctx, s.Root, s.Core, c, r, local, false)
	if err != nil {
		return nil, err
	}
	return subscriptionPreviewSummary(snapshot, previews), nil
}
