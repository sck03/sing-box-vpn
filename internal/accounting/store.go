// Package accounting owns entitlements, durable proxy usage and download grants.
// Download counts are stored separately and never contribute to traffic usage.
package accounting

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sck03/sing-box-vpn/internal/database"
	"github.com/sck03/sing-box-vpn/internal/domain"
)

var ErrDenied = errors.New("account disabled, expired, exhausted, or unavailable")
var ErrConflict = errors.New("account revision conflict")

// Reservations are durable before bytes can be forwarded. A process crash may
// conservatively charge its unspent reservation, but cannot issue it twice.
const ReservationBytes int64 = 64 << 10

type Store struct {
	db                 *sql.DB
	authorizationEpoch atomic.Uint64
	reservationMu      sync.Mutex
	reservations       *reservationWriter
	closed             bool
}

func Open(path string) (*Store, error) {
	db, err := database.Open(path)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	_, err = db.Exec(schema)
	if err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	s.reservationMu.Lock()
	if s.closed {
		s.reservationMu.Unlock()
		return nil
	}
	s.closed = true
	writer := s.reservations
	if writer != nil {
		close(writer.requests)
	}
	s.reservationMu.Unlock()
	if writer != nil {
		<-writer.done
	}
	return s.db.Close()
}

const schema = `
CREATE TABLE IF NOT EXISTS accounts (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, disabled INTEGER NOT NULL,
 expires INTEGER, quota INTEGER CHECK(quota IS NULL OR quota>=0),
 revision INTEGER NOT NULL, created INTEGER NOT NULL,
 charged INTEGER NOT NULL DEFAULT 0 CHECK(charged>=0),
 upload INTEGER NOT NULL DEFAULT 0 CHECK(upload>=0),
 download INTEGER NOT NULL DEFAULT 0 CHECK(download>=0),
 subscriber TEXT NOT NULL DEFAULT '', start_mode TEXT NOT NULL DEFAULT '', starts INTEGER,
 duration INTEGER NOT NULL DEFAULT 0, period_kind TEXT NOT NULL DEFAULT '',
 period_days INTEGER NOT NULL DEFAULT 0, period_day INTEGER NOT NULL DEFAULT 0,
 timezone TEXT NOT NULL DEFAULT '', started INTEGER, period INTEGER NOT NULL DEFAULT 0,
 archived INTEGER NOT NULL DEFAULT 0 CHECK(archived IN (0,1)));
CREATE INDEX IF NOT EXISTS account_lifecycle ON accounts(archived,created,id);
CREATE INDEX IF NOT EXISTS account_subscriber ON accounts(subscriber);
CREATE TABLE IF NOT EXISTS usage_periods (
 account TEXT NOT NULL REFERENCES accounts(id), period INTEGER NOT NULL,
 charged INTEGER NOT NULL, upload INTEGER NOT NULL, download INTEGER NOT NULL,
 PRIMARY KEY(account,period));
CREATE TABLE IF NOT EXISTS subscribers (
 id TEXT PRIMARY KEY, name TEXT NOT NULL, disabled INTEGER NOT NULL DEFAULT 0,
 revision INTEGER NOT NULL DEFAULT 1);
CREATE TABLE IF NOT EXISTS download_grants (
 id TEXT PRIMARY KEY,account TEXT REFERENCES accounts(id),scope TEXT NOT NULL,
 clients TEXT NOT NULL,outputs TEXT NOT NULL,expires INTEGER,max_downloads INTEGER,hash TEXT NOT NULL UNIQUE,
 downloads INTEGER NOT NULL DEFAULT 0,revoked INTEGER NOT NULL DEFAULT 0,
 created INTEGER NOT NULL DEFAULT (unixepoch()), updated INTEGER NOT NULL DEFAULT (unixepoch()));
CREATE INDEX IF NOT EXISTS grant_retention ON download_grants(updated);
CREATE INDEX IF NOT EXISTS grant_account ON download_grants(account,revoked,scope);
CREATE TABLE IF NOT EXISTS credentials (
 id TEXT PRIMARY KEY, account TEXT NOT NULL REFERENCES accounts(id),
 runtime TEXT NOT NULL, inbound TEXT NOT NULL, protocol TEXT NOT NULL,
 generation INTEGER NOT NULL, secret BLOB NOT NULL,
 UNIQUE(account,runtime,inbound));
CREATE INDEX IF NOT EXISTS credential_runtime ON credentials(runtime,inbound);
CREATE UNIQUE INDEX IF NOT EXISTS credential_identity ON credentials(runtime,inbound,
 CASE WHEN protocol IN ('anytls','hysteria2') THEN json_extract(secret,'$.password') ELSE json_extract(secret,'$.uuid') END);
CREATE TABLE IF NOT EXISTS credential_publications (
 runtime TEXT PRIMARY KEY, revision INTEGER NOT NULL DEFAULT 0,
 published INTEGER NOT NULL DEFAULT 0, loaded INTEGER NOT NULL DEFAULT 0);
INSERT OR IGNORE INTO credential_publications(runtime) VALUES('normal'),('vpngate');
CREATE TABLE IF NOT EXISTS revoked_credentials (id TEXT PRIMARY KEY);
CREATE TABLE IF NOT EXISTS leases (
 id TEXT PRIMARY KEY, account TEXT NOT NULL REFERENCES accounts(id),
 runtime TEXT NOT NULL, reserved INTEGER NOT NULL, upload INTEGER NOT NULL DEFAULT 0,
 download INTEGER NOT NULL DEFAULT 0, closed INTEGER NOT NULL DEFAULT 0,
 period INTEGER NOT NULL DEFAULT 0);
CREATE INDEX IF NOT EXISTS lease_period ON leases(account,period,closed);
CREATE TABLE IF NOT EXISTS lease_scopes (
 id TEXT PRIMARY KEY, retired INTEGER NOT NULL DEFAULT 0, closed INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS events (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT, at INTEGER NOT NULL,
 account TEXT NOT NULL, kind TEXT NOT NULL, detail TEXT NOT NULL);
`

