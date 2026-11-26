package manage

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

const subscriptionsActivity = "subscriptions/activity.json"

type SubscriptionActivity struct {
	ID       string    `json:"id"`
	Name     string    `json:"name,omitempty"`
	Kind     string    `json:"kind"`
	State    string    `json:"state"`
	Checked  time.Time `json:"checked,omitzero"`
	Success  time.Time `json:"success,omitzero"`
	Next     time.Time `json:"next,omitzero"`
	Failures int       `json:"failures"`
	Error    string    `json:"error,omitempty"`
}

type DeliveryActivity struct {
	SubscriptionActivity
	Output    string `json:"output"`
	Automatic bool   `json:"automatic"`
}

type NotificationActivity struct {
	Configured bool                 `json:"configured"`
	Activity   SubscriptionActivity `json:"activity"`
	Event      string               `json:"event,omitempty"`
	ObjectID   string               `json:"object_id,omitempty"`
}

type SubscriptionStatusResponse struct {
	Revision     uint64                     `json:"revision"`
	Sources      []SubscriptionActivity     `json:"sources"`
	Resources    []SubscriptionActivity     `json:"resources"`
	Deliveries   []DeliveryActivity         `json:"deliveries"`
	Outputs      []SubscriptionOutputStatus `json:"outputs"`
	Notification NotificationActivity       `json:"notification"`
}

// The operational projection contains no cached bodies, nodes or destination secrets.
// It commits with runtime/schedule so status reads never scan the large runtime cache.
type subscriptionActivityProjection struct {
	Revision    uint64                 `json:"revision"`
	Sources     []SubscriptionActivity `json:"sources"`
	Resources   []SubscriptionActivity `json:"resources"`
	Deliveries  []DeliveryActivity     `json:"deliveries"`
	WebhookHash string                 `json:"webhook_hash,omitempty"`
}

func activity(id, name, kind string, checked, success, next time.Time, failures int, message string) SubscriptionActivity {
	state := "pending"
	if !checked.IsZero() || !success.IsZero() {
		state = "ok"
	}
	if message != "" {
		state = "failed"
	}
	return SubscriptionActivity{id, name, kind, state, checked, success, next, failures, message}
}

func subscriptionActivityStatus(config sub.Config, runtime sub.Runtime) subscriptionActivityProjection {
	runtime = sub.PruneRuntime(config, runtime)
	result := subscriptionActivityProjection{Revision: config.Revision, Sources: []SubscriptionActivity{}, Resources: []SubscriptionActivity{}, Deliveries: []DeliveryActivity{}}
	if config.Webhook != "" {
		result.WebhookHash = sub.Digest([]byte(config.Webhook))
	}
	for _, source := range config.Sources {
		if source.Kind != "remote" {
			continue
		}
		state := runtime.Sources[source.ID]
		result.Sources = append(result.Sources, activity(source.ID, source.Name, "source", state.Checked, state.Success, state.Next, state.Failures, state.Error))
	}
	resource := func(id, kind string) {
		state := runtime.Resources[kind+":"+id]
		result.Resources = append(result.Resources, activity(id, id, kind, state.Checked, state.Success, state.Next, state.Failures, state.Error))
	}
	for _, template := range config.Templates {
		if template.URL != "" {
			resource(template.ID, "template")
		}
	}
	for _, rules := range config.RuleSets {
		if rules.URL != "" {
			resource(rules.ID, "rules")
		}
	}
	for _, destination := range config.Deliveries {
		state := runtime.Deliveries[destination.ID]
		row := activity(destination.ID, destination.ID, destination.Kind, state.Checked, state.Success, state.Next, state.Failures, state.Error)
		if state.Status != "" {
			row.State = state.Status
		}
		result.Deliveries = append(result.Deliveries, DeliveryActivity{row, destination.Output, destination.Automatic})
	}
	return result
}

func (s *PlatformService) SubscriptionStatus(ctx context.Context) (SubscriptionStatusResponse, error) {
	var result SubscriptionStatusResponse
	err := withStartupLock(ctx, s.Root, func() error {
		var cached subscriptionActivityProjection
		found, err := ReadJSON(filepath.Join(s.Root, subscriptionsActivity), &cached)
		if err != nil {
			return err
		}
		if !found {
			return errors.New("subscription activity projection unavailable; rebuild the publication")
		}
		result.Revision, result.Sources, result.Resources, result.Deliveries = cached.Revision, cached.Sources, cached.Resources, cached.Deliveries
		result.Outputs = []SubscriptionOutputStatus{}
		var outputs subscriptionOutputProjection
		if _, err = ReadJSON(filepath.Join(s.Root, subscriptionsOutputStatus), &outputs); err != nil {
			return err
		}
		if outputs.Outputs != nil {
			result.Outputs = outputs.Outputs
		}
		if s.Store == nil {
			return errors.New("configuration repository dependency is required")
		}
		applied, _, generationErr := confirmedSubscriptionState(ctx, s.Root, s.accountService())
		for i := range result.Outputs {
			output := &result.Outputs[i]
			if generationErr != nil || outputs.Revision != applied.Subscriptions.Revision {
				output.Available, output.Reason = false, "发布尚未确认，请稍后重试"
			} else if err := checkOutputPolicy(ctx, outputs.Policies[output.ID], s.Accounts, time.Now()); err != nil {
				output.Available, output.Reason = false, err.Error()
			}
		}
		var state sub.NotificationState
		if _, err = ReadJSON(filepath.Join(s.Root, subscriptionNotification), &state); err != nil {
			return err
		}
		if state.EndpointHash != cached.WebhookHash {
			state = sub.NotificationState{}
		}
		row := activity("webhook", "Webhook", "notification", state.Checked, state.Success, time.Time{}, state.Failures, state.Error)
		if cached.WebhookHash == "" {
			row.State = "disabled"
		}
		result.Notification = NotificationActivity{cached.WebhookHash != "", row, state.Event, state.ObjectID}
		return nil
	})
	return result, err
}
