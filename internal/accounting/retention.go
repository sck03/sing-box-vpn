package accounting

import (
	"context"
	"database/sql"
	"time"
)

// Invalid bearer tokens need no tombstone: deletion can never authorize a token.
// Keep up to 1,000 recently invalid grants for 30 days; accounting replay guards
// and revoked credential IDs deliberately have a separate, permanent lifetime.
func pruneGrants(ctx context.Context, tx *sql.Tx, now time.Time) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM download_grants WHERE id IN (
 SELECT id FROM (SELECT id,invalid_at,ROW_NUMBER() OVER (ORDER BY invalid_at DESC,id) AS position FROM (
 SELECT id,CASE WHEN revoked=0 AND expires<=? THEN MAX(updated,expires) ELSE updated END AS invalid_at
 FROM download_grants WHERE revoked=1 OR expires<=? OR downloads>=max_downloads))
 WHERE position>1000 OR invalid_at<?)`, now.Unix(), now.Unix(), now.Add(-30*24*time.Hour).Unix())
	return err
}

func (s *Store) PruneHistory(ctx context.Context, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = pruneGrants(ctx, tx, now); err != nil {
		return err
	}
	// An old period with an unsettled lease is still part of crash accounting.
	_, err = tx.ExecContext(ctx, `DELETE FROM usage_periods WHERE (account,period) IN (
 SELECT account,period FROM (SELECT account,period,
 ROW_NUMBER() OVER (PARTITION BY account ORDER BY period DESC) AS position FROM usage_periods)
 WHERE position>120) AND NOT EXISTS (
 SELECT 1 FROM leases WHERE leases.account=usage_periods.account AND leases.period=usage_periods.period AND closed=0)`)
	if err != nil {
		return err
	}
	return tx.Commit()
}
