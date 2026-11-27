package controlstore

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Shared by cancellation mutations and every public task projection.
const taskCancellable = "cancel_requested=0 AND ((state='queued' AND kind!='apply') OR (state='running' AND lane='probe'))"
const taskCancellationColumns = "cancel_requested,(" + taskCancellable + ")"

// Persist the execution-time recovery baseline before an apply changes files or
// services. Recovery retains the private input; terminal tasks erase it.
func (s *Store) CheckpointApply(ctx context.Context, id int64, input json.RawMessage) error {
	if !json.Valid(input) {
		return errors.New("invalid apply checkpoint")
	}
	r, err := s.db.ExecContext(ctx, `UPDATE control_tasks SET input=?,updated=? WHERE id=? AND kind='apply' AND state='running' AND cancel_requested=0`, []byte(input), time.Now().Unix(), id)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n != 1 {
		return ErrConflict
	}
	return err
}

func (s *Store) Finish(ctx context.Context, id int64, result error) error {
	state, detail := "succeeded", ""
	if result != nil {
		state, detail = "failed", result.Error()
		if len(detail) > 2048 {
			detail = detail[:2048]
		}
	}
	// Cancellation wins a race with completion, but keeps the lane occupied until
	// the worker has released its subprocesses, sockets and publication locks.
	_, err := s.db.ExecContext(ctx, `UPDATE control_tasks SET
 state=CASE WHEN cancel_requested=1 THEN 'cancelled' ELSE ? END,
 phase=CASE WHEN cancel_requested=1 THEN 'cancelled' WHEN ?='failed' THEN phase ELSE ? END,
 error=CASE WHEN cancel_requested=1 THEN '' ELSE ? END,updated=?,input='{}',
 outcome=CASE WHEN cancel_requested=1 THEN 'cancelled' WHEN ?='failed' AND kind='apply' THEN 'failed' ELSE outcome END,
 result=CASE WHEN cancel_requested=1 THEN '{}' WHEN ?='failed' AND kind='apply' THEN json_set(result,'$.outcome','failed') ELSE result END
 WHERE id=? AND state IN ('queued','running')`, state, state, state, detail, time.Now().Unix(), state, state, id)
	return err
}

// Only an interrupted apply is replayed. Cancellation requests survive restart.
func (s *Store) Recover(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE control_tasks SET
 state=CASE WHEN cancel_requested=1 THEN 'cancelled' WHEN kind='apply' THEN 'queued' ELSE 'failed' END,
 input=CASE WHEN kind='apply' THEN input ELSE CAST('{}' AS BLOB) END,
 phase=CASE WHEN cancel_requested=1 THEN 'cancelled' ELSE 'interrupted' END,
 error=CASE WHEN cancel_requested=1 THEN '' ELSE 'process interrupted; inspect state before retrying' END,
 outcome=CASE WHEN cancel_requested=1 THEN 'cancelled' ELSE outcome END,
 result=CASE WHEN cancel_requested=1 THEN '{}' ELSE result END,updated=?
 WHERE state='running' AND NOT (kind IN ('program-upgrade','panel-restart') AND phase='awaiting-panel-health')`, time.Now().Unix())
	return err
}

func (s *Store) Cancel(ctx context.Context, id int64) error {
	r, err := s.db.ExecContext(ctx, `UPDATE control_tasks SET
 cancel_requested=1,
 phase=CASE WHEN state='queued' THEN 'cancelled' ELSE 'cancelling' END,
 input=CASE WHEN state='queued' THEN '{}' ELSE input END,
 state=CASE WHEN state='queued' THEN 'cancelled' ELSE state END,updated=?
 WHERE id=? AND `+taskCancellable, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n != 1 {
		return errors.New("only queued non-configuration tasks or running probes can be cancelled")
	}
	return err
}

func (s *Store) CancellationRequested(ctx context.Context, id int64) (bool, error) {
	var requested bool
	err := s.db.QueryRowContext(ctx, "SELECT cancel_requested FROM control_tasks WHERE id=?", id).Scan(&requested)
	return requested, err
}
