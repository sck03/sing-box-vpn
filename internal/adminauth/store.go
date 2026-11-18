// Package adminauth implements persistent administrator authentication. Proxy
// subscribers and download grants intentionally do not use these sessions.
package adminauth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/sck03/sing-box-vpn/internal/database"
	"github.com/sck03/sing-box-vpn/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

var ErrUnauthorized = errors.New("invalid credentials or expired session")
var ErrLimited = errors.New("too many login attempts; retry after 15 minutes")

const MinPasswordBytes = 8
const MaxPasswordBytes = 72

type Store struct{ db *sql.DB }
type Admin struct {
	ID       string `json:"id"`
	Role     string `json:"role"`
	Disabled bool   `json:"disabled"`
	MFA      bool   `json:"mfa"`
	Revision int64  `json:"revision"`
}
type Session struct {
	Admin
	Token   string    `json:"-"`
	CSRF    string    `json:"csrf"`
	Expires time.Time `json:"expires"`
}

func Open(path string) (*Store, error) {
	db, err := database.Open(path)
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(`
CREATE TABLE IF NOT EXISTS administrators(id TEXT PRIMARY KEY,password BLOB NOT NULL,role TEXT NOT NULL,disabled INTEGER NOT NULL DEFAULT 0,revision INTEGER NOT NULL DEFAULT 1,totp TEXT NOT NULL DEFAULT '',pending_totp TEXT NOT NULL DEFAULT '',pending_at INTEGER NOT NULL DEFAULT 0,step INTEGER NOT NULL DEFAULT -1);
CREATE TABLE IF NOT EXISTS admin_sessions(hash TEXT PRIMARY KEY,admin TEXT NOT NULL REFERENCES administrators(id),csrf TEXT NOT NULL,expires INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS session_admin ON admin_sessions(admin);
CREATE TABLE IF NOT EXISTS login_guard(key TEXT PRIMARY KEY,window INTEGER NOT NULL,attempts INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS audit(sequence INTEGER PRIMARY KEY AUTOINCREMENT,at INTEGER NOT NULL,actor TEXT NOT NULL,action TEXT NOT NULL,target TEXT NOT NULL,result TEXT NOT NULL);
`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}
func (s *Store) Close() error { return s.db.Close() }
func Secret() string          { var b [32]byte; _, _ = rand.Read(b[:]); return hex.EncodeToString(b[:]) }
func hash(v string) string    { b := sha256.Sum256([]byte(v)); return hex.EncodeToString(b[:]) }
func passwordHash(password string) ([]byte, error) {
	if len(password) < MinPasswordBytes || len(password) > MaxPasswordBytes {
		return nil, errors.New("password must contain 8–72 bytes")
	}
	return bcrypt.GenerateFromPassword([]byte(password), 12)
}

// PrepareAdministrator validates and hashes before initialization touches files.
// The returned function installs the first administrator in the caller's transaction.
func PrepareAdministrator(id, password string) (func(context.Context, *sql.Tx) error, error) {
	if !domain.ResourceID.MatchString(id) {
		return nil, errors.New("invalid administrator ID")
	}
	p, err := passwordHash(password)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, tx *sql.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM administrators`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return errors.New("administrator already initialized")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO administrators(id,password,role) VALUES(?,?,'admin')`, id, p); err != nil {
			return err
		}
		return audit(ctx, tx, "local-cli", "administrator-initialize", id, "success")
	}, nil
}

// Bootstrap/reset is available only through the local recovery CLI. Reset is
// explicit and revokes every prior session and authenticator for this admin.
func (s *Store) Bootstrap(ctx context.Context, id, password string, reset bool) error {
	if !domain.ResourceID.MatchString(id) {
		return errors.New("invalid administrator ID")
	}
	p, err := passwordHash(password)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE administrators SET revision=revision WHERE id=?`, id); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM administrators`).Scan(&count); err != nil {
		return err
	}
	if count > 0 && !reset {
		return errors.New("administrator already initialized; use explicit reset")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO administrators(id,password,role) VALUES(?,?,'admin') ON CONFLICT(id) DO UPDATE SET password=excluded.password,role='admin',disabled=0,totp='',pending_totp='',step=-1,revision=revision+1`, id, p)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM admin_sessions WHERE admin=?`, id); err != nil {
		return err
	}
	if err = audit(ctx, tx, "local-cli", "administrator-reset", id, "success"); err != nil {
		return err
	}
	return tx.Commit()
}