type Usage struct {
	domain.Account
	EffectiveExpiresAt *time.Time `json:"effective_expires_at"`
	Archived           bool       `json:"archived"`
	Charged            int64      `json:"charged_bytes"`
	Upload             int64      `json:"upload_bytes"`
	Download           int64      `json:"download_bytes"`
	StartedAt          *time.Time `json:"started_at"`
	PeriodID           int64      `json:"period_id"`
	NextReset          *time.Time `json:"next_reset"`
	State              string     `json:"state"`
	SubscriberDisabled bool       `json:"subscriber_disabled"`
}

type scanner interface{ Scan(...any) error }

func readAccount(row scanner) (u Usage, err error) {
	var expires, quota, starts, started sql.NullInt64
	var created int64
	err = row.Scan(&u.ID, &u.Name, &u.Disabled, &expires, &quota, &u.Revision, &created, &u.Charged, &u.Upload, &u.Download,
		&u.SubscriberID, &u.StartMode, &starts, &u.DurationSeconds, &u.Period.Kind, &u.Period.Days, &u.Period.Day, &u.Period.Timezone, &started, &u.PeriodID, &u.Archived)
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
	if starts.Valid {
		v := time.Unix(starts.Int64, 0).UTC()
		u.StartsAt = &v
	}
	if started.Valid {
		t := time.Unix(started.Int64, 0).UTC()
		u.StartedAt = &t
	}
	u.describe(time.Now())
	return
}

const accountColumns = `id,name,disabled,expires,quota,revision,created,charged,upload,download,subscriber,start_mode,starts,duration,period_kind,period_days,period_day,timezone,started,period,archived`

func (s *Store) Account(ctx context.Context, id string) (Usage, error) {
	return readAccount(s.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id=?`, id))
}
func timestamp(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Unix()
}

func (s *Store) SaveAccount(ctx context.Context, a domain.Account) error {
	defer s.authorizationEpoch.Add(1)
	if err := a.Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE accounts SET revision=revision WHERE id=?`, a.ID); err != nil {
		return err
	}
	if a.SubscriberID != "" {
		var exists int
		if err = tx.QueryRowContext(ctx, `SELECT 1 FROM subscribers WHERE id=?`, a.SubscriberID).Scan(&exists); err != nil {
			return errors.New("subscriber does not exist")
		}
	}
	if a.Revision == 0 {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM accounts WHERE archived=0`).Scan(&count); err != nil {
			return err
		}
		if count >= 1000 {
			return errors.New("account limit is 1000")
		}
		now := time.Now().UTC().Truncate(time.Second)
		var start *time.Time
		if a.StartMode == "scheduled" {
			start = a.StartsAt
		} else if a.StartMode != "first_use" {
			start = &now
		}
		period := int64(0)
		if start != nil {
			period = start.Unix()
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO accounts(id,name,disabled,expires,quota,revision,created,subscriber,start_mode,starts,duration,period_kind,period_days,period_day,timezone,started,period) VALUES(?,?,?,?,?,1,?,?,?,?,?,?,?,?,?,?,?)`,
			a.ID, a.Name, a.Disabled, timestamp(a.ExpiresAt), a.QuotaBytes, now.Unix(), a.SubscriberID, a.StartMode, timestamp(a.StartsAt), a.DurationSeconds, a.Period.Kind, a.Period.Days, a.Period.Day, a.Period.Timezone, timestamp(start), period)
	} else {
		old, e := readAccount(tx.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id=?`, a.ID))
		if e != nil {
			return e
		}
		// Activation and the reset anchor are immutable. New plans get new IDs;
		// editing a quota must never silently reset usage or reactivate expiry.
		if old.StartMode != a.StartMode || timestamp(old.StartsAt) != timestamp(a.StartsAt) || old.DurationSeconds != a.DurationSeconds || old.Period != a.Period {
			return errors.New("start and period policy are immutable; create a new entitlement")
		}
		var r sql.Result
		r, err = tx.ExecContext(ctx, `UPDATE accounts SET name=?,disabled=?,expires=?,quota=?,subscriber=?,revision=revision+1 WHERE id=? AND revision=? AND archived=0`, a.Name, a.Disabled, timestamp(a.ExpiresAt), a.QuotaBytes, a.SubscriberID, a.ID, a.Revision)
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
	return pruneEvents(ctx, tx)
}

func pruneEvents(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM events WHERE sequence < (SELECT COALESCE(MAX(sequence),0)-10000 FROM events)`)
	return err
}

