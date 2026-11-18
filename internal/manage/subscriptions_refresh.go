package manage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sck03/sing-box-vpn/internal/application"
	strict "github.com/sck03/sing-box-vpn/internal/encoding"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func fetchSubscription(ctx context.Context, client *http.Client, endpoint, etag string) ([]byte, http.Header, bool, error) {
	if err := sub.HTTPURL(endpoint); err != nil {
		return nil, nil, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, nil, false, errors.New("invalid source URL")
	}
	req.Header.Set("User-Agent", "sing-box-vpn/subscriptions")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, nil, false, errors.New("subscription request failed or timed out")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified {
		return nil, response.Header, true, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, nil, false, fmt.Errorf("subscription HTTP %d", response.StatusCode)
	}
	if response.ContentLength > sub.MaxBody {
		return nil, nil, false, errors.New("subscription response too large")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, sub.MaxBody+1))
	if err != nil {
		return nil, nil, false, errors.New("subscription response interrupted")
	}
	if len(data) > sub.MaxBody {
		return nil, nil, false, errors.New("subscription response too large")
	}
	return data, response.Header, false, nil
}

func parseSubscriptionUsage(header string) (sub.Usage, error) {
	u := sub.Usage{}
	if len(header) > 8192 {
		return u, errors.New("upstream usage header too large")
	}
	seen := map[string]bool{}
	for item := range strings.SplitSeq(header, ";") {
		key, value, found := strings.Cut(strings.TrimSpace(item), "=")
		if !found {
			if key == "upload" || key == "download" || key == "total" || key == "expire" {
				return u, errors.New("incomplete upstream usage field")
			}
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if key == "upload" || key == "download" || key == "total" || key == "expire" {
			if seen[key] {
				return u, errors.New("duplicate upstream usage field")
			}
			seen[key] = true
		}
		if key == "expire" {
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil || n < 0 {
				return u, errors.New("invalid upstream expiration")
			}
			u.Expire = &n
			continue
		}
		if key != "upload" && key != "download" && key != "total" {
			continue
		}
		n, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return u, errors.New("invalid upstream traffic information")
		}
		switch key {
		case "upload":
			u.Upload = &n
		case "download":
			u.Download = &n
		case "total":
			u.Total = &n
		}
	}
	return u, nil
}

func formatSubscriptionUsage(u sub.Usage) string {
	values := []string{}
	if u.Upload != nil {
		values = append(values, "upload="+strconv.FormatUint(*u.Upload, 10))
	}
	if u.Download != nil {
		values = append(values, "download="+strconv.FormatUint(*u.Download, 10))
	}
	if u.Total != nil {
		values = append(values, "total="+strconv.FormatUint(*u.Total, 10))
	}
	if u.Expire != nil {
		values = append(values, "expire="+strconv.FormatInt(*u.Expire, 10))
	}
	return strings.Join(values, "; ")
}

func refreshSource(ctx context.Context, client *http.Client, source sub.Source, old sub.SourceCache, now time.Time) sub.SourceCache {
	if old.SourceHash != sub.SourceHash(source) {
		old = sub.SourceCache{SourceHash: sub.SourceHash(source)}
	}
	old.Checked = now
	data, headers, unchanged, err := fetchSubscription(ctx, client, source.URL, old.ETag)
	if err == nil && unchanged && len(old.Nodes) == 0 {
		err = errors.New("upstream returned 304 without a cached result")
	}
	if err == nil {
		nodes, usage, etag := old.Nodes, old.Usage, old.ETag
		if !unchanged {
			nodes, err = sub.Parse(data, source.Format, source.ID)
			usage, etag = sub.Usage{}, headers.Get("ETag")
		}
		if err == nil {
			var update sub.Usage
			update, err = parseSubscriptionUsage(headers.Get("subscription-userinfo"))
			if err == nil {
				if update.Upload != nil {
					usage.Upload = update.Upload
				}
				if update.Download != nil {
					usage.Download = update.Download
				}
				if update.Total != nil {
					usage.Total = update.Total
				}
				if update.Expire != nil {
					usage.Expire = update.Expire
				}
				if headers.Get("ETag") != "" {
					etag = headers.Get("ETag")
				}
				old.Nodes, old.Usage, old.ETag = nodes, usage, etag
			}
		}
	}
	interval := source.IntervalSeconds
	if interval == 0 {
		interval = 3600
	}
	if err != nil {
		old.Failures++
		if old.Failures > 10 {
			old.Failures = 10
		}
		old.Error = err.Error()
		backoff := 60 * (1 << (old.Failures - 1))
		if backoff > 21600 {
			backoff = 21600
		}
		interval = backoff
	} else {
		old.Success, old.Error, old.Failures = now, "", 0
	}
	old.Next = now.Add(time.Duration(interval) * time.Second)
	return old
}

// The separate cross-process lock coalesces scheduled and manual refreshes. It
// never holds the configuration lock while waiting on a remote server.
func refreshSubscriptions(ctx context.Context, root, core string, force bool) error {
	return refreshSubscriptionsResult(ctx, root, core, force, &application.RefreshResult{})
}

