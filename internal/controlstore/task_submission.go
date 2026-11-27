package controlstore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

var ErrRequestConflict = errors.New("request identity was already used for another task")
var ErrRequestExpired = errors.New("previous task was accepted but its history has been removed; verify its result before submitting a new operation")
var ErrRequestLimit = errors.New("task request receipt capacity reached; retry after maintenance")

const requestRetention = 24 * time.Hour
const maxTaskRequests = 10000

type submissionReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// SubmittedTask avoids repeating preparation. Queue rechecks under the write
// transaction, so concurrent callers and other processes cannot admit it twice.
func (s *Store) SubmittedTask(ctx context.Context, submission domain.TaskSubmission) (int64, error) {
	if err := submission.Validate(); err != nil {
		return 0, err
	}
	return submittedTask(ctx, s.db, submission)
}

func submittedTask(ctx context.Context, db submissionReader, submission domain.TaskSubmission) (int64, error) {
	if submission.RequestID == "" {
		return 0, nil
	}
	var fingerprint string
	var id sql.NullInt64
	err := db.QueryRowContext(ctx, `SELECT r.fingerprint,t.id FROM task_requests r LEFT JOIN control_tasks t ON t.id=r.task_id WHERE r.actor=? AND r.request_id=?`, submission.Actor, submission.RequestID).Scan(&fingerprint, &id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if fingerprint != submission.Fingerprint {
		return 0, ErrRequestConflict
	}
	if !id.Valid {
		return 0, ErrRequestExpired
	}
	return id.Int64, nil
}

func pruneTaskRequests(ctx context.Context, tx *sql.Tx, now time.Time) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM task_requests WHERE created<? AND NOT EXISTS (SELECT 1 FROM control_tasks t WHERE t.id=task_requests.task_id AND t.state IN ('queued','running'))`, now.Add(-requestRetention).Unix())
	return err
}

func rememberSubmission(ctx context.Context, tx *sql.Tx, submission domain.TaskSubmission, id, now int64) error {
	if submission.RequestID == "" {
		return nil
	}
	if err := pruneTaskRequests(ctx, tx, time.Unix(now, 0)); err != nil {
		return err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_requests`).Scan(&count); err != nil {
		return err
	}
	if count >= maxTaskRequests {
		return ErrRequestLimit
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO task_requests(actor,request_id,fingerprint,task_id,created) VALUES(?,?,?,?,?)`, submission.Actor, submission.RequestID, submission.Fingerprint, id, now)
	return err
}
