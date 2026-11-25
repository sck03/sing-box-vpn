package manage

import (
	"bytes"
	"context"

	"github.com/sck03/sing-box-vpn/internal/application"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"

	"sync/atomic"
	"testing"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestSubscriptionRequiredSourceApplyAndRefresh(t *testing.T) {
	root, core, _ := appFixture(t)
	service, config, _ := platformFixture(t, root, core)
	var requests atomic.Int32
	var fail atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(304)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("subscription-userinfo", "download=10; total=1000; expire=2000000000")
		_, _ = io.WriteString(w, "trojan://remote-secret@example.com:443#remote")
	}))
	defer upstream.Close()
	c, _, _, err := loadSubscriptions(root)
	if err != nil {
		t.Fatal(err)
	}
	c.Sources = append(c.Sources, sub.Source{ID: "remote", Kind: "remote", Format: "uri", URL: upstream.URL, Required: true})
	c.Collections[0].Sources = append(c.Collections[0].Sources, "remote")
	input := filepath.Join(t.TempDir(), "config.json")
	if err := AtomicJSON(input, c); err != nil {
		t.Fatal(err)
	}
	config.Subscriptions = c
	if err := applyFixture(t, service, config, 1); err != nil {
		t.Fatal(err)
	}
	_, state, _, err := loadSubscriptions(root)
	if err != nil || len(state.Sources["remote"].Nodes) != 1 {
		t.Fatal("required source was not prepared", err)
	}
	before := readArtifact(t, root, "normal-provider")
	if !bytes.Contains(before, []byte("remote-secret")) {
		t.Fatal("local exports differ from snapshot")
	}
	if err := refreshSubscriptions(t, context.Background(), root, core, false); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatal("scheduler ignored Next")
	}
	if err := refreshSubscriptions(t, context.Background(), root, core, true); err != nil {
		t.Fatal(err)
	}
	_, state, _, _ = loadSubscriptions(root)
	if requests.Load() != 2 || state.Sources["remote"].Usage.Download == nil {
		t.Fatal("304 lost cached nodes/usage")
	}
	fail.Store(true)
	var report application.RefreshResult
	if err := refreshSubscriptionsResult(context.Background(), root, core, true, &report); err != nil {
		t.Fatal(err)
	}
	if report.Outcome != "cached" || len(report.Failures) != 1 || report.Outputs == 0 {
		t.Fatal("upstream failure hidden", report)
	}
	_, state, _, _ = loadSubscriptions(root)
	if state.Sources["remote"].Failures != 1 || !bytes.Equal(before, readArtifact(t, root, "normal-provider")) {
		t.Fatal("transient failure did not retain valid output")
	}
	if _, err := service.QueueApply(t.Context(), config, 1, "test"); err == nil {
		t.Fatal("stale revision accepted")
	}
}

func TestSubscriptionResourceScheduleAndRejection(t *testing.T) {
	var count int
	bad := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if bad {
			_, _ = io.WriteString(w, `[{"type":"execute","value":"bad","target":"DIRECT"}]`)
			return
		}
		if r.Header.Get("If-None-Match") == "rules-v1" {
			w.WriteHeader(304)
			return
		}
		w.Header().Set("ETag", "rules-v1")
		_, _ = io.WriteString(w, `[{"type":"domain","value":"example.com","target":"PROXY"}]`)
	}))
	defer server.Close()
	c := fixtureSubscriptionConfig()
	c.RuleSets = []sub.RuleSet{{ID: "rules", URL: server.URL}}
	now := time.Now()
	changed, state, _ := refreshSubscriptionResources(context.Background(), server.Client(), &c, nil, now, false)
	effective, err := sub.ResolveResources(c, state)
	if !changed || count != 1 || err != nil || len(effective.RuleSets[0].Rules) != 1 || len(c.RuleSets[0].Rules) != 0 {
		t.Fatal("scheduled resource did not refresh")
	}
	changed, next, _ := refreshSubscriptionResources(context.Background(), server.Client(), &c, state, now.Add(time.Minute), false)
	if changed || len(next) != 0 || count != 1 {
		t.Fatal("resource refreshed before deadline")
	}
	changed, next, _ = refreshSubscriptionResources(context.Background(), server.Client(), &c, state, now.Add(time.Hour), false)
	if changed || next["rules:rules"].Failures != 0 {
		t.Fatal("resource 304 failed")
	}
	bad = true
	changed, next, events := refreshSubscriptionResources(context.Background(), server.Client(), &c, next, now.Add(2*time.Hour), false)
	effective, err = sub.ResolveResources(c, next)
	if changed || err != nil || len(effective.RuleSets[0].Rules) != 1 || next["rules:rules"].Failures != 1 || len(events) != 1 {
		t.Fatal("bad rules replaced valid rules")
	}
}