func audit(ctx context.Context, tx *sql.Tx, actor, action, target, result string) error {
	if len(target) > 128 {
		target = target[:128]
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit(at,actor,action,target,result) VALUES(?,?,?,?,?)`, time.Now().Unix(), actor, action, target, result); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM audit WHERE sequence<(SELECT COALESCE(MAX(sequence),0)-10000 FROM audit)`)
	return err
}
func (s *Store) Audit(ctx context.Context, actor, action, target, result string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = audit(ctx, tx, actor, action, target, result); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) guard(ctx context.Context, id, peer string, now int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM login_guard WHERE window<?`, now-900); err != nil {
		return err
	}
	for _, g := range []struct {
		key   string
		limit int
	}{{"global", 500}, {"peer:" + hash(peer), 40}, {"user:" + hash(id), 8}} {
		if _, err = tx.ExecContext(ctx, `INSERT INTO login_guard(key,window,attempts) VALUES(?,?,1) ON CONFLICT(key) DO UPDATE SET attempts=attempts+1`, g.key, now); err != nil {
			return err
		}
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT attempts FROM login_guard WHERE key=?`, g.key).Scan(&count); err != nil {
			return err
		}
		if count > g.limit {
			if err = tx.Commit(); err != nil {
				return err
			}
			return ErrLimited
		}
	}
	return tx.Commit()
}

// A precomputed cost-12 hash keeps unknown-user comparisons equally expensive
// without doing password hashing when an unrelated manager command starts.
var dummyHash = []byte("$2a$12$R9h/cIPz0gi.URNNX3kh2OPST9/PgBkqquzi.Ss7KIUgO2t0jWMUW")

func (s *Store) Login(ctx context.Context, id, password, code, peer string) (Session, error) {
	now := time.Now()
	if len(id) > 64 || len(password) > MaxPasswordBytes || len(code) > 6 {
		return Session{}, ErrUnauthorized
	}
	if err := s.guard(ctx, id, peer, now.Unix()); err != nil {
		return Session{}, err
	}
	var a Admin
	var p []byte
	var secret string
	err := s.db.QueryRowContext(ctx, `SELECT id,password,role,disabled,revision,totp FROM administrators WHERE id=?`, id).Scan(&a.ID, &p, &a.Role, &a.Disabled, &a.Revision, &secret)
	if errors.Is(err, sql.ErrNoRows) {
		p = dummyHash
	} else if err != nil {
		return Session{}, err
	}
	valid := bcrypt.CompareHashAndPassword(p, []byte(password)) == nil && a.ID != "" && !a.Disabled
	step := int64(-1)
	if secret != "" {
		step = verifyTOTP(secret, code, now)
		valid = valid && step >= 0
	}
	if !valid {
		_ = s.Audit(ctx, "anonymous", "login", "", "denied")
		return Session{}, ErrUnauthorized
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback()
	// Password/role changes and TOTP replay are checked again under the writer.
	r, err := tx.ExecContext(ctx, `UPDATE administrators SET step=? WHERE id=? AND revision=? AND disabled=0 AND (totp='' OR step<?)`, step, id, a.Revision, step)
	if err != nil {
		return Session{}, err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return Session{}, err
	}
	if n != 1 {
		return Session{}, ErrUnauthorized
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM admin_sessions WHERE expires<=? OR admin=?`, now.Unix(), id); err != nil {
		return Session{}, err
	}
	result := Session{Admin: a, Token: Secret(), Expires: now.Add(8 * time.Hour)}
	result.CSRF = SessionCSRF(result.Token)
	result.MFA = secret != ""
	if _, err = tx.ExecContext(ctx, `INSERT INTO admin_sessions(hash,admin,csrf,expires) VALUES(?,?,?,?)`, hash(result.Token), id, hash(result.CSRF), result.Expires.Unix()); err != nil {
		return Session{}, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM login_guard WHERE key=?`, "user:"+hash(id)); err != nil {
		return Session{}, err
	}
	if err = audit(ctx, tx, id, "login", "", "success"); err != nil {
		return Session{}, err
	}
	return result, tx.Commit()
}

func SessionCSRF(token string) string { return hash("csrf:" + token) }

func (s *Store) Authenticate(ctx context.Context, token, csrf string, write bool) (Session, error) {
	if len(token) != 64 {
		return Session{}, ErrUnauthorized
	}
	var v Session
	var expected string
	var expires int64
	err := s.db.QueryRowContext(ctx, `SELECT a.id,a.role,a.disabled,a.revision,a.totp!='',s.csrf,s.expires FROM admin_sessions s JOIN administrators a ON a.id=s.admin WHERE s.hash=? AND s.expires>? AND a.disabled=0`, hash(token), time.Now().Unix()).Scan(&v.ID, &v.Role, &v.Disabled, &v.Revision, &v.MFA, &expected, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrUnauthorized
	}
	if err != nil {
		return v, err
	}
	if write && subtle.ConstantTimeCompare([]byte(hash(csrf)), []byte(expected)) != 1 {
		return v, ErrUnauthorized
	}
	v.Expires = time.Unix(expires, 0).UTC()
	return v, nil
}
func (s *Store) Logout(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE hash=?`, hash(token))
	return err
}
func (s *Store) Revoke(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE admin=?`, id)
	return err
}

