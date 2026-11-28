package accounting

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
)

// A compact high-water mark rejects replay after closed lease rows are removed.
func checkLeaseScope(ctx context.Context, tx *sql.Tx, id string) error {
	scope, number, scoped := strings.Cut(id, "/")
	if !scoped {
		return ErrDenied
	}
	decoded, err := hex.DecodeString(scope)
	sequence, e := strconv.ParseInt(number, 10, 64)
	if err != nil || e != nil || len(decoded) != 16 || sequence < 1 || strconv.FormatInt(sequence, 10) != number {
		return ErrDenied
	}
	var retired int64
	var closed bool
	err = tx.QueryRowContext(ctx, `SELECT retired,closed FROM lease_scopes WHERE id=?`, scope).Scan(&retired, &closed)
	if errors.Is(err, sql.ErrNoRows) {
		// The reservation transaction already holds the writer lock. A scope is
		// created once; normal refills need only the replay/closure read.
		_, err = tx.ExecContext(ctx, `INSERT INTO lease_scopes(id) VALUES(?)`, scope)
		return err
	}
	if err != nil {
		return err
	}
	if closed || sequence <= retired {
		return ErrDenied
	}
	return nil
}

func (s *Store) compactScope(ctx context.Context, scope string, retired uint64, closed bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO lease_scopes(id,retired,closed) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET retired=MAX(retired,excluded.retired),closed=MAX(closed,excluded.closed)`, scope, retired, closed); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM leases WHERE id LIKE ? AND CAST(substr(id,34) AS INTEGER)<=? AND closed=1`, scope+"/%", retired); err != nil {
		return err
	}
	return tx.Commit()
}
