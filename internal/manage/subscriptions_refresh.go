package manage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sck03/sing-box-vpn/internal/application"
	strict "github.com/sck03/sing-box-vpn/internal/encoding"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func fetchSubscription(ctx context.Context, client *http.Client, endpoint, etag string) ([]byte, http.Header, bool, error) {
	return fetchSubscriptionRequest(ctx, client, endpoint, etag, nil)
}

func fetchSubscriptionRequest(ctx context.Context, client *http.Client, endpoint, etag string, headers http.Header) ([]byte, http.Header, bool, error) {
	if err := sub.HTTPURL(endpoint); err != nil {
		return nil, nil, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, nil, false, errors.New("invalid source URL")
	}
	req.Header.Set("User-Agent", "sing-box-vpn/subscriptions")
	for key, values := range headers {
		req.Header[key] = append([]string(nil), values...)
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	requestClient := *client
	if len(headers) > 0 {
		originalRedirect := client.CheckRedirect
		requestClient.CheckRedirect = func(next *http.Request, via []*http.Request) error {
			if len(via) >= 3 || next.URL.Scheme != req.URL.Scheme || next.URL.Host != req.URL.Host {
				return errors.New("custom subscription headers require same-origin redirects")
			}
			if originalRedirect != nil {
				return originalRedirect(next, via)
			}
			return nil
		}
	}
	response, err := requestClient.Do(req)
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
	requestHeaders := http.Header{}
	if source.UserAgent != "" {
		requestHeaders.Set("User-Agent", source.UserAgent)
	}
	for key, value := range source.Headers {
		requestHeaders.Set(key, value)
	}
	data, headers, unchanged, err := fetchSubscriptionRequest(ctx, client, source.URL, old.ETag, requestHeaders)
	if err == nil && unchanged && len(old.Nodes) == 0 {
		err = errors.New("upstream returned 304 without a cached result")
	}
	if err == nil {
		nodes, usage, etag := old.Nodes, old.Usage, old.ETag
		if !unchanged {
			var parsed sub.ParseResult
			parsed, err = sub.ParseSource(data, source)
			nodes = parsed.Nodes
			if err == nil {
				old.Diagnostics = parsed.Diagnostics
			}
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
func refreshSubscriptionsResult(ctx context.Context, root, core string, force bool, report *application.RefreshResult) error {
	*report = application.RefreshResult{Failures: []application.RefreshFailure{}}
	defer func() {
		report.Interrupted = ctx.Err() != nil
		report.Complete()
	}()
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
	err = withStartupLock(ctx, root, func() error {
		var err error
		config, runtime, err = loadSubscriptionState(root)
		if err != nil {
			return err
		}
		_, _, err = confirmedSubscriptionConfig(ctx, root, config)
		return err
	})
	if err != nil {
		return err
	}
	report.ExpectedOutputs = len(config.Outputs)
	client := newHTTPClient(20 * time.Second)
	runtime = sub.PruneRuntime(config, runtime)
	defer client.CloseIdleConnections()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("too many redirects")
		}
		return sub.HTTPURL(req.URL.String())
	}
	now := time.Now()
	fetch, stop := context.WithTimeout(ctx, subscriptionSourceBudget)
	results, deferred, err := refreshSubscriptionSources(fetch, client, config.Sources, runtime.Sources, now, force)
	stop()
	if err != nil {
		return err
	}
	fetch, stop = context.WithTimeout(ctx, subscriptionResourceBudget)
	resourceChanged, resources, events := refreshSubscriptionResources(fetch, client, &config, runtime.Resources, now, force)
	stop()
	report.Deferred = deferred + dueSubscriptionResources(config, runtime.Resources, now, force) - len(resources)
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
	if err := ctx.Err(); err != nil {
		return errors.Join(err, checkpointSubscriptionSources(ctx, root, config, results))
	}
	events, err = publishSubscriptionRefresh(ctx, root, core, config, results, resources, events, now, resourceChanged, report, compilePreparedSubscriptions)
	if ctx.Err() != nil && !report.Published {
		return errors.Join(err, ctx.Err(), checkpointSubscriptionSources(ctx, root, config, results))
	}
	// Notifications cannot consume the remaining publication/task budget.
	notify, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for _, event := range events {
		if notify.Err() != nil {
			break
		}
		notifySubscriptionEvent(notify, client, root, config, event)
	}
	return err
}

func strictSubscriptionJSON(data []byte, target any) error {
	return strict.JSON(data, target, sub.MaxBody)
}
