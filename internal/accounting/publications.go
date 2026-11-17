package accounting

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

// CredentialPublication separates durable mutations, generated downloads and
// credentials actually loaded by a running core. Mutations advance revision in
// the same transaction; a crash cannot lose the need to reconcile.
type CredentialPublication struct {
	Runtime   string `json:"runtime"`
	Revision  int64  `json:"revision"`
	Published int64  `json:"published"`
	Loaded    int64  `json:"loaded"`
}

func advanceCredentials(ctx context.Context, tx *sql.Tx, scope string) error {
	_, err := tx.ExecContext(ctx, `UPDATE credential_publications SET revision=revision+1 WHERE runtime=?`, scope)
	return err
}

func (s *Store) CredentialPublications(ctx context.Context) ([]CredentialPublication, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT runtime,revision,published,loaded FROM credential_publications ORDER BY runtime`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []CredentialPublication{}
	for rows.Next() {
		var v CredentialPublication
		if err := rows.Scan(&v.Runtime, &v.Revision, &v.Published, &v.Loaded); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

// CredentialSnapshot reads the version and users from the same SQLite snapshot.
func (s *Store) CredentialSnapshot(ctx context.Context, scope string) ([]domain.Credential, int64, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	var revision int64
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM credential_publications WHERE runtime=?`, scope).Scan(&revision); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT secret FROM credentials WHERE runtime=? ORDER BY id LIMIT 10001`, scope)
	if err != nil {
		return nil, 0, err
	}
	result := []domain.Credential{}
	for rows.Next() {
		var raw []byte
		var c domain.Credential
		if err = rows.Scan(&raw); err == nil {
			err = json.Unmarshal(raw, &c)
		}
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		result = append(result, c)
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return nil, 0, err
	}
	if len(result) > 10000 {
		return nil, 0, errors.New("credential limit exceeded")
	}
	return result, revision, tx.Commit()
}

func (s *Store) CredentialsPublished(ctx context.Context, scope string, revision int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE credential_publications SET published=max(published,?) WHERE runtime=? AND revision>=?`, revision, scope, revision)
	return err
}

// Called only after listeners start successfully, never by configuration checks.
func (s *Store) CredentialsLoaded(ctx context.Context, scope string, revision int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE credential_publications SET loaded=? WHERE runtime=? AND revision>=?`, revision, scope, revision)
	return err
}
