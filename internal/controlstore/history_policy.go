package controlstore

import (
	"context"
	"encoding/json"
	"errors"
)

func (s *Store) PendingKind(ctx context.Context, kind string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM control_tasks WHERE kind=? AND state IN ('queued','running')`, kind).Scan(&n)
	return n > 0, err
}

type HistoryPolicy struct {
	Count       int `json:"count"`
	Days        int `json:"days"`
	ResultBytes int `json:"result_bytes"`
}

var DefaultHistoryPolicy = HistoryPolicy{Count: 200, Days: 30, ResultBytes: 8 << 20}

func (p HistoryPolicy) Validate() error {
	if p.Count < 20 || p.Count > 2000 || p.Days < 1 || p.Days > 365 || p.ResultBytes < 256<<10 || p.ResultBytes > 64<<20 {
		return errors.New("保留数量应为 20–2000，天数 1–365，详情预算 256 KiB–64 MiB")
	}
	return nil
}
func (s *Store) HistoryPolicy(ctx context.Context) (HistoryPolicy, error) {
	p := DefaultHistoryPolicy
	raw, err := s.Observation(ctx, "task-history-policy")
	if err == nil && raw != "" {
		err = json.Unmarshal([]byte(raw), &p)
	}
	if err == nil {
		err = p.Validate()
	}
	return p, err
}
func (s *Store) SetHistoryPolicy(ctx context.Context, p HistoryPolicy) error {
	if err := p.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return s.Observe(ctx, "task-history-policy", string(raw))
}
func (s *Store) Acknowledge(ctx context.Context, id int64) error {
	r, err := s.db.ExecContext(ctx, `UPDATE control_tasks SET acknowledged=1 WHERE id=? AND state IN ('succeeded','failed','cancelled')`, id)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n != 1 {
		return errors.New("只能标记已结束的任务")
	}
	return err
}
func (s *Store) DeleteTask(ctx context.Context, id int64) error {
	r, err := s.db.ExecContext(ctx, `DELETE FROM control_tasks WHERE id=? AND state IN ('succeeded','failed','cancelled')`, id)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n != 1 {
		return errors.New("只能删除已结束的任务")
	}
	return err
}
func (s *Store) ClearTasks(ctx context.Context, acknowledgedOnly bool) (int64, error) {
	r, err := s.db.ExecContext(ctx, `DELETE FROM control_tasks WHERE state IN ('succeeded','failed','cancelled') AND (?=0 OR acknowledged=1)`, acknowledgedOnly)
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}

type HistoryUsage struct {
	Completed   int64 `json:"completed"`
	Pending     int64 `json:"pending"`
	ResultBytes int64 `json:"result_bytes"`
}

func (s *Store) HistoryUsage(ctx context.Context) (u HistoryUsage, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(state IN ('succeeded','failed','cancelled')),0),COALESCE(SUM(state IN ('queued','running')),0),COALESCE(SUM(length(CAST(result AS BLOB))),0) FROM control_tasks`).Scan(&u.Completed, &u.Pending, &u.ResultBytes)
	return
}
