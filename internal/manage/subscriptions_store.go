package manage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/sck03/sing-box-vpn/internal/accounting"
	"path/filepath"
	"time"

	"github.com/sck03/sing-box-vpn/internal/application"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

const subscriptionsConfig = "subscriptions/config.json"
const subscriptionsRuntime = "subscriptions/runtime.json"
const subscriptionsLocal = "subscriptions/local.json"
const subscriptionsSnapshot = "subscriptions/published.json"

func loadSubscriptions(root string) (sub.Config, sub.Runtime, map[string][]sub.Node, error) {
	c := sub.DefaultConfig()
	if _, err := ReadJSON(filepath.Join(root, subscriptionsConfig), &c); err != nil {
		return c, sub.Runtime{}, nil, err
	}
	if err := c.Validate(); err != nil {
		return c, sub.Runtime{}, nil, err
	}
	r := sub.Runtime{Sources: map[string]sub.SourceCache{}}
	if _, err := ReadJSON(filepath.Join(root, subscriptionsRuntime), &r); err != nil {
		return c, r, nil, err
	}
	local := map[string][]sub.Node{}
	_, err := ReadJSON(filepath.Join(root, subscriptionsLocal), &local)
	return c, r, local, err
}

func buildSubscriptions(ctx context.Context, root, core string, c sub.Config, r sub.Runtime, local map[string][]sub.Node) (fileBundle, sub.Snapshot, map[string]sub.Preview, error) {
	return compileSubscriptions(ctx, root, core, c, r, local, false)
}

func compileSubscriptions(ctx context.Context, root, core string, c sub.Config, r sub.Runtime, local map[string][]sub.Node, available bool, stores ...*accounting.Store) (fileBundle, sub.Snapshot, map[string]sub.Preview, error) {
	c, local, err := captureSubscriptionSources(ctx, root, c, local, stores...)
	if err != nil {
		return nil, sub.Snapshot{}, nil, err
	}
	return compilePreparedSubscriptions(ctx, root, core, c, r, local, available)
}

// Capture account material while the caller owns the publication lock. Core
// validation consumes the resulting value snapshot and does not need that lock.
func captureSubscriptionSources(ctx context.Context, root string, c sub.Config, local map[string][]sub.Node, stores ...*accounting.Store) (sub.Config, map[string][]sub.Node, error) {
	service := AccountService{Root: root}
	if len(stores) > 0 {
		service.Store = stores[0]
	}
	var closeStore func()
	defer func() {
		if closeStore != nil {
			closeStore()
		}
	}()
	if local == nil {
		local = map[string][]sub.Node{}
	}
	if platformOwned(root) {
		local[normalScope], local[vpngateScope] = nil, nil
		c.Sources = append([]sub.Source(nil), c.Sources...)
		for i := range c.Sources {
			if c.Sources[i].Kind == "local" {
				c.Sources[i].Required = false
			}
		}
	}
	for _, source := range c.Sources {
		if source.Kind == "account" {
			if service.Store == nil {
				var err error
				service.Store, closeStore, err = service.openStore()
				if err != nil {
					return c, nil, err
				}
			}
			nodes, err := service.accountNodes(ctx, source.AccountID, source.Scope, 0)
			if errors.Is(err, accounting.ErrDenied) || errors.Is(err, sql.ErrNoRows) {
				nodes = nil
				err = nil
			}
			if err != nil {
				return c, nil, err
			}
			local["account:"+source.ID] = nodes
		}
	}
	return c, local, nil
}

func compilePreparedSubscriptions(ctx context.Context, root, core string, c sub.Config, r sub.Runtime, local map[string][]sub.Node, available bool) (fileBundle, sub.Snapshot, map[string]sub.Preview, error) {
	r = sub.PruneRuntime(c, r)
	build := sub.Build
	if available {
		build = sub.BuildAvailable
	}
	snapshot, previews, err := build(c, r, local, time.Now())
	if err != nil {
		return nil, snapshot, previews, err
	}
	snapshot.CoreIdentity, err = coreIdentity(core)
	if err != nil {
		return nil, snapshot, previews, err
	}
	var previous sub.Snapshot
	if _, err := ReadJSON(filepath.Join(root, subscriptionsSnapshot), &previous); err != nil {
		return nil, snapshot, previews, err
	}
	templates := map[string]sub.Template{}
	effective, err := sub.ResolveResources(c, r.Resources)
	if err != nil {
		return nil, snapshot, previews, err
	}
	for _, t := range effective.Templates {
		templates[t.ID] = t
	}
	for _, output := range c.Outputs {
		artifact, exists := snapshot.Artifacts[output.ID]
		if !exists {
			continue
		}
		if old, exists := previous.Artifacts[output.ID]; exists && templates[output.Template].Client == "sing-box" && previous.CoreIdentity == snapshot.CoreIdentity && old.ETag == artifact.ETag {
			snapshot.Diagnostics[output.ID] = append(snapshot.Diagnostics[output.ID], sub.Diagnostic{Stage: "validation", Message: "sing-box core-check; unchanged validated artifact"})
			continue
		}
		verification, err := validateSubscriptionArtifact(ctx, root, core, templates[output.Template], artifact)
		if err != nil {
			if available {
				delete(snapshot.Artifacts, output.ID)
				snapshot.Diagnostics[output.ID] = append(snapshot.Diagnostics[output.ID], sub.Diagnostic{Stage: "validation", Message: err.Error()})
				continue
			}
			return nil, snapshot, previews, fmt.Errorf("subscription %s: %w", output.ID, err)
		}
		snapshot.Diagnostics[output.ID] = append(snapshot.Diagnostics[output.ID], sub.Diagnostic{Stage: "validation", Message: verification})
	}
	files := fileBundle{}
	for name, value := range map[string]any{subscriptionsConfig: c, subscriptionsRuntime: r, subscriptionsLocal: local, subscriptionsSnapshot: snapshot} {
		if err := files.json(name, value); err != nil {
			return nil, snapshot, previews, err
		}
		if len(files[name]) > maxJSONFile {
			return nil, snapshot, previews, errors.New("subscription state exceeds 16 MiB")
		}
	}
	if err := appendDefaultExports(files, snapshot, previews); err != nil {
		return nil, snapshot, previews, err
	}
	return files, snapshot, previews, nil
}

func validateSubscriptionsRevision(current, candidate sub.Config) error {
	if current.Revision != candidate.Revision {
		return application.ErrConflict
	}
	return nil
}
