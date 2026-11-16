// Package accounting owns durable, shared proxy usage. Download counters live
// in the subscription service and never contribute to this ledger.
package accounting

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sck03/sing-box-vpn/internal/domain"
	_ "modernc.org/sqlite"
)

var ErrDenied = errors.New("account disabled, expired, exhausted, or unavailable")
var ErrConflict = errors.New("account revision conflict")

// Reservations are durable before bytes can be forwarded. A process crash may
// conservatively charge its unspent reservation, but cannot issue it twice.
const ReservationBytes int64 = 64 << 10

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	if err = os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if _, err = db.Exec(`PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA foreign_keys=ON;`); err == nil {
		var version int
		err = db.QueryRow(`PRAGMA user_version`).Scan(&version)
		if err == nil && version != 0 && version != 1 {
			err = errors.New("unsupported account database schema")
		}
		if err == nil {
			_, err = db.Exec(schema)
		}
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS accounts (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, disabled INTEGER NOT NULL,
 expires INTEGER, quota INTEGER CHECK(quota IS NULL OR quota>=0),
 revision INTEGER NOT NULL, created INTEGER NOT NULL,
 charged INTEGER NOT NULL DEFAULT 0 CHECK(charged>=0),
 upload INTEGER NOT NULL DEFAULT 0 CHECK(upload>=0),
 download INTEGER NOT NULL DEFAULT 0 CHECK(download>=0));
CREATE TABLE IF NOT EXISTS credentials (
 id TEXT PRIMARY KEY, account TEXT NOT NULL REFERENCES accounts(id),
 runtime TEXT NOT NULL, inbound TEXT NOT NULL, protocol TEXT NOT NULL,
 generation INTEGER NOT NULL, secret BLOB NOT NULL,
 UNIQUE(account,runtime,inbound));
CREATE TABLE IF NOT EXISTS leases (
 id TEXT PRIMARY KEY, account TEXT NOT NULL REFERENCES accounts(id),
 runtime TEXT NOT NULL, reserved INTEGER NOT NULL, upload INTEGER NOT NULL DEFAULT 0,
 download INTEGER NOT NULL DEFAULT 0, closed INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS events (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT, at INTEGER NOT NULL,
 account TEXT NOT NULL, kind TEXT NOT NULL, detail TEXT NOT NULL);
PRAGMA user_version=1;`

type Usage struct {
	domain.Account
	Charged  int64 `json:"charged_bytes"`
	Upload   int64 `json:"upload_bytes"`
	Download int64 `json:"download_bytes"`
}

type scanner interface{ Scan(...any) error }

func readAccount(row scanner) (u Usage, err error) {
	var expires, quota sql.NullInt64
	var created int64
	err = row.Scan(&u.ID, &u.Name, &u.Disabled, &expires, &quota, &u.Revision, &created, &u.Charged, &u.Upload, &u.Download)
	if err != nil {
		return
	}
	u.CreatedAt = time.Unix(created, 0).UTC()
	if expires.Valid {
		v := time.Unix(expires.Int64, 0).UTC()
		u.ExpiresAt = &v
	}
	if quota.Valid {
		u.QuotaBytes = &quota.Int64
	}
	return
}

const accountColumns = `id,name,disabled,expires,quota,revision,created,charged,upload,download`

func (s *Store) Account(ctx context.Context, id string) (Usage, error) {
	return readAccount(s.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id=?`, id))
}
func (s *Store) Accounts(ctx context.Context) ([]Usage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+accountColumns+` FROM accounts ORDER BY created,id LIMIT 1001`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Usage{}
	for rows.Next() {
		u, err := readAccount(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, u)
	}
	return result, rows.Err()
}
func timestamp(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Unix()
}

func (s *Store) SaveAccount(ctx context.Context, a domain.Account) error {
	if err := a.Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if a.Revision == 0 {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM accounts`).Scan(&count); err != nil {
			return err
		}
		if count >= 1000 {
			return errors.New("account limit is 1000")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO accounts(id,name,disabled,expires,quota,revision,created) VALUES(?,?,?,?,?,1,?)`, a.ID, a.Name, a.Disabled, timestamp(a.ExpiresAt), a.QuotaBytes, time.Now().Unix())
	} else {
		var r sql.Result
		r, err = tx.ExecContext(ctx, `UPDATE accounts SET name=?,disabled=?,expires=?,quota=?,revision=revision+1 WHERE id=? AND revision=?`, a.Name, a.Disabled, timestamp(a.ExpiresAt), a.QuotaBytes, a.ID, a.Revision)
		if err == nil {
			n, e := r.RowsAffected()
			if e != nil {
				return e
			}
			if n != 1 {
				return ErrConflict
			}
		}
	}
	if err != nil {
		return err
	}
	if err = record(ctx, tx, a.ID, "account", "account configuration saved"); err != nil {
		return err
	}
	return tx.Commit()
}

func record(ctx context.Context, tx *sql.Tx, id, kind, detail string) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO events(at,account,kind,detail) VALUES(?,?,?,?)`, time.Now().Unix(), id, kind, detail); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM events WHERE sequence < (SELECT COALESCE(MAX(sequence),0)-10000 FROM events)`)
	return err
}