func (s *Store) List(ctx context.Context) ([]Admin, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,role,disabled,revision,totp!='' FROM administrators ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	v := []Admin{}
	for rows.Next() {
		var a Admin
		if err = rows.Scan(&a.ID, &a.Role, &a.Disabled, &a.Revision, &a.MFA); err != nil {
			return nil, err
		}
		v = append(v, a)
	}
	return v, rows.Err()
}

func (s *Store) Save(ctx context.Context, actor string, a Admin, password string) error {
	if !domain.ResourceID.MatchString(a.ID) || !slices.Contains([]string{"admin", "operator", "viewer"}, a.Role) {
		return errors.New("invalid administrator")
	}
	var p []byte
	var err error
	if password != "" || a.Revision == 0 {
		p, err = passwordHash(password)
		if err != nil {
			return err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE administrators SET revision=revision WHERE id=?`, a.ID); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM administrators WHERE role='admin' AND disabled=0 AND id!=?`, a.ID).Scan(&count); err != nil {
		return err
	}
	if count == 0 && (a.Role != "admin" || a.Disabled) {
		return errors.New("cannot disable or demote the last administrator")
	}
	if a.Revision == 0 {
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM administrators`).Scan(&count); err != nil {
			return err
		}
		if count >= 32 {
			return errors.New("administrator limit is 32")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO administrators(id,password,role,disabled) VALUES(?,?,?,?)`, a.ID, p, a.Role, a.Disabled)
	} else {
		var r sql.Result
		r, err = tx.ExecContext(ctx, `UPDATE administrators SET role=?,disabled=?,password=COALESCE(?,password),revision=revision+1 WHERE id=? AND revision=?`, a.Role, a.Disabled, p, a.ID, a.Revision)
		if err == nil {
			n, e := r.RowsAffected()
			if e != nil {
				return e
			}
			if n != 1 {
				return errors.New("administrator revision conflict")
			}
		}
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM admin_sessions WHERE admin=?`, a.ID); err != nil {
		return err
	}
	if err = audit(ctx, tx, actor, "administrator-save", a.ID, "success"); err != nil {
		return err
	}
	return tx.Commit()
}

func totp(secret string, step int64) string {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		return ""
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(counter[:])
	data := mac.Sum(nil)
	offset := data[len(data)-1] & 15
	n := binary.BigEndian.Uint32(data[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", n%1000000)
}
func verifyTOTP(secret, code string, now time.Time) int64 {
	if len(code) != 6 {
		return -1
	}
	for _, offset := range []int64{0, -1, 1} {
		step := now.Unix()/30 + offset
		if subtle.ConstantTimeCompare([]byte(totp(secret, step)), []byte(code)) == 1 {
			return step
		}
	}
	return -1
}
func (s *Store) Enroll(ctx context.Context, id, password string) (string, error) {
	if err := s.guard(ctx, "mfa:"+id, "mfa:"+id, time.Now().Unix()); err != nil {
		return "", err
	}
	var p []byte
	if err := s.db.QueryRowContext(ctx, `SELECT password FROM administrators WHERE id=? AND disabled=0`, id).Scan(&p); err != nil {
		return "", ErrUnauthorized
	}
	if bcrypt.CompareHashAndPassword(p, []byte(password)) != nil {
		return "", ErrUnauthorized
	}
	var b [20]byte
	rand.Read(b[:])
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:])
	_, err := s.db.ExecContext(ctx, `UPDATE administrators SET pending_totp=?,pending_at=? WHERE id=?`, secret, time.Now().Unix(), id)
	return secret, err
}
func (s *Store) ConfirmMFA(ctx context.Context, id, code string) error {
	if err := s.guard(ctx, "mfa:"+id, "mfa:"+id, time.Now().Unix()); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE administrators SET step=step WHERE id=?`, id); err != nil {
		return err
	}
	var secret string
	var at int64
	if err = tx.QueryRowContext(ctx, `SELECT pending_totp,pending_at FROM administrators WHERE id=?`, id).Scan(&secret, &at); err != nil {
		return err
	}
	step := verifyTOTP(secret, strings.TrimSpace(code), time.Now())
	if secret == "" || time.Now().Unix()-at > 600 || step < 0 {
		return ErrUnauthorized
	}
	if _, err = tx.ExecContext(ctx, `UPDATE administrators SET totp=?,pending_totp='',step=?,revision=revision+1 WHERE id=?`, secret, step, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM admin_sessions WHERE admin=?`, id); err != nil {
		return err
	}
	if err = audit(ctx, tx, id, "mfa-enabled", id, "success"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) AuditLog(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT at,actor,action,target,result FROM audit ORDER BY sequence DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var at int64
		var actor, action, target, result string
		if err = rows.Scan(&at, &actor, &action, &target, &result); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"at": time.Unix(at, 0).UTC(), "actor": actor, "action": action, "target": target, "result": result})
	}
	return out, rows.Err()
}
