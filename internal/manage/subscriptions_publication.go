package manage

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/sck03/sing-box-vpn/internal/application"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

type subscriptionSnapshotCompiler func(context.Context, string, string, sub.Config, sub.Runtime, map[string][]sub.Node, sub.BuildMode) (fileBundle, sub.Snapshot, map[string]sub.Preview, error)

func subscriptionInputsDigest(local map[string][]sub.Node, accounts map[string]sub.AccountSourceState) string {
	data, _ := json.Marshal(struct {
		Nodes    map[string][]sub.Node
		Accounts map[string]sub.AccountSourceState
	}{local, accounts})
	return sub.Digest(data)
}

// Fetching and core validation own no publication lock. The final transaction
// rechecks both configuration and independently changing account credentials.
func publishSubscriptionRefresh(ctx context.Context, root, core string, expected sub.Config, sources map[string]sub.SourceCache, resources map[string]sub.ResourceCache, events []sub.Run, now time.Time, resourceChanged bool, report *application.RefreshResult, compile subscriptionSnapshotCompiler, services ...AccountService) ([]sub.Run, error) {
	service, closeSource, err := subscriptionSource(root, services)
	if err != nil {
		return events, err
	}
	defer closeSource()
	var config sub.Config
	var state sub.Runtime
	var local map[string][]sub.Node
	var generation int64
	err = withStartupLock(ctx, root, func() error {
		var err error
		config, state, err = loadSubscriptionState(root)
		if err != nil {
			return err
		}
		if err = validateSubscriptionsRevision(config, expected); err != nil {
			return err
		}
		local, state.Accounts, generation, err = captureConfirmedSubscriptionSources(ctx, root, config, service)
		return err
	})
	if err != nil {
		return events, err
	}
	inputDigest := subscriptionInputsDigest(local, state.Accounts)
	previousResources := state.Resources
	state = sub.PruneRuntime(config, state)
	if state.Sources == nil {
		state.Sources = map[string]sub.SourceCache{}
	}
	if state.Resources == nil {
		state.Resources = map[string]sub.ResourceCache{}
	}
	runs := append([]sub.Run(nil), events...)
	for id, value := range resources {
		state.Resources[id] = value
	}
	for id, value := range sources {
		old := state.Sources[id]
		if expiry := value.Usage.Expire; expiry != nil && *expiry > 0 && *expiry <= now.Add(72*time.Hour).Unix() && value.NotifiedExpiry != *expiry {
			value.NotifiedExpiry = *expiry
			event := sub.Run{Time: now, Operation: "expiry", ObjectID: id, Message: "source expires at " + time.Unix(*expiry, 0).UTC().Format(time.RFC3339)}
			events = append(events, event)
			runs = append(runs, event)
		}
		state.Sources[id] = value
		message := "refresh succeeded"
		if value.Error != "" {
			message = value.Error
		}
		run := sub.Run{Time: now, Operation: "refresh", ObjectID: id, Message: message}
		runs = append(runs, run)
		if old.Error != value.Error && (old.Error != "" || value.Error != "") {
			events = append(events, run)
		}
	}
	mode := sub.Available
	if resourceChanged {
		mode = sub.Candidate
	}
	files, snapshot, _, buildErr := compile(ctx, root, core, config, state, local, mode)
	var rejected error
	if buildErr != nil && resourceChanged && ctx.Err() == nil {
		rejected = buildErr
		for id, value := range resources {
			previous := previousResources[id]
			if previous.URL != value.URL {
				previous = sub.ResourceCache{URL: value.URL}
			}
			previous.Checked, previous.Error = now, "resource update rejected by output validation"
			previous.Failures = min(previous.Failures+1, 10)
			previous.Next = now.Add(time.Duration(min(60*(1<<(previous.Failures-1)), 21600)) * time.Second)
			state.Resources[id] = previous
			if value.Error == "" {
				report.Updated--
				report.Failures = append(report.Failures, application.RefreshFailure{ID: id, Error: previous.Error})
			}
		}
		runs = append(runs, sub.Run{Time: now, Operation: "publish", Message: "resource update rejected; retained validated resources"})
		// An invalid update must not prevent unrelated outputs or revoked account
		// sources from being republished using the previous valid resources.
		files, snapshot, _, buildErr = compile(ctx, root, core, config, state, local, sub.Available)
	}
	report.Outputs, report.ExpectedOutputs = len(snapshot.Artifacts), len(config.Outputs)
	if err := ctx.Err(); err != nil {
		return events, err
	}
	err = withLock(root, func() error {
		current, latest, err := loadSubscriptionState(root)
		if err != nil {
			return err
		}
		if err = validateSubscriptionsRevision(current, config); err != nil {
			return err
		}
		currentNodes, accounts, currentGeneration, err := captureConfirmedSubscriptionSources(ctx, root, current, service)
		if err != nil {
			return err
		}
		if currentGeneration != generation || subscriptionInputsDigest(currentNodes, accounts) != inputDigest {
			return application.ErrConflict
		}
		if buildErr == nil {
			identity, err := coreIdentity(core)
			if err != nil {
				return err
			}
			if snapshot.CoreIdentity != identity {
				return application.ErrConflict
			}
		}
		// Probe/delivery records may have changed while validation was running.
		state.Runs, state.Deliveries = latest.Runs, latest.Deliveries
		for _, run := range runs {
			state.Record(run.Operation, run.ObjectID, run.Message, run.Time)
		}
		if buildErr != nil {
			state.Record("publish", "", buildErr.Error(), now)
			return errors.Join(buildErr, writeSubscriptionRuntime(root, current, state))
		}
		state.RefreshPending = false
		if err = files.subscriptionRuntime(current, state); err != nil {
			return err
		}
		if err := publish(root, files); err != nil {
			return err
		}
		report.Published = true
		return nil
	})
	return events, errors.Join(err, rejected)
}
