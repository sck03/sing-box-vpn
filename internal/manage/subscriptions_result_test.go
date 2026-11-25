package manage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/application"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestRefreshTasksPersistRejectedResourceReport(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(map[bool]string{false: "manual", true: "scheduled"}[automatic], func(t *testing.T) {
			root, core, _ := appFixture(t)
			s, _, _ := platformFixture(t, root, core)
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, `[{"type":"domain","value":"example.com","target":"missing-policy-group"}]`)
			}))
			defer remote.Close()
			config, _, err := loadSubscriptionState(root)
			if err != nil {
				t.Fatal(err)
			}
			config.RuleSets = append(config.RuleSets, sub.RuleSet{ID: "audit-rules", URL: remote.URL})
			for i := range config.Templates {
				if config.Templates[i].ID == "normal-sing-box" {
					config.Templates[i].RuleSets = append(config.Templates[i].RuleSets, "audit-rules")
				}
			}
			if err = AtomicJSON(filepath.Join(root, subscriptionsConfig), config); err != nil {
				t.Fatal(err)
			}
			command := application.TaskCommand{Operation: "subscription", Subscription: application.SubscriptionCommand{Operation: "refresh"}}
			confirmSubscriptionFixture(t, root)
			id, err := s.queueTask(t.Context(), taskInput{TaskCommand: command, Automatic: automatic}, "admin")
			if err != nil {
				t.Fatal(err)
			}
			task, err := s.Store.ClaimLane(t.Context(), "subscription")
			if err != nil || task.ID != id {
				t.Fatal(task, err)
			}
			if err = s.runTask(t.Context(), task); err != nil {
				t.Fatal(err)
			}
			task, err = s.Store.Task(t.Context(), id)
			var result application.RefreshResult
			if err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(task.Result, &result); err != nil {
				t.Fatal(err)
			}
			if task.State != "failed" || task.Outcome != "cached" || !result.Published || result.Checked != 1 || result.Updated != 0 || len(result.Failures) != 1 || result.Outputs == 0 {
				t.Fatal(task.State, task.Outcome, string(task.Result))
			}
		})
	}
}

func TestRefreshCheckpointPreservesCompletedSourcesWithoutPublishing(t *testing.T) {
	root, core, _ := appFixture(t)
	config, _, err := loadSubscriptionState(root)
	if err != nil {
		t.Fatal(err)
	}
	source := sub.Source{ID: "remote", Kind: "remote", URL: "https://example.invalid/sub", Format: "uri"}
	config.Sources = append(config.Sources, source)
	if err = AtomicJSON(filepath.Join(root, subscriptionsConfig), config); err != nil {
		t.Fatal(err)
	}
	before := readArtifact(t, root, "normal-provider")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	now := time.Now().UTC()
	cache := map[string]sub.SourceCache{"remote": {SourceHash: sub.SourceHash(source), Checked: now, Success: now, Next: now.Add(time.Hour)}}
	if err = checkpointSubscriptionSources(ctx, root, config, cache); err != nil {
		t.Fatal(err)
	}
	_, state, err := loadSubscriptionState(root)
	if err != nil || !state.Sources["remote"].Success.Equal(now) {
		t.Fatal(state.Sources, err)
	}
	if !bytes.Equal(before, readArtifact(t, root, "normal-provider")) {
		t.Fatal("checkpoint published an artifact")
	}
	_, schedule, err := loadSubscriptionSchedule(root)
	if err != nil || !state.RefreshPending || !schedule.RefreshPending {
		t.Fatal("checkpoint would never publish", schedule, err)
	}
	var report application.RefreshResult
	confirmSubscriptionFixture(t, root)
	if err = refreshSubscriptionsResult(t.Context(), root, core, false, &report); err != nil || !report.Published || report.Checked != 0 {
		t.Fatal("checkpoint fetched completed sources again", report, err)
	}
	_, schedule, err = loadSubscriptionSchedule(root)
	if err != nil || schedule.RefreshPending {
		t.Fatal("published checkpoint remains pending", err)
	}
	stale := config
	config.Revision++
	if err = AtomicJSON(filepath.Join(root, subscriptionsConfig), config); err != nil {
		t.Fatal(err)
	}
	if err = checkpointSubscriptionSources(ctx, root, stale, cache); !errors.Is(err, application.ErrConflict) {
		t.Fatal(err)
	}
}
