package manage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"sync"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

const (
	subscriptionFetchConcurrency = 4
	subscriptionSourceBudget     = 90 * time.Second
	subscriptionResourceBudget   = 45 * time.Second
)

type subscriptionFetchJob struct {
	checked time.Time
	run     func()
}

// Oldest/never checked inputs go first, so bounded rounds make fair progress.
// Jobs own network work; only their short cache merge needs synchronization.
func runSubscriptionFetches(ctx context.Context, jobs []subscriptionFetchJob) {
	sort.SliceStable(jobs, func(i, j int) bool { return jobs[i].checked.Before(jobs[j].checked) })
	queue := make(chan subscriptionFetchJob)
	var workers sync.WaitGroup
	for range min(subscriptionFetchConcurrency, len(jobs)) {
		workers.Go(func() {
			for job := range queue {
				if ctx.Err() == nil {
					job.run()
				}
			}
		})
	}
dispatch:
	for _, job := range jobs {
		select {
		case <-ctx.Done():
			break dispatch
		case queue <- job:
		}
	}
	close(queue)
	workers.Wait()
}

func refreshSubscriptionSources(ctx context.Context, client *http.Client, sources []sub.Source, old map[string]sub.SourceCache, now time.Time, force bool) (map[string]sub.SourceCache, int, error) {
	results := map[string]sub.SourceCache{}
	cacheBytes, cacheNodes := 0, 0
	sizes := make(map[string]int, len(old))
	for id, entry := range old {
		data, err := json.Marshal(entry)
		if err != nil {
			return nil, 0, err
		}
		sizes[id] = len(data)
		cacheBytes += len(data)
		cacheNodes += len(entry.Nodes)
	}
	if cacheBytes > sub.MaxBody || cacheNodes > sub.MaxNodes {
		return nil, 0, errors.New("combined source cache limit exceeded")
	}
	var mu sync.Mutex
	var jobs []subscriptionFetchJob
	for _, source := range sources {
		if source.Kind != "remote" || !force && now.Before(old[source.ID].Next) {
			continue
		}
		jobs = append(jobs, subscriptionFetchJob{old[source.ID].Checked, func() {
			result := refreshSource(ctx, client, source, old[source.ID], now)
			// A round deadline is not an upstream failure. Leave interrupted inputs
			// due for the next round instead of poisoning their cache/backoff.
			if ctx.Err() != nil {
				return
			}
			data, _ := json.Marshal(result)
			mu.Lock()
			defer mu.Unlock()
			previous := old[source.ID]
			if cacheBytes-sizes[source.ID]+len(data) > sub.MaxBody || cacheNodes-len(previous.Nodes)+len(result.Nodes) > sub.MaxNodes {
				result = previous
				result.Checked, result.Next, result.Error = now, now.Add(time.Hour), "combined source cache limit exceeded"
				result.Failures++
				data, _ = json.Marshal(result)
			}
			cacheBytes += len(data) - sizes[source.ID]
			cacheNodes += len(result.Nodes) - len(previous.Nodes)
			results[source.ID] = result
		}})
	}
	runSubscriptionFetches(ctx, jobs)
	return results, len(jobs) - len(results), nil
}

func dueSubscriptionResources(c sub.Config, old map[string]sub.ResourceCache, now time.Time, force bool) int {
	count := 0
	due := func(id, endpoint string) {
		if endpoint != "" && (force || old[id].URL != endpoint || !now.Before(old[id].Next)) {
			count++
		}
	}
	for _, t := range c.Templates {
		due("template:"+t.ID, t.URL)
	}
	for _, r := range c.RuleSets {
		due("rules:"+r.ID, r.URL)
	}
	return count
}
