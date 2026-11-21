package manage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestRefreshFetchesBoundConcurrencyAndKeepCompletedProgress(t *testing.T) {
	var active, peak atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		if r.URL.Path == "/fast" {
			io.WriteString(w, "trojan://secret@example.com:443#node")
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	sources := []sub.Source{{ID: "fast", Kind: "remote", Format: "uri", URL: server.URL + "/fast"}}
	for i := range 32 {
		sources = append(sources, sub.Source{ID: fmt.Sprint("slow-", i), Kind: "remote", Format: "uri", URL: server.URL + "/slow"})
	}
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	result, deferred, err := refreshSubscriptionSources(ctx, server.Client(), sources, nil, time.Now(), false)
	if err != nil || len(result) != 1 || result["fast"].Error != "" || deferred != 32 {
		t.Fatal(result, deferred, err)
	}
	if peak.Load() > subscriptionFetchConcurrency {
		t.Fatal("unbounded fetches", peak.Load())
	}
	if result["fast"].Next.IsZero() {
		t.Fatal("completed progress not scheduled")
	}
}

func TestFetchQueuePrioritizesOldestInputs(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan int, 8)
	jobs := make([]subscriptionFetchJob, 8)
	for i := range jobs {
		jobs[i] = subscriptionFetchJob{checked: time.Unix(int64(8-i), 0), run: func() { started <- i; <-ctx.Done() }}
	}
	done := make(chan struct{})
	go func() { runSubscriptionFetches(ctx, jobs); close(done) }()
	for range subscriptionFetchConcurrency {
		select {
		case i := <-started:
			if i < 4 {
				t.Fatal("newer input skipped older work", i)
			}
		case <-time.After(time.Second):
			t.Fatal("workers did not start")
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("workers leaked")
	}
}

func TestResourceRefreshIsConcurrentAndRetainsCancelledCache(t *testing.T) {
	var active, peak atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		if r.URL.Path == "/fast" {
			io.WriteString(w, `[{"type":"domain","value":"example.com","target":"DIRECT"}]`)
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	c := sub.Config{RuleSets: []sub.RuleSet{{ID: "fast", URL: server.URL + "/fast"}}}
	for i := range 16 {
		c.RuleSets = append(c.RuleSets, sub.RuleSet{ID: fmt.Sprint("slow-", i), URL: server.URL + "/slow"})
	}
	previous := map[string]sub.ResourceCache{"rules:slow-0": {URL: server.URL + "/slow", Body: []byte(`[]`), ETag: "old"}}
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	changed, results, _ := refreshSubscriptionResources(ctx, server.Client(), &c, previous, time.Now(), true)
	if !changed || len(results) != 1 || results["rules:fast"].Error != "" || peak.Load() > subscriptionFetchConcurrency {
		t.Fatal(changed, results, peak.Load())
	}
	if previous["rules:slow-0"].ETag != "old" || previous["rules:slow-0"].Error != "" {
		t.Fatal("cancelled resource changed cache", previous)
	}
}
