package manage

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

const subscriptionNotification = "subscriptions/notification.json"

// Notification activity is small, reconstructible metadata. Updating it must
// not rewrite multi-megabyte node/template caches or roll back a publication.
func notifySubscriptionEvent(ctx context.Context, client *http.Client, root string, c sub.Config, event sub.Run) {
	if c.Webhook == "" {
		return
	}
	sendErr := remoteRequest(ctx, client, c.Webhook, "POST", event, nil, nil)
	record, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = withStartupLock(record, root, func() error {
		var current struct {
			Webhook string `json:"webhook"`
		}
		if _, err := ReadJSON(filepath.Join(root, subscriptionsConfig), &current); err != nil {
			return err
		}
		if current.Webhook != c.Webhook {
			return nil
		}
		var state sub.NotificationState
		if _, err := ReadJSON(filepath.Join(root, subscriptionNotification), &state); err != nil {
			return err
		}
		hash := sub.Digest([]byte(c.Webhook))
		if state.EndpointHash != hash {
			state = sub.NotificationState{EndpointHash: hash}
		}
		now := time.Now()
		state.Checked, state.Event, state.ObjectID = now, event.Operation, event.ObjectID
		if sendErr == nil {
			state.Success, state.Failures, state.Error = now, 0, ""
		} else {
			state.Failures = min(state.Failures+1, 1000000)
			state.Error = "notification delivery failed"
			var status httpStatusError
			if errors.As(sendErr, &status) {
				state.Error = fmt.Sprintf("notification delivery failed (HTTP %d)", status)
			}
		}
		return AtomicJSON(filepath.Join(root, subscriptionNotification), state)
	})
}
