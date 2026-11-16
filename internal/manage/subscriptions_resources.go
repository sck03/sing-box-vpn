package manage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

// Resources refresh hourly with independent ETags and bounded failure backoff.
// Failed updates leave the previously validated template/rules in place.
func refreshSubscriptionResources(ctx context.Context, client *http.Client, config *sub.Config, old map[string]sub.ResourceCache, now time.Time, force bool) (bool, map[string]sub.ResourceCache, []sub.Run) {
	changed := false
	results := map[string]sub.ResourceCache{}
	var events []sub.Run
	refresh := func(id, endpoint string, apply func([]byte) error) {
		if endpoint == "" {
			return
		}
		cache := old[id]
		if cache.URL != endpoint {
			cache = sub.ResourceCache{URL: endpoint}
		}
		if !force && now.Before(cache.Next) {
			return
		}
		priorError := cache.Error
		data, headers, unchanged, err := fetchSubscription(ctx, client, endpoint, cache.ETag)
		if err == nil && unchanged && cache.ETag == "" {
			err = errors.New("resource returned 304 without cache")
		}
		if err == nil && !unchanged {
			err = apply(data)
		}
		if err != nil {
			cache.Failures = min(cache.Failures+1, 10)
			cache.Error = err.Error()
			cache.Next = now.Add(time.Duration(min(60*(1<<(cache.Failures-1)), 21600)) * time.Second)
		} else {
			cache.Failures, cache.Error = 0, ""
			cache.Next = now.Add(time.Hour)
			if !unchanged {
				cache.ETag = headers.Get("ETag")
			}
		}
		results[id] = cache
		if priorError != cache.Error {
			message := cache.Error
			if message == "" {
				message = "resource refresh recovered"
			}
			events = append(events, sub.Run{Time: now, Operation: "resource", ObjectID: id, Message: message})
		}
	}
	for i, t := range config.Templates {
		refresh("template:"+t.ID, t.URL, func(data []byte) error {
			var updated sub.Template
			if err := strictSubscriptionJSON(data, &updated); err != nil {
				return err
			}
			if updated.ID != t.ID || updated.Client != t.Client {
				return errors.New("remote template cannot change ID or client")
			}
			updated.URL = t.URL
			if err := updated.Validate(); err != nil {
				return err
			}
			before, _ := json.Marshal(t)
			after, _ := json.Marshal(updated)
			config.Templates[i] = updated
			changed = changed || string(before) != string(after)
			return nil
		})
	}
	for i, r := range config.RuleSets {
		refresh("rules:"+r.ID, r.URL, func(data []byte) error {
			var rules []sub.Rule
			if err := strictSubscriptionJSON(data, &rules); err != nil {
				return err
			}
			if len(rules) > 4096 {
				return errors.New("rule limit exceeded")
			}
			for _, rule := range rules {
				if err := rule.Validate(); err != nil {
					return err
				}
			}
			before, _ := json.Marshal(r.Rules)
			after, _ := json.Marshal(rules)
			config.RuleSets[i].Rules = rules
			changed = changed || string(before) != string(after)
			return nil
		})
	}
	return changed, results, events
}