func (s *Store) Bind(ctx context.Context, c domain.Credential) error {
	return s.BindBatch(ctx, []domain.Credential{c})
}

// BindBatch publishes every credential in one transaction. A conflict on any
// inbound leaves all accounts unchanged.
func (s *Store) BindBatch(ctx context.Context, credentials []domain.Credential) error {
	if len(credentials) == 0 {
		return nil
	}
	if len(credentials) > 10000 {
		return errors.New("credential batch limit exceeded")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE credentials SET generation=generation WHERE id=''`); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM credentials`).Scan(&count); err != nil {
		return err
	}
	if count+len(credentials) > 10000 {
		return errors.New("credential limit exceeded")
	}
	changed := map[string]bool{}
	for _, c := range credentials {
		if err = bindCredential(ctx, tx, c); err != nil {
			return err
		}
		changed[c.RuntimeID] = true
	}
	for scope := range changed {
		if err = advanceCredentials(ctx, tx, scope); err != nil {
			return err
		}
	}
	if err = pruneEvents(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func bindCredential(ctx context.Context, tx *sql.Tx, c domain.Credential) error {
	c, err := c.Normalize()
	if err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	var revoked int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM revoked_credentials WHERE id=?`, c.ID).Scan(&revoked); err != nil {
		return err
	}
	if revoked > 0 {
		return errors.New("credential ID was revoked; create a new credential ID")
	}
	var active bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM accounts WHERE id=? AND archived=0)`, c.AccountID).Scan(&active); err != nil {
		return err
	}
	if !active {
		return ErrDenied
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO credentials(id,account,runtime,inbound,protocol,generation,secret) VALUES(?,?,?,?,?,?,?)`, c.ID, c.AccountID, c.RuntimeID, c.InboundID, c.Protocol, c.Generation, data); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO events(at,account,kind,detail) VALUES(?,?,'bind',?)`, time.Now().Unix(), c.AccountID, c.RuntimeID+"/"+c.InboundID); err != nil {
		return err
	}
	return nil
}

func (s *Store) Credentials(ctx context.Context, runtime string) ([]domain.Credential, error) {
	return s.credentials(ctx, `runtime=?`, runtime)
}

func (s *Store) AccountCredentials(ctx context.Context, account, runtime string) ([]domain.Credential, error) {
	return s.credentials(ctx, `account=? AND runtime=?`, account, runtime)
}

func (s *Store) credentials(ctx context.Context, predicate string, args ...any) ([]domain.Credential, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT secret FROM credentials WHERE `+predicate+` ORDER BY id LIMIT 10001`, args...)
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
	u, err := s.authorize(ctx, credential, generation, now, false)
	return u.ID, err
}

// Reserve atomically allocates one shared pool across both runtime processes.
// Cumulative checkpoints make replay idempotent; the reservation itself is the
// recovery bound if a process dies before its next checkpoint.
func (s *Store) Reserve(ctx context.Context, id, account, runtime string, want int64, now time.Time) (int64, error) {
	u, err := s.Advance(ctx, account, now, false)
	if err != nil {
		return 0, err
	}
	return s.reservePeriod(ctx, id, account, runtime, want, now, u.PeriodID)
}

func reserveInTransaction(ctx context.Context, tx *sql.Tx, id, account, runtime string, want int64, now time.Time, periodID int64) (int64, error) {
	// Acquire the SQLite writer before reading the quota, including across processes.
	if _, err := tx.ExecContext(ctx, `UPDATE accounts SET charged=charged WHERE id=?`, account); err != nil {
		return 0, err
	}
	if err := checkLeaseScope(ctx, tx, id); err != nil {
		return 0, err
	}
	u, err := readAccount(tx.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id=?`, account))
	if err != nil {
		return 0, err
	}
	u.describe(now)
	if u.PeriodID != periodID || !permitted(u, now) {
		return 0, ErrDenied
	}
	if u.QuotaBytes != nil {
		want = min(want, *u.QuotaBytes-u.Charged)
	}
	if want <= 0 {
		return 0, ErrDenied
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO leases(id,account,runtime,reserved,period) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET reserved=reserved+excluded.reserved WHERE account=excluded.account AND runtime=excluded.runtime AND period=excluded.period AND closed=0`, id, account, runtime, want, u.PeriodID); err != nil {
		return 0, err
	}
	var closed int
	var owner, instance string
	var period int64
	if err = tx.QueryRowContext(ctx, `SELECT account,runtime,closed,period FROM leases WHERE id=?`, id).Scan(&owner, &instance, &closed, &period); err != nil {
		return 0, err
	}
	if closed != 0 || owner != account || instance != runtime || period != u.PeriodID {
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
	return want, nil
}

func (s *Store) Checkpoint(ctx context.Context, id string, upload, download int64, closeLease bool) error {
	return s.CheckpointBatch(ctx, []UsageCheckpoint{{id, upload, download, closeLease}})
}

type UsageCheckpoint struct {
	ID               string
	Upload, Download int64
	Close            bool
}

// CheckpointBatch commits one tick atomically, retaining cumulative replay
// semantics and FULL durability while avoiding a disk sync for each pool.
func (s *Store) CheckpointBatch(ctx context.Context, points []UsageCheckpoint) error {
	if len(points) == 0 {
		return nil
	}
	if len(points) > 10000 {
		return errors.New("checkpoint batch exceeds pool limit")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE leases SET closed=closed WHERE id=?`, points[0].ID); err != nil {
		return err
	}
	for _, point := range points {
		if err = checkpoint(ctx, tx, point); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func checkpoint(ctx context.Context, tx *sql.Tx, point UsageCheckpoint) error {
	id, upload, download, closeLease := point.ID, point.Upload, point.Download, point.Close
	if upload < 0 || download < 0 || upload > 1<<61 || download > 1<<61 {
		return errors.New("invalid cumulative usage")
	}
	var account string
	var reserved, oldUp, oldDown, period int64
	var closed bool
	if err := tx.QueryRowContext(ctx, `SELECT account,reserved,upload,download,closed,period FROM leases WHERE id=?`, id).Scan(&account, &reserved, &oldUp, &oldDown, &closed, &period); err != nil {
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
	if _, err := tx.ExecContext(ctx, `UPDATE accounts SET upload=upload+?,download=download+?,charged=charged-? WHERE id=? AND period=?`, upload-oldUp, download-oldDown, refund, account, period); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE usage_periods SET upload=upload+?,download=download+?,charged=charged-? WHERE account=? AND period=?`, upload-oldUp, download-oldDown, refund, account, period); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE leases SET upload=?,download=?,closed=? WHERE id=?`, upload, download, closeLease, id); err != nil {
		return err
	}
	return nil
}

func (s *Store) Rotate(ctx context.Context, id, uuid, password string, expected int64) error {
	defer s.authorizationEpoch.Add(1)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE credentials SET generation=generation WHERE id=?`, id); err != nil {
		return err
	}
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
	if c, err = c.Normalize(); err != nil {
		return err
	}
	previousUUID, previousPassword := c.UUID, c.Password
	c.Generation++
	c.UUID = uuid
	c.Password = password
	if c, err = c.Normalize(); err != nil {
		return err
	}
	if previousUUID == c.UUID && previousPassword == c.Password {
		return errors.New("rotation must change the authentication secret")
	}
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
	if err = advanceCredentials(ctx, tx, c.RuntimeID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Unbind(ctx context.Context, id string, generation int64) error {
	defer s.authorizationEpoch.Add(1)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var scope string
	err = tx.QueryRowContext(ctx, `DELETE FROM credentials WHERE id=? AND generation=? RETURNING runtime`, id, generation).Scan(&scope)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO revoked_credentials(id) VALUES(?)`, id); err != nil {
		return err
	}
	if err = advanceCredentials(ctx, tx, scope); err != nil {
		return err
	}
	return tx.Commit()
}
