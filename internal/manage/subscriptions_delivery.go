package manage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

type deliveryResult struct {
	application.DeliveryOutcome
	State sub.DeliveryState
}

func deliverSubscriptions(ctx context.Context, root, id string, automatic bool, output io.Writer, services ...AccountService) (operationErr error) {
	report := application.DeliveryResult{Items: []application.DeliveryOutcome{}}
	defer func() {
		report.Interrupted = ctx.Err() != nil
		report.Complete(operationErr)
		operationErr = errors.Join(operationErr, jsonEncoder(output).Encode(report))
	}()
	if !automatic && !sub.ValidID(id) {
		return errors.New("deliver requires --id")
	}
	if err := os.MkdirAll(filepath.Join(root, "subscriptions"), 0700); err != nil {
		return err
	}
	service, closeSource, err := subscriptionSource(root, services)
	if err != nil {
		return err
	}
	defer closeSource()
	return withNamedFileLock(filepath.Join(root, "subscriptions"), ".delivery.lock", func() error {
		config, runtime, snapshot, err := captureSubscriptionDelivery(ctx, root, service)
		if err != nil {
			return err
		}
		accounts := service.Store
		client := newHTTPClient(20 * time.Second)
		defer client.CloseIdleConnections()
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		var results []deliveryResult
		var mu sync.Mutex
		var workers sync.WaitGroup
		jobs := make(chan sub.Delivery)
		now := time.Now()
		var selected []sub.Delivery
		for _, destination := range config.Deliveries {
			if automatic && (!destination.Automatic || now.Before(runtime.Deliveries[destination.ID].Next)) || !automatic && destination.ID != id {
				continue
			}
			selected = append(selected, destination)
		}
		report.Selected = len(selected)
		if len(selected) == 0 && !automatic {
			return errors.New("delivery not found")
		}
		for range 2 {
			workers.Go(func() {
				for destination := range jobs {
					if ctx.Err() != nil {
						continue
					}
					result := deliverSubscription(ctx, client, root, config, snapshot, destination, runtime.Deliveries[destination.ID], accounts)
					mu.Lock()
					results = append(results, result)
					mu.Unlock()
				}
			})
		}
	dispatch:
		for _, destination := range selected {
			select {
			case jobs <- destination:
			case <-ctx.Done():
				break dispatch
			}
		}
		close(jobs)
		workers.Wait()
		slices.SortFunc(results, func(a, b deliveryResult) int { return strings.Compare(a.ID, b.ID) })
		// Cancellation stops network work, but cannot discard acknowledged deliveries.
		finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		events, checkpointErr := checkpointDeliveries(finish, root, results)
		var failures []error
		for _, result := range results {
			result.Persisted = checkpointErr == nil && result.Persisted
			report.Items = append(report.Items, result.DeliveryOutcome)
			if result.Error != "" {
				failures = append(failures, fmt.Errorf("%s: %s", result.ID, result.Error))
			}
		}
		notify, stop := context.WithTimeout(ctx, 10*time.Second)
		defer stop()
		for _, event := range events {
			if notify.Err() != nil {
				break
			}
			notifySubscriptionEvent(notify, client, root, config, event)
		}
		return errors.Join(append(failures, checkpointErr, ctx.Err())...)
	})
}

func deliverSubscription(ctx context.Context, client *http.Client, root string, config sub.Config, snapshot sub.Snapshot, destination sub.Delivery, state sub.DeliveryState, accounts *accounting.Store) deliveryResult {
	hash := sub.DeliveryHash(destination)
	if state.ConfigHash != hash {
		state = sub.DeliveryState{ConfigHash: hash}
	}
	artifact := snapshot.Artifacts[destination.Output]
	digest := sub.Digest([]byte(hash + artifact.ETag))
	status := "unchanged"
	var err error
	if e := checkOutputPolicy(ctx, sub.PolicyForOutput(config, snapshot, destination.Output), accounts, time.Now()); e != nil {
		err = e
	} else if state.Digest != digest {
		err = sendSubscriptionArtifact(ctx, client, destination, artifact)
		status = "delivered"
	}
	now := time.Now()
	state.Checked = now
	if err != nil {
		state.Failures = min(10, state.Failures+1)
		state.Error = err.Error()
		state.Next = now.Add(time.Duration(min(60*(1<<(state.Failures-1)), 21600)) * time.Second)
		status = "failed"
		if errors.Is(err, errDeliveryUncertain) {
			status = "uncertain"
		}
		if ctx.Err() != nil && status != "uncertain" {
			status = "interrupted"
		}
	} else {
		state.Digest, state.Error, state.Failures, state.Next = digest, "", 0, now.Add(time.Minute)
		if status == "delivered" {
			state.Success = now
		}
	}
	state.Status = status
	return deliveryResult{DeliveryOutcome: application.DeliveryOutcome{ID: destination.ID, Status: status, Error: state.Error}, State: state}
}

// Preserve acknowledgements across unrelated edits, never for replaced destinations.
func checkpointDeliveries(ctx context.Context, root string, results []deliveryResult) ([]sub.Run, error) {
	var events []sub.Run
	if len(results) == 0 {
		return events, nil
	}
	err := withStartupLock(ctx, root, func() error {
		var config sub.Config
		if found, err := ReadJSON(filepath.Join(root, subscriptionsConfig), &config); err != nil {
			return err
		} else if !found {
			return errors.New("subscription configuration is missing")
		}
		operations, err := loadSubscriptionOperations(root)
		if err != nil {
			return err
		}
		state := sub.Runtime{Deliveries: operations.Deliveries, Runs: operations.Runs}
		current := map[string]string{}
		for _, destination := range config.Deliveries {
			current[destination.ID] = sub.DeliveryHash(destination)
		}
		if state.Deliveries == nil {
			state.Deliveries = map[string]sub.DeliveryState{}
		}
		for i := range results {
			result := &results[i]
			if current[result.ID] != result.State.ConfigHash {
				continue
			}
			old := state.Deliveries[result.ID]
			state.Deliveries[result.ID] = result.State
			result.Persisted = true
			if result.Status != "unchanged" {
				state.Record("delivery", result.ID, result.Status, result.State.Checked)
			}
			if old.Error != result.State.Error {
				events = append(events, sub.Run{Time: result.State.Checked, Operation: "delivery", ObjectID: result.ID, Message: result.Status})
			}
		}
		return writeSubscriptionOperations(root, config, state)
	})
	return events, err
}
