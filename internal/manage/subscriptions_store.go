package manage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

const subscriptionsConfig = "subscriptions/config.json"
const subscriptionsRuntime = "subscriptions/runtime.json"
const subscriptionsLocal = "subscriptions/local.json"
const subscriptionsSnapshot = "subscriptions/published.json"

func loadSubscriptions(root string) (sub.Config, sub.Runtime, map[string][]sub.Node, error) {
	var c sub.Config
	if found, err := ReadJSON(filepath.Join(root, subscriptionsConfig), &c); err != nil {
		return c, sub.Runtime{}, nil, err
	} else if !found {
		c = sub.DefaultConfig()
	}
	if err := c.Validate(); err != nil {
		return c, sub.Runtime{}, nil, err
	}
	r, err := loadSubscriptionRuntime(root)
	if err != nil {
		return c, r, nil, err
	}
	local := map[string][]sub.Node{}
	_, err = ReadJSON(filepath.Join(root, subscriptionsLocal), &local)
	return c, r, local, err
}

func loadSubscriptionRuntime(root string) (sub.Runtime, error) {
	r := sub.Runtime{Sources: map[string]sub.SourceCache{}}
	_, err := ReadJSON(filepath.Join(root, subscriptionsRuntime), &r)
	return r, err
}

// Called under the publication lock during explicit recovery/startup only.
// The cache is rebuildable; retain the damaged bytes for diagnosis and record
// the loss, while I/O errors still fail rather than masquerading as empty data.
func recoverSubscriptionRuntime(root string) error {
	path := filepath.Join(root, subscriptionsRuntime)
	var state sub.Runtime
	found, err := ReadJSON(path, &state)
	if err == nil {
		return nil
	}
	if !found {
		return err
	}
	if err = os.Rename(path, filepath.Join(root, "subscriptions/runtime.corrupt.json")); err != nil {
		return err
	}
	state = sub.Runtime{Sources: map[string]sub.SourceCache{}}
	state.Record("cache-recovery", "", "Damaged subscription cache quarantined; remote sources must refresh", time.Now())
	return AtomicJSON(path, state)
}

func compileSubscriptions(ctx context.Context, root, core string, c sub.Config, r sub.Runtime, local map[string][]sub.Node, mode sub.BuildMode, services ...AccountService) (fileBundle, sub.Snapshot, map[string]sub.Preview, error) {
	c, local, accounts, err := captureSubscriptionSources(ctx, root, c, local, services...)
	if err != nil {
		return nil, sub.Snapshot{}, nil, err
	}
	r.Accounts = accounts
	return compilePreparedSubscriptions(ctx, root, core, c, r, local, mode)
}

// Capture account material while the caller owns the publication lock. Core
// validation consumes the resulting value snapshot and does not need that lock.
func captureSubscriptionSources(ctx context.Context, root string, c sub.Config, local map[string][]sub.Node, services ...AccountService) (sub.Config, map[string][]sub.Node, map[string]sub.AccountSourceState, error) {
	service := AccountService{Root: root}
	if len(services) > 0 {
		service = services[0]
	}
	accounts := map[string]sub.AccountSourceState{}
	usages := map[string]accounting.Usage{}
	now := time.Now()
	var closeStore func()
	defer func() {
		if closeStore != nil {
			closeStore()
		}
	}()
	if local == nil {
		local = map[string][]sub.Node{}
	}
	for _, source := range c.Sources {
		if source.Kind == "account" {
			if service.Store == nil {
				var err error
				service.Store, closeStore, err = service.openStore()
				if err != nil {
					return c, nil, nil, err
				}
			}
			if service.Config == nil {
				config, err := service.configSnapshot(ctx)
				if err != nil {
					return c, nil, nil, err
				}
				service.Config = &config
			}
			state, found := accounts[source.AccountID]
			usage := usages[source.AccountID]
			if !found {
				var err error
				usage, state, err = accountSourceState(ctx, service.Store, source.AccountID, now)
				if err != nil {
					return c, nil, nil, err
				}
				accounts[source.AccountID], usages[source.AccountID] = state, usage
			}
			var nodes []sub.Node
			if state.Available {
				var err error
				nodes, err = accountNodes(ctx, service.Store, *service.Config, usage, source.Scope, 0)
				if err != nil {
					return c, nil, nil, err
				}
			}
			local["account:"+source.ID] = nodes
		}
	}
	return c, local, accounts, nil
}

func accountSourceState(ctx context.Context, store *accounting.Store, id string, now time.Time) (accounting.Usage, sub.AccountSourceState, error) {
	u, err := store.DownloadAccount(ctx, id, now)
	state := sub.AccountSourceState{Revision: u.Revision, Available: err == nil}
	if errors.Is(err, accounting.ErrDenied) || errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return u, state, err
}

func compilePreparedSubscriptions(ctx context.Context, root, core string, c sub.Config, r sub.Runtime, local map[string][]sub.Node, mode sub.BuildMode) (fileBundle, sub.Snapshot, map[string]sub.Preview, error) {
	r = sub.PruneRuntime(c, r)
	snapshot, previews, err := sub.Compile(c, r, local, time.Now(), mode)
	if err != nil {
		return nil, snapshot, previews, err
	}
	snapshot.CoreIdentity, err = coreIdentity(core)
	if err != nil {
		return nil, snapshot, previews, err
	}
	var previous sub.Snapshot
	// This is only a validation cache. A broken projection is rebuilt, never
	// allowed to prevent recovery of the database-owned configuration.
	if _, err := ReadJSON(filepath.Join(root, subscriptionsSnapshot), &previous); err != nil {
		previous = sub.Snapshot{}
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
			if mode == sub.Available {
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
