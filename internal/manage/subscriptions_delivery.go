package manage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func sendSubscriptionArtifact(ctx context.Context, client *http.Client, d sub.Delivery, artifact sub.Artifact) error {
	method, data := http.MethodPut, artifact.Data
	if d.Kind == "github-gist" {
		// Subscription credentials must never be uploaded to a public Gist.
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.URL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+d.Token)
		response, err := client.Do(req)
		if err != nil {
			return errors.New("Gist visibility check failed")
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, sub.MaxBody+1))
		response.Body.Close()
		var gist struct {
			Public *bool `json:"public"`
		}
		if readErr != nil || response.StatusCode != 200 || len(body) > sub.MaxBody || json.Unmarshal(body, &gist) != nil || gist.Public == nil || *gist.Public {
			return errors.New("delivery requires an existing secret Gist")
		}
		method = http.MethodPatch
		data, err = json.Marshal(Object{"files": Object{d.Filename: Object{"content": string(artifact.Data)}}})
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, d.URL, bytes.NewReader(data))
	if err != nil {
		return errors.New("invalid delivery request")
	}
	req.Header.Set("Content-Type", artifact.MIME)
	if d.Kind == "github-gist" {
		req.Header.Set("Content-Type", "application/json")
	}
	if d.Username != "" {
		req.SetBasicAuth(d.Username, d.Password)
	}
	if d.Token != "" {
		req.Header.Set("Authorization", "Bearer "+d.Token)
	}
	response, err := client.Do(req)
	if err != nil {
		return errors.New("delivery failed or timed out")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("delivery HTTP %d", response.StatusCode)
	}
	n, err := io.Copy(io.Discard, io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || n > 4<<20 {
		return errors.New("invalid delivery acknowledgement")
	}
	return nil
}

func deliverSubscriptions(ctx context.Context, root, id string, automatic bool, output io.Writer) error {
	if !automatic && !sub.ValidID(id) {
		return errors.New("deliver requires --id")
	}
	if err := os.MkdirAll(filepath.Join(root, "subscriptions"), 0700); err != nil {
		return err
	}
	return withNamedFileLock(filepath.Join(root, "subscriptions"), ".delivery.lock", func() error {
		var c sub.Config
		var r sub.Runtime
		var snapshot sub.Snapshot
		if err := withStartupLock(ctx, root, func() error {
			var err error
			c, r, _, err = loadSubscriptions(root)
			if err != nil {
				return err
			}
			_, err = ReadJSON(filepath.Join(root, subscriptionsSnapshot), &snapshot)
			return err
		}); err != nil {
			return err
		}
		client := newHTTPClient(20 * time.Second)
		defer client.CloseIdleConnections()
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		type result struct {
			ID     string
			State  sub.DeliveryState
			Err    error
			Status string
		}
		var results []result
		var mu sync.Mutex
		var workers sync.WaitGroup
		jobs := make(chan sub.Delivery)
		now := time.Now()
		for range 2 {
			workers.Go(func() {
				for d := range jobs {
					state := r.Deliveries[d.ID]
					artifact, exists := snapshot.Artifacts[d.Output]
					settings, _ := json.Marshal(d)
					digest := sub.Digest(append(settings, []byte(artifact.ETag)...))
					status := "unchanged"
					var err error
					if !exists || !subscriptionFresh(c, snapshot, d.Output, now) {
						err = errors.New("output unavailable or stale")
					} else if state.Digest != digest {
						err = sendSubscriptionArtifact(ctx, client, d, artifact)
						status = "delivered"
					}
					state.Checked = now
					if err != nil {
						state.Failures = min(10, state.Failures+1)
						state.Error = err.Error()
						state.Next = now.Add(time.Duration(min(60*(1<<(state.Failures-1)), 21600)) * time.Second)
						status = "failed"
					} else {
						state.Digest, state.Error, state.Failures, state.Next = digest, "", 0, now.Add(time.Minute)
					}
					mu.Lock()
					results = append(results, result{d.ID, state, err, status})
					mu.Unlock()
				}
			})
		}
		for _, d := range c.Deliveries {
			if automatic && (!d.Automatic || now.Before(r.Deliveries[d.ID].Next)) || !automatic && d.ID != id {
				continue
			}
			select {
			case jobs <- d:
			case <-ctx.Done():
			}
		}
		close(jobs)
		workers.Wait()
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(results) == 0 {
			if automatic {
				return nil
			}
			return errors.New("delivery not found")
		}
		var failures []error
		var events []sub.Run
		err := withLock(root, func() error {
			current, state, _, err := loadSubscriptions(root)
			if err != nil {
				return err
			}
			if err := validateSubscriptionsRevision(current, c); err != nil {
				return err
			}
			if state.Deliveries == nil {
				state.Deliveries = map[string]sub.DeliveryState{}
			}
			for _, result := range results {
				old := state.Deliveries[result.ID]
				state.Deliveries[result.ID] = result.State
				if result.Status != "unchanged" {
					state.Record("delivery", result.ID, result.Status, now)
				}
				if old.Error != result.State.Error {
					events = append(events, sub.Run{Time: now, Operation: "delivery", ObjectID: result.ID, Message: result.Status})
				}
				if result.Err != nil {
					failures = append(failures, fmt.Errorf("%s: %w", result.ID, result.Err))
				}
				if !automatic {
					if err := jsonEncoder(output).Encode(Object{"id": result.ID, "status": result.Status}); err != nil {
						return err
					}
				}
			}
			return AtomicJSON(filepath.Join(root, subscriptionsRuntime), state)
		})
		for _, event := range events {
			notifySubscriptionEvent(ctx, client, root, c, event)
		}
		return errors.Join(append(failures, err)...)
	})
}
