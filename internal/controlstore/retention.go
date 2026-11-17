package controlstore

import (
	"context"
	"time"
)

// Completed task metadata is bounded by age/count, and detailed results by a
// separate 8 MiB budget. Pending task inputs are never evicted by retention.
func (s *Store) PruneHistory(ctx context.Context, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `DELETE FROM control_tasks WHERE id IN (
 SELECT id FROM (SELECT id,updated,ROW_NUMBER() OVER (ORDER BY id DESC) AS position
 FROM control_tasks WHERE state IN ('succeeded','failed','cancelled'))
 WHERE position>200 OR updated<?)`, now.Add(-30*24*time.Hour).Unix())
	if err != nil {
		return err
	}
	// Reserve space for at most 200 compact markers and count UTF-8 bytes,
	// not SQLite's character length. Keep the outcome after detail eviction.
	_, err = tx.ExecContext(ctx, `UPDATE control_tasks SET result=json_object(
	 'retained',json('false'),'reason','result retention budget','outcome',
	 CASE WHEN json_extract(result,'$.outcome') IN ('passed','partial','failed') THEN json_extract(result,'$.outcome') END) WHERE id IN (
	 SELECT id FROM (SELECT id,SUM(length(CAST(result AS BLOB))) OVER (ORDER BY id DESC) AS bytes
	 FROM control_tasks WHERE state IN ('succeeded','failed','cancelled')) WHERE bytes>?)`, (8<<20)-200*128)
	if err != nil {
		return err
	}
	return tx.Commit()
}
