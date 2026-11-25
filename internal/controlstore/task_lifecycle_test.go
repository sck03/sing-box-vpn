package controlstore

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func cancellationStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err = s.Initialize(t.Context(), []byte("{}")); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRunningProbeCancellationRetainsLaneUntilFinished(t *testing.T) {
	s := cancellationStore(t)
	ctx := t.Context()
	id, err := s.QueueTask(ctx, "probe", "outbound-probe", "admin", []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClaimLane(ctx, "probe"); err != nil {
		t.Fatal(err)
	}
	task, err := s.Task(ctx, id)
	if err != nil || !task.Cancellable {
		t.Fatal(task, err)
	}
	if err = s.Cancel(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err = s.QueueTask(ctx, "probe", "outbound-probe", "admin", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClaimLane(ctx, "probe"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cancelled worker released lane too early", err)
	}
	if err = s.Phase(ctx, id, "late-phase"); err != nil {
		t.Fatal(err)
	}
	if err = s.Result(ctx, id, []byte(`{"secret":"late-result"}`)); err != nil {
		t.Fatal(err)
	}
	task, err = s.Task(ctx, id)
	if err != nil || task.Phase != "cancelling" || task.Cancellable || !task.CancelRequested {
		t.Fatal(task, err)
	}
	if err = s.Finish(ctx, id, nil); err != nil {
		t.Fatal(err)
	}
	task, err = s.Task(ctx, id)
	if err != nil || task.State != "cancelled" || string(task.Result) != "{}" {
		t.Fatal(task, err)
	}
	if _, err = s.ClaimLane(ctx, "probe"); err != nil {
		t.Fatal(err)
	}
	if err = s.Finish(ctx, id, errors.New("late failure")); err != nil {
		t.Fatal(err)
	}
	task, _ = s.Task(ctx, id)
	if task.State != "cancelled" {
		t.Fatal(task)
	}
}

func TestCancellationRecoveryAndSideEffectBoundary(t *testing.T) {
	s := cancellationStore(t)
	ctx := t.Context()
	for _, lane := range []string{"control", "subscription", "probe"} {
		id, err := s.QueueTask(ctx, lane, "subscription", "admin", []byte("{}"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.ClaimLane(ctx, lane); err != nil {
			t.Fatal(err)
		}
		err = s.Cancel(ctx, id)
		if lane != "probe" {
			if err == nil {
				t.Fatal("cancelled side-effecting task", lane)
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Recover(ctx); err != nil {
			t.Fatal(err)
		}
		task, err := s.Task(ctx, id)
		if err != nil || task.State != "cancelled" || task.Error != "" {
			t.Fatal(task, err)
		}
	}
}

func TestConcurrentClaimsRespectLaneExclusion(t *testing.T) {
	s := cancellationStore(t)
	for range 4 {
		if _, err := s.QueueTask(t.Context(), "probe", "outbound-probe", "admin", []byte("{}")); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for range 8 {
		wg.Go(func() { _, err := s.ClaimLane(context.Background(), "probe"); results <- err })
	}
	wg.Wait()
	close(results)
	claimed := 0
	for err := range results {
		if err == nil {
			claimed++
		} else if !errors.Is(err, sql.ErrNoRows) {
			t.Fatal(err)
		}
	}
	if claimed != 1 {
		t.Fatalf("claimed %d probes concurrently", claimed)
	}
}

func TestProbeQueueLeavesCapacityForOtherWork(t *testing.T) {
	s := cancellationStore(t)
	for range 8 {
		if _, err := s.QueueTask(t.Context(), "probe", "outbound-probe", "admin", []byte("{}")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.QueueTask(t.Context(), "probe", "outbound-probe", "admin", []byte("{}")); err == nil {
		t.Fatal("probe budget exceeded")
	}
	for _, lane := range []string{"control", "subscription"} {
		if _, err := s.QueueTask(t.Context(), lane, "service", "admin", []byte("{}")); err != nil {
			t.Fatal(err)
		}
	}
}