func (s *Store) Bind(ctx context.Context, c domain.Credential) error {
	if err := c.Validate(); err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO credentials(id,account,runtime,inbound,protocol,generation,secret) VALUES(?,?,?,?,?,?,?)`, c.ID, c.AccountID, c.RuntimeID, c.InboundID, c.Protocol, c.Generation, data); err != nil {
		return err
	}
	if err = record(ctx, tx, c.AccountID, "bind", c.RuntimeID+"/"+c.InboundID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Credentials(ctx context.Context, runtime string) ([]domain.Credential, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT secret FROM credentials WHERE runtime=? ORDER BY id LIMIT 10001`, runtime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.Credential{}
	for rows.Next() {
		var data []byte
		var c domain.Credential
		if err = rows.Scan(&data); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(data, &c); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	if len(result) > 10000 {
		return nil, errors.New("credential limit exceeded")
	}
	return result, rows.Err()
}

func (s *Store) Authorized(ctx context.Context, credential string, generation int64, now time.Time) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT a.id FROM accounts a JOIN credentials c ON c.account=a.id WHERE c.id=? AND c.generation=? AND a.disabled=0 AND (a.expires IS NULL OR a.expires>?) AND (a.quota IS NULL OR a.charged<=a.quota)`, credential, generation, now.Unix()).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrDenied
	}
	return id, err
}

// Reserve atomically allocates one shared pool across both runtime processes.
// Cumulative checkpoints make replay idempotent; the reservation itself is the
// recovery bound if a process dies before its next checkpoint.
func (s *Store) Reserve(ctx context.Context, id, account, runtime string, want int64, now time.Time) (int64, error) {
	if want < 1 || want > ReservationBytes {
		return 0, errors.New("invalid reservation size")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// Acquire the SQLite writer before reading the quota, including across processes.
	if _, err = tx.ExecContext(ctx, `UPDATE accounts SET charged=charged WHERE id=?`, account); err != nil {
		return 0, err
	}
	u, err := readAccount(tx.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id=?`, account))
	if err != nil {
		return 0, err
	}
	if u.Disabled || (u.ExpiresAt != nil && !now.Before(*u.ExpiresAt)) {
		return 0, ErrDenied
	}
	if u.QuotaBytes != nil {
		want = min(want, *u.QuotaBytes-u.Charged)
	}
	if want <= 0 {
		return 0, ErrDenied
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO leases(id,account,runtime,reserved) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET reserved=reserved+excluded.reserved WHERE account=excluded.account AND runtime=excluded.runtime AND closed=0`, id, account, runtime, want); err != nil {
		return 0, err
	}
	var closed int
	var owner, instance string
	if err = tx.QueryRowContext(ctx, `SELECT account,runtime,closed FROM leases WHERE id=?`, id).Scan(&owner, &instance, &closed); err != nil {
		return 0, err
	}
	if closed != 0 || owner != account || instance != runtime {
		return 0, errors.New("reservation owner mismatch")
	}
	charged, err := tx.ExecContext(ctx, `UPDATE accounts SET charged=charged+? WHERE id=? AND charged<=?`, want, account, int64(1<<62)-want)
	if err != nil {
		return 0, err
	}
	if n, err := charged.RowsAffected(); err != nil {
		return 0, err
	} else if n != 1 {
		return 0, errors.New("account counter capacity exceeded")
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return want, nil
}

func (s *Store) Checkpoint(ctx context.Context, id string, upload, download int64, closeLease bool) error {
	if upload < 0 || download < 0 || upload > 1<<61 || download > 1<<61 {
		return errors.New("invalid cumulative usage")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE leases SET closed=closed WHERE id=?`, id); err != nil {
		return err
	}
	var account string
	var reserved, oldUp, oldDown int64
	var closed bool
	if err = tx.QueryRowContext(ctx, `SELECT account,reserved,upload,download,closed FROM leases WHERE id=?`, id).Scan(&account, &reserved, &oldUp, &oldDown, &closed); err != nil {
		return err
	}
	if upload < oldUp || download < oldDown || upload+download > reserved {
		return errors.New("invalid or reordered usage checkpoint")
	}
	if closed {
		if upload == oldUp && download == oldDown {
			return nil
		}
		return errors.New("closed lease changed")
	}
	refund := int64(0)
	if closeLease {
		refund = reserved - upload - download
	}
	if _, err = tx.ExecContext(ctx, `UPDATE accounts SET upload=upload+?,download=download+?,charged=charged-? WHERE id=?`, upload-oldUp, download-oldDown, refund, account); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE leases SET upload=?,download=?,closed=? WHERE id=?`, upload, download, closeLease, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Rotate(ctx context.Context, id, uuid, password string, expected int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var data []byte
	if err = tx.QueryRowContext(ctx, `SELECT secret FROM credentials WHERE id=? AND generation=?`, id, expected).Scan(&data); errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	} else if err != nil {
		return err
	}
	var c domain.Credential
	if err = json.Unmarshal(data, &c); err != nil {
		return err
	}
	c.Generation++
	c.UUID = uuid
	c.Password = password
	data, err = json.Marshal(c)
	if err != nil {
		return err
	}
	r, err := tx.ExecContext(ctx, `UPDATE credentials SET generation=?,secret=? WHERE id=? AND generation=?`, c.Generation, data, id, expected)
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
	if err = record(ctx, tx, c.AccountID, "rotate", fmt.Sprintf("credential %s generation %d", id, c.Generation)); err != nil {
		return err
	}
	return tx.Commit()
}