func refreshSubscriptionsResult(ctx context.Context, root, core string, force bool, report *application.RefreshResult) error {
	if err := os.MkdirAll(filepath.Join(root, "subscriptions"), 0700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(root, "subscriptions", ".refresh.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := lockFile(lock); err != nil {
		return fmt.Errorf("订阅刷新已在运行: %w", err)
	}
	var config sub.Config
	var runtime sub.Runtime
	err = withStartupLock(ctx, root, func() error { var e error; config, runtime, _, e = loadSubscriptions(root); return e })
	if err != nil {
		return err
	}
	client := newHTTPClient(20 * time.Second)
	runtime = sub.PruneRuntime(config, runtime)
	defer client.CloseIdleConnections()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("too many redirects")
		}
		return sub.HTTPURL(req.URL.String())
	}
	results := map[string]sub.SourceCache{}
	cacheBytes, cacheNodes := 0, 0
	cacheSizes := map[string]int{}
	for id, cache := range runtime.Sources {
		data, err := json.Marshal(cache)
		if err != nil {
			return err
		}
		cacheSizes[id] = len(data)
		cacheBytes += len(data)
		cacheNodes += len(cache.Nodes)
	}
	if cacheBytes > sub.MaxBody || cacheNodes > sub.MaxNodes {
		return errors.New("combined source cache limit exceeded")
	}
	var mu sync.Mutex
	var workers sync.WaitGroup
	jobs := make(chan sub.Source)
	now := time.Now()
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for source := range jobs {
				result := refreshSource(ctx, client, source, runtime.Sources[source.ID], now)
				mu.Lock()
				data, _ := json.Marshal(result)
				old := runtime.Sources[source.ID]
				if cacheBytes-cacheSizes[source.ID]+len(data) > sub.MaxBody || cacheNodes-len(old.Nodes)+len(result.Nodes) > sub.MaxNodes {
					result = old
					result.Checked, result.Next, result.Error = now, now.Add(time.Hour), "combined source cache limit exceeded"
					result.Failures++
					data, _ = json.Marshal(result)
				}
				cacheBytes += len(data) - cacheSizes[source.ID]
				cacheNodes += len(result.Nodes) - len(old.Nodes)
				results[source.ID] = result
				mu.Unlock()
			}
		}()
	}
	for _, source := range config.Sources {
		if source.Kind != "remote" || (!force && now.Before(runtime.Sources[source.ID].Next)) {
			continue
		}
		select {
		case jobs <- source:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
	}
	close(jobs)
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	resourceChanged, resources, events := refreshSubscriptionResources(ctx, client, &config, runtime.Resources, now, force)
	report.Failures = []application.RefreshFailure{}
	for id, value := range results {
		report.Checked++
		if value.Error != "" {
			report.Failures = append(report.Failures, application.RefreshFailure{ID: id, Error: value.Error, LastSuccess: value.Success})
		} else {
			report.Updated++
		}
	}
	for id, value := range resources {
		report.Checked++
		if value.Error != "" {
			report.Failures = append(report.Failures, application.RefreshFailure{ID: id, Error: value.Error})
		} else {
			report.Updated++
		}
	}
	sort.Slice(report.Failures, func(i, j int) bool { return report.Failures[i].ID < report.Failures[j].ID })
	err = withLock(root, func() error {
		current, state, local, err := loadSubscriptions(root)
		if err != nil {
			return err
		}
		if err := validateSubscriptionsRevision(current, config); err != nil {
			return err
		}
		if state.Sources == nil {
			state.Sources = map[string]sub.SourceCache{}
		}
		if state.Resources == nil {
			state.Resources = map[string]sub.ResourceCache{}
		}
		for id, value := range resources {
			state.Resources[id] = value
		}
		for _, event := range events {
			state.Record(event.Operation, event.ObjectID, event.Message, now)
		}
		for id, value := range results {
			old := state.Sources[id]
			if expiry := value.Usage.Expire; expiry != nil && *expiry > 0 && *expiry <= now.Add(72*time.Hour).Unix() && value.NotifiedExpiry != *expiry {
				value.NotifiedExpiry = *expiry
				events = append(events, sub.Run{Time: now, Operation: "expiry", ObjectID: id, Message: "source expires at " + time.Unix(*expiry, 0).UTC().Format(time.RFC3339)})
			}
			state.Sources[id] = value
			message := "refresh succeeded"
			if value.Error != "" {
				message = value.Error
			}
			state.Record("refresh", id, message, now)
			if old.Error != value.Error && (old.Error != "" || value.Error != "") {
				events = append(events, sub.Run{Time: now, Operation: "refresh", ObjectID: id, Message: message})
			}
		}
		mode := sub.Available
		if resourceChanged {
			mode = sub.Strict
		}
		files, snapshot, _, buildErr := compileSubscriptions(ctx, root, core, current, state, local, mode)
		report.Outputs, report.ExpectedOutputs = len(snapshot.Artifacts), len(current.Outputs)
		if buildErr != nil {
			// A rejected resource update must be fetched again; do not retain its ETag.
			for id := range resources {
				previous := runtime.Resources[id]
				previous.URL = resources[id].URL
				previous.Checked = now
				previous.Error = "resource update rejected by output validation"
				previous.Failures = min(previous.Failures+1, 10)
				previous.Next = now.Add(time.Duration(min(60*(1<<(previous.Failures-1)), 21600)) * time.Second)
				state.Resources[id] = previous
			}
			state.Record("publish", "", buildErr.Error(), now)
			if err := AtomicJSON(filepath.Join(root, subscriptionsRuntime), state); err != nil {
				return errors.Join(buildErr, err)
			}
			return buildErr
		}
		return publish(root, files)
	})
	report.Complete()
	for _, event := range events {
		notifySubscriptionEvent(ctx, client, root, config, event)
	}
	return err
}

func strictSubscriptionJSON(data []byte, target any) error {
	return strict.JSON(data, target, sub.MaxBody)
}
