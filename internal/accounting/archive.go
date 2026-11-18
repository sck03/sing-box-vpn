package accounting

import "context"

// ArchiveAccount retires the identity permanently. Existing leases can still
// settle against their original period. Revocation and publication intent are
// committed with the lifecycle change, even if a worker is interrupted.
func (s *Store) ArchiveAccount(ctx context.Context, id string, revision int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := tx.ExecContext(ctx, `UPDATE accounts SET archived=1, revision=revision+1 WHERE id=? AND revision=? AND archived=0`, id, revision)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	for _, statement := range []string{
		`UPDATE credential_publications SET revision=revision+1 WHERE runtime IN (SELECT runtime FROM credentials WHERE account=?)`,
		`INSERT INTO revoked_credentials(id) SELECT id FROM credentials WHERE account=?`,
		`DELETE FROM credentials WHERE account=?`,
		`UPDATE download_grants SET revoked=1,updated=unixepoch() WHERE account=? AND revoked=0`,
	} {
		if _, err = tx.ExecContext(ctx, statement, id); err != nil {
			return err
		}
	}
	if err = record(ctx, tx, id, "archive", "entitlement archived"); err != nil {
		return err
	}
	if err = tx.Commit(); err == nil {
		s.authorizationEpoch.Add(1)
	}
	return err
}
