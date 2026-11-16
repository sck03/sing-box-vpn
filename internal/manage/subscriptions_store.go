package manage

import (
	"context"
	"encoding/json"
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
	r = sub.PruneRuntime(c, r)
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
					return nil, sub.Snapshot{}, nil, err
				}
			}
			nodes, err := service.accountNodes(ctx, source.AccountID, source.Scope, 0)
			if errors.Is(err, accounting.ErrDenied) {
				nodes = nil
				err = nil
			}
			if err != nil {
				return nil, sub.Snapshot{}, nil, err
			}
			local["account:"+source.ID] = nodes
		}
	}
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
		if old, exists := previous.Artifacts[output.ID]; exists && previous.CoreIdentity == snapshot.CoreIdentity && old.ETag == artifact.ETag {
			continue
		}
		if t := templates[output.Template]; t.Client == "sing-box" && t.Mode == "config" {
			var config Object
			if err := json.Unmarshal(artifact.Data, &config); err != nil {
				return nil, snapshot, previews, err
			}
			if err := CheckJSON(ctx, core, root, config); err != nil {
				return nil, snapshot, previews, fmt.Errorf("subscription %s: %w", output.ID, err)
			}
		}
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
