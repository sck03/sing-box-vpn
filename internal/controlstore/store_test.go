package controlstore

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestIdleClaimDoesNotWaitForAccountingWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Initialize(t.Context(), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	peer, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	writer, err := peer.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback()
	if _, err = writer.ExecContext(t.Context(), `UPDATE control_state SET revision=revision`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err = s.ClaimLane(ctx, "control"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("idle worker contended with writer: %v", err)
	}
}

func TestTaskRecoveryAndConfigurationRevisions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	ctx := t.Context()
	if e = s.Initialize(ctx, []byte(`{"value":1}`)); e != nil {
		t.Fatal(e)
	}
	id, e := s.Queue(ctx, "apply", "admin", []byte(`{"revision":2}`), 1, []byte(`{"value":2}`))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Queue(ctx, "apply", "admin", []byte(`{}`), 2, []byte(`{}`)); e == nil {
		t.Fatal("overlapping apply accepted")
	}
	if _, e = s.ClaimLane(ctx, "control"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.Recover(ctx); e != nil {
		t.Fatal(e)
	}
	task, e := s.ClaimLane(ctx, "control")
	if e != nil || task.ID != id || task.State != "running" || task.Phase != "validating" || string(task.Input) != `{"revision":2}` {
		t.Fatal(task, e)
	}
	state, _ := s.State(ctx)
	if state.AppliedRevision != 1 || state.Revision != 2 {
		t.Fatal(state)
	}
	if e = s.Applied(ctx, 2); e != nil {
		t.Fatal(e)
	}
	if e = s.Finish(ctx, id, nil); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Queue(ctx, "apply", "admin", []byte(`{}`), 1, []byte(`{}`)); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	id, e = s.Queue(ctx, "account", "admin", []byte(`{"secret":"pending"}`), 0, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ClaimLane(ctx, "control"); e != nil {
		t.Fatal(e)
	}
	if e = s.Recover(ctx); e != nil {
		t.Fatal(e)
	}
	tasks, e := s.RecentTaskSummaries(ctx)
	if e != nil || len(tasks) == 0 {
		t.Fatal(tasks, e)
	}
	if tasks[0].State != "failed" {
		t.Fatal("non-idempotent action replayed")
	}
	if e = s.Cancel(ctx, id); e == nil {
		t.Fatal("cancelled completed task")
	}
}
