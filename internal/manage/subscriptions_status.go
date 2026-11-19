package manage

import (
	"context"
	"path/filepath"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

type subscriptionActivity struct {
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

type activityMeta struct {
	Checked  time.Time `json:"checked"`
	Success  time.Time `json:"success"`
	Next     time.Time `json:"next"`
	Failures int       `json:"failures"`
	Error    string    `json:"error"`
}

// This projection skips cached node arrays, template bodies and credentials.
// Large source caches do not become large status objects or API responses.
type subscriptionActivityCache struct {
	Sources map[string]struct {
		activityMeta
		SourceHash string `json:"source_hash"`
	} `json:"sources"`
	Resources map[string]struct {
		activityMeta
		URL string `json:"url"`
	} `json:"resources"`
}

func activity(id, name, kind string, meta activityMeta) subscriptionActivity {
	state := "pending"
	if !meta.Checked.IsZero() || !meta.Success.IsZero() {
		state = "ok"
	}
	if meta.Error != "" {
		state = "failed"
	}
	return subscriptionActivity{id, name, kind, state, meta.Checked, meta.Success, meta.Next, meta.Failures, meta.Error}
}

func (s *PlatformService) SubscriptionStatus(ctx context.Context) (Object, error) {
	var config sub.Config
	var cached subscriptionActivityCache
	var notificationState sub.NotificationState
	outputs := []subscriptionOutputStatus{}
	err := withStartupLock(ctx, s.Root, func() error {
		if _, err := ReadJSON(filepath.Join(s.Root, subscriptionsConfig), &config); err != nil {
			return err
		}
		if _, err := ReadJSON(filepath.Join(s.Root, subscriptionsRuntime), &cached); err != nil {
			return err
		}
		if _, err := ReadJSON(filepath.Join(s.Root, subscriptionsOutputStatus), &outputs); err != nil {
			return err
		}
		_, err := ReadJSON(filepath.Join(s.Root, subscriptionNotification), &notificationState)
		return err
	})
	if err != nil {
		return nil, err
	}
	sources, resources := []subscriptionActivity{}, []subscriptionActivity{}
	for _, source := range config.Sources {
		if source.Kind != "remote" {
			continue
		}
		entry := cached.Sources[source.ID]
		meta := entry.activityMeta
		if entry.SourceHash != sub.SourceHash(source) {
			meta = activityMeta{}
		}
		sources = append(sources, activity(source.ID, source.Name, "source", meta))
	}
	for _, t := range config.Templates {
		if t.URL == "" {
			continue
		}
		entry := cached.Resources["template:"+t.ID]
		meta := entry.activityMeta
		if entry.URL != t.URL {
			meta = activityMeta{}
		}
		resources = append(resources, activity(t.ID, t.ID, "template", meta))
	}
	for _, r := range config.RuleSets {
		if r.URL == "" {
			continue
		}
		entry := cached.Resources["rules:"+r.ID]
		meta := entry.activityMeta
		if entry.URL != r.URL {
			meta = activityMeta{}
		}
		resources = append(resources, activity(r.ID, r.ID, "rules", meta))
	}
	n := notificationState
	if n.EndpointHash != sub.Digest([]byte(config.Webhook)) {
		n = sub.NotificationState{}
	}
	notification := activity("webhook", "Webhook", "notification", activityMeta{Checked: n.Checked, Success: n.Success, Failures: n.Failures, Error: n.Error})
	if config.Webhook == "" {
		notification.State = "disabled"
	}
	return Object{"revision": config.Revision, "sources": sources, "resources": resources, "outputs": outputs, "notification": Object{"configured": config.Webhook != "", "activity": notification, "event": n.Event, "object_id": n.ObjectID}}, nil
}
