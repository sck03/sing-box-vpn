package manage

import (
	"bytes"
	"context"
	"errors"
	"github.com/sck03/sing-box-vpn/internal/application"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestSubscriptionRequiredSourceApplyAndRefresh(t *testing.T) {
	root, core, _ := appFixture(t)
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
	if err := RunSubscriptions(context.Background(), []string{"--root", root, "--core", core, "apply", "--file", input}, nil, io.Discard); err != nil {
		t.Fatal(err)
	}
	_, state, _, err := loadSubscriptions(root)
	if err != nil || len(state.Sources["remote"].Nodes) != 1 {
		t.Fatal("required source was not prepared", err)
	}
	before := readBytes(t, filepath.Join(root, "proxies.yaml"))
	if !bytes.Contains(before, []byte("remote-secret")) {
		t.Fatal("local exports differ from snapshot")
	}
	if err := refreshSubscriptions(context.Background(), root, core, false); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatal("scheduler ignored Next")
	}
	if err := refreshSubscriptions(context.Background(), root, core, true); err != nil {
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
	if state.Sources["remote"].Failures != 1 || !bytes.Equal(before, readBytes(t, filepath.Join(root, "proxies.yaml"))) {
		t.Fatal("transient failure did not retain valid output")
	}
	if err := RunSubscriptions(context.Background(), []string{"--root", root, "--core", core, "apply", "--file", input}, nil, io.Discard); err == nil {
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

type fakeSubscriptionService struct {
	applied, restored bool
	fail              error
}

func (s *fakeSubscriptionService) state(context.Context) (bool, bool, error) { return true, true, nil }
func (s *fakeSubscriptionService) apply(_ context.Context, _, _ bool) error {
	s.applied = true
	return s.fail
}
func (s *fakeSubscriptionService) restore(_ context.Context, active, enabled bool) error {
	s.restored = active && enabled
	return nil
}

func TestSubscriptionServiceRollback(t *testing.T) {
	root := t.TempDir()
	subscriptionFixture(t, root, strings.Repeat("a", 64), "old")
	before := readBytes(t, filepath.Join(root, "subscription.json"))
	configBefore := readBytes(t, filepath.Join(root, subscriptionsConfig))
	service := &fakeSubscriptionService{fail: errors.New("start failed")}
	err := applySubscriptionChange(context.Background(), root, func() error {
		return AtomicJSON(filepath.Join(root, "subscription.json"), subscriptionSettings{Enabled: false})
	}, service)
	if err == nil || !service.applied || !service.restored || !bytes.Equal(before, readBytes(t, filepath.Join(root, "subscription.json"))) || !bytes.Equal(configBefore, readBytes(t, filepath.Join(root, subscriptionsConfig))) {
		t.Fatal("subscription rollback failed", err)
	}
}

type fakeRestoreServices struct {
	before, after map[string]unitState
	failOnce      bool
	stops         int
}

func (s *fakeRestoreServices) states(context.Context) (map[string]unitState, error) {
	return s.before, nil
}
func (s *fakeRestoreServices) stop(context.Context, map[string]unitState) error {
	s.stops++
	return nil
}
func (s *fakeRestoreServices) apply(_ context.Context, states, _ map[string]unitState) error {
	if s.failOnce {
		s.failOnce = false
		return errors.New("restart failed")
	}
	s.after = states
	return nil
}
func TestSubscriptionRestoreTransaction(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "rollback"}[failure], func(t *testing.T) {
			root := t.TempDir()
			if err := AtomicBytes(filepath.Join(root, "sb.json"), []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			service := &fakeRestoreServices{before: map[string]unitState{"sing-box": {true, true}, "subscription": {true, true}, "vpngate": {true, true}}, failOnce: failure}
			err := applySubscriptionRestore(context.Background(), root, fileBundle{"sb.json": []byte("new"), "new.json": []byte("new")}, service)
			if failure {
				if err == nil || string(readBytes(t, filepath.Join(root, "sb.json"))) != "old" || !reflect.DeepEqual(service.after, service.before) {
					t.Fatal("restore rollback failed", err)
				}
				if _, err := os.Stat(filepath.Join(root, "new.json")); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("new file survived rollback")
				}
			} else if err != nil || service.after["subscription"].Active || service.after["vpngate"].Enabled || !service.after["sing-box"].Active {
				t.Fatal("restore state invalid", err)
			}
		})
	}
}
