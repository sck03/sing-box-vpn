package controlstore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

func (s *Store) Queue(ctx context.Context, kind string, submission domain.TaskSubmission, input []byte, expected int64, desired []byte) (int64, error) {
	return s.queue(ctx, "control", kind, submission, input, expected, desired)
}

func (s *Store) QueueTask(ctx context.Context, lane, kind string, submission domain.TaskSubmission, input []byte) (int64, error) {
	if !validLane(lane) {
		return 0, errors.New("unknown task lane")
	}
	return s.queue(ctx, lane, kind, submission, input, 0, nil)
}

func validLane(lane string) bool {
	return lane == domain.ControlLane || lane == domain.SubscriptionLane || lane == domain.ProbeLane
}

func (s *Store) queue(ctx context.Context, lane, kind string, submission domain.TaskSubmission, input []byte, expected int64, desired []byte) (int64, error) {
	if err := submission.Validate(); err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE control_state SET revision=revision WHERE id=1`); err != nil {
		return 0, err
	}
	if id, err := submittedTask(ctx, tx, submission); err != nil || id != 0 {
		return id, err
	}
	if submission.Source == domain.TaskScheduler {
		var id int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM control_tasks WHERE source='scheduler' AND actor=? AND lane=? AND kind=? AND input=? AND state IN ('queued','running') ORDER BY id LIMIT 1`, submission.Actor, lane, kind, input).Scan(&id)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
	}
	var pending int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM control_tasks WHERE state IN ('queued','running')`).Scan(&pending); err != nil {
		return 0, err
	}
	if pending >= 32 {
		return 0, errors.New("task queue is full")
	}
	// Leave capacity for configuration repair and automatic refresh under probe load.
	if lane == domain.ProbeLane {
		var probes int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM control_tasks WHERE lane='probe' AND state IN ('queued','running')").Scan(&probes); err != nil {
			return 0, err
		}
		if probes >= 8 {
			return 0, errors.New("probe queue is full")
		}
	}
	if desired != nil {
		// Serialize desired configuration changes with active side effects.
		var blocking int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM control_tasks WHERE state IN ('queued','running') AND lane='control'`).Scan(&blocking); err != nil {
			return 0, err
		}
		if blocking > 0 {
			return 0, errors.New("wait for the current task before saving configuration")
		}
		r, e := tx.ExecContext(ctx, `UPDATE control_state SET desired=?,revision=revision+1 WHERE id=1 AND revision=?`, desired, expected)
		if e != nil {
			return 0, e
		}
		n, e := r.RowsAffected()
		if e != nil {
			return 0, e
		}
		if n != 1 {
			return 0, ErrConflict
		}
	}
	now := time.Now().Unix()
	r, err := tx.ExecContext(ctx, `INSERT INTO control_tasks(kind,actor,source,lane,state,phase,created,updated,input) VALUES(?,?,?,?,'queued','queued',?,?,?)`, kind, submission.Actor, submission.Source, lane, now, now, input)
	if err != nil {
		return 0, err
	}
	id, err := r.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err = rememberSubmission(ctx, tx, submission, id, now); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}
