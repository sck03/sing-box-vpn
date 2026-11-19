package controlstore

import (
	"context"
	"time"
)

// Policy bounds terminal metadata and detailed results. Pending work is never
// evicted; maintenance failures must not change the worker completion result.
func (s *Store) PruneHistory(ctx context.Context, now time.Time) error {
	p, err := s.HistoryPolicy(ctx)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `DELETE FROM control_tasks WHERE id IN (
 SELECT id FROM (SELECT id,updated,ROW_NUMBER() OVER (ORDER BY id DESC) AS position
 FROM control_tasks WHERE state IN ('succeeded','failed','cancelled'))
 WHERE position>? OR updated<?)`, p.Count, now.Add(-time.Duration(p.Days)*24*time.Hour).Unix())
	if err != nil {
		return err
	}
	// Reserve space for the configured compact markers and count UTF-8 bytes,
	// not SQLite's character length. Keep the outcome after detail eviction.
	_, err = tx.ExecContext(ctx, `UPDATE control_tasks SET result=json_object(
	 'retained',json('false'),'reason','result retention budget','outcome',
	 outcome) WHERE id IN (
	 SELECT id FROM (SELECT id,SUM(length(CAST(result AS BLOB))) OVER (ORDER BY id DESC) AS bytes
	 FROM control_tasks WHERE state IN ('succeeded','failed','cancelled')) WHERE bytes>?)`, p.ResultBytes-p.Count*128)
	if err != nil {
		return err
	}
	return tx.Commit()
}
