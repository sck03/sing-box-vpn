package controlstore

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

func TestManualHistoryPreservesWork(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, err := s.Queue(t.Context(), "service", domain.TaskSubmission{Actor: "admin", Source: domain.TaskUser}, []byte("{}"), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteTask(t.Context(), id); err == nil {
		t.Fatal("deleted queued task")
	}
	if _, err = s.ClaimLane(t.Context(), "control"); err != nil {
		t.Fatal(err)
	}
	if err = s.Acknowledge(t.Context(), id); err == nil {
		t.Fatal("acknowledged running task")
	}
	// Broken maintenance cannot make finishing durable work fail.
	if err = s.Observe(t.Context(), "task-history-policy", "invalid"); err != nil {
		t.Fatal(err)
	}
	if err = s.Finish(t.Context(), id, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.PruneHistory(t.Context(), time.Now()); err == nil {
		t.Fatal("broken retention unexpectedly passed")
	}
	if task, err := s.Task(t.Context(), id); err != nil || task.State != "succeeded" {
		t.Fatal(task, err)
	}
	if err = s.SetHistoryPolicy(t.Context(), DefaultHistoryPolicy); err != nil {
		t.Fatal(err)
	}
	if err = s.Acknowledge(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	pending, err := s.Queue(t.Context(), "service", domain.TaskSubmission{Actor: "admin", Source: domain.TaskUser}, []byte("{}"), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := s.ClearTasks(t.Context(), true); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, err = s.Task(t.Context(), pending); err != nil {
		t.Fatal("pending work lost", err)
	}
}
func TestPanelRestartSurvivesRecovery(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, _ := s.Queue(t.Context(), "panel-restart", domain.TaskSubmission{Actor: "admin", Source: domain.TaskUser}, []byte("{}"), 0, nil)
	if _, err = s.ClaimLane(t.Context(), "control"); err != nil {
		t.Fatal(err)
	}
	if err = s.Phase(t.Context(), id, "awaiting-panel-health"); err != nil {
		t.Fatal(err)
	}
	if err = s.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	task, err := s.Task(t.Context(), id)
	if err != nil || task.State != "running" {
		t.Fatal(task, err)
	}
	if changed, err := s.CompletePanelRestart(t.Context(), id, []byte(`{"outcome":"passed"}`), "succeeded", ""); err != nil || !changed {
		t.Fatal(changed, err)
	}
	if changed, err := s.CompletePanelRestart(t.Context(), id, []byte("{}"), "rolled-back", "late result"); err != nil || changed {
		t.Fatal(changed, err)
	}
}

func TestHistoryAlwaysIncludesOldActiveTask(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.SetHistoryPolicy(t.Context(), HistoryPolicy{Count: 20, Days: 1, ResultBytes: 256 << 10}); err != nil {
		t.Fatal(err)
	}
	active, err := s.Queue(t.Context(), "service", domain.TaskSubmission{Actor: "admin", Source: domain.TaskUser}, []byte("{}"), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 60; i++ {
		id, err := s.Queue(t.Context(), "service", domain.TaskSubmission{Actor: "admin", Source: domain.TaskUser}, []byte("{}"), 0, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Finish(t.Context(), id, nil); err != nil {
			t.Fatal(err)
		}
	}
	tasks, err := s.RecentTaskSummaries(t.Context())
	if err != nil || len(tasks) == 0 || tasks[0].ID != active {
		t.Fatal(tasks, err)
	}
}
