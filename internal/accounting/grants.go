package accounting

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/sck03/sing-box-vpn/internal/clientprofile"
	"github.com/sck03/sing-box-vpn/internal/domain"
	"slices"
	"time"
)

type Grant struct {
	ID           string     `json:"id"`
	AccountID    string     `json:"account_id"`
	Outputs      []string   `json:"outputs,omitempty"`
	Scope        string     `json:"scope"`
	Clients      []string   `json:"clients"`
	ExpiresAt    *time.Time `json:"expires_at"`
	MaxDownloads *int64     `json:"max_downloads"`
	Downloads    int64      `json:"downloads"`
	Revoked      bool       `json:"revoked"`
	State        string     `json:"state"`
}

func (g Grant) Status(now time.Time) string {
	if g.Revoked {
		return "revoked"
	}
	if g.ExpiresAt != nil && !now.Before(*g.ExpiresAt) {
		return "expired"
	}
	if g.MaxDownloads != nil && g.Downloads >= *g.MaxDownloads {
		return "exhausted"
	}
	return "active"
}

func grantHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func (s *Store) CreateGrant(ctx context.Context, g Grant) (string, error) {
	if !domain.ResourceID.MatchString(g.ID) || !slices.Contains([]string{"", "normal", "vpngate"}, g.Scope) || len(g.Clients) > len(clientprofile.All()) {
		return "", errors.New("invalid download grant")
	}
	if g.AccountID != "" {
		if !domain.ResourceID.MatchString(g.AccountID) || len(g.Clients) == 0 || len(g.Outputs) != 0 {
			return "", errors.New("account grants require client formats and cannot select outputs")
		}
	} else {
		if len(g.Outputs) == 0 || len(g.Outputs) > 128 || len(g.Clients) != 0 || g.Scope != "" {
			return "", errors.New("output grants require 1–128 outputs")
		}
		seen := map[string]bool{}
		for _, id := range g.Outputs {
			if !domain.ResourceID.MatchString(id) || seen[id] {
				return "", errors.New("invalid or duplicate output")
			}
			seen[id] = true
		}
	}
	seenClients := make(map[string]bool, len(g.Clients))
	for _, client := range g.Clients {
		if !clientprofile.SupportsAccount(client) || seenClients[client] {
			return "", errors.New("unsupported or duplicate client")
		}
		seenClients[client] = true
	}
	if g.MaxDownloads != nil && (*g.MaxDownloads < 0 || *g.MaxDownloads > 1<<40) {
		return "", errors.New("invalid download limit")
	}
	clients, err := json.Marshal(g.Clients)
	if err != nil {
		return "", err
	}
	var b [32]byte
	rand.Read(b[:])
	token := hex.EncodeToString(b[:])
	outputs, err := json.Marshal(g.Outputs)
	if err != nil {
		return "", err
	}
	var account any
	if g.AccountID != "" {
		account = g.AccountID
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err = pruneGrants(ctx, tx, time.Now()); err != nil {
		return "", err
	}
	r, err := tx.ExecContext(ctx, `INSERT INTO download_grants(id,account,scope,clients,outputs,expires,max_downloads,hash) SELECT ?,?,?,?,?,?,?,? WHERE (SELECT COUNT(*) FROM download_grants WHERE revoked=0 AND (expires IS NULL OR expires>?) AND (max_downloads IS NULL OR downloads<max_downloads))<10000`, g.ID, account, g.Scope, string(clients), string(outputs), timestamp(g.ExpiresAt), g.MaxDownloads, grantHash(token), time.Now().Unix())
	if err != nil {
		return "", err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return "", err
	}
	if n != 1 {
		return "", errors.New("download grant limit reached")
	}
	if err = pruneGrants(ctx, tx, time.Now()); err != nil {
		return "", err
	}
	return token, tx.Commit()
}
func readGrant(row scanner) (g Grant, err error) {
	var clients, outputs string
	var account sql.NullString
	var expires, limit sql.NullInt64
	err = row.Scan(&g.ID, &account, &g.Scope, &clients, &outputs, &expires, &limit, &g.Downloads, &g.Revoked)
	if err != nil {
		return
	}
	err = json.Unmarshal([]byte(clients), &g.Clients)
	if err != nil {
		return
	}
	g.AccountID = account.String
	err = json.Unmarshal([]byte(outputs), &g.Outputs)
	if expires.Valid {
		t := time.Unix(expires.Int64, 0).UTC()
		g.ExpiresAt = &t
	}
	if limit.Valid {
		g.MaxDownloads = &limit.Int64
	}
	g.State = g.Status(time.Now())
	return
}

const grantColumns = `id,account,scope,clients,outputs,expires,max_downloads,downloads,revoked`

func (s *Store) Grants(ctx context.Context) ([]Grant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+grantColumns+` FROM download_grants ORDER BY created DESC,id LIMIT 11000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Grant{}
	for rows.Next() {
		v, e := readGrant(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) RevokeGrant(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE download_grants SET revoked=1,updated=? WHERE id=? AND revoked=0`, time.Now().Unix(), id)
	return err
}
func (s *Store) Grant(ctx context.Context, token string) (Grant, Usage, error) {
	g, err := s.LookupGrant(ctx, token)
	if err != nil || g.AccountID == "" {
		return g, Usage{}, ErrDenied
	}
	u, err := s.VisibleAccount(ctx, g.AccountID, time.Now())
	return g, u, err
}

func (s *Store) LookupGrant(ctx context.Context, token string) (Grant, error) {
	if len(token) != 64 {
		return Grant{}, ErrDenied
	}
	g, err := readGrant(s.db.QueryRowContext(ctx, `SELECT `+grantColumns+` FROM download_grants WHERE hash=?`, grantHash(token)))
	if err != nil {
		return g, ErrDenied
	}
	now := time.Now()
	if g.Status(now) != "active" {
		return g, ErrDenied
	}
	return g, nil
}
func (s *Store) CountDownload(ctx context.Context, token, client string) error {
	g, err := s.LookupGrant(ctx, token)
	if err != nil {
		return err
	}
	if g.AccountID != "" {
		if !slices.Contains(g.Clients, client) {
			return ErrDenied
		}
		if _, err := s.DownloadAccount(ctx, g.AccountID, time.Now()); err != nil {
			return err
		}
	} else if !slices.Contains(g.Outputs, client) {
		return ErrDenied
	}
	r, err := s.db.ExecContext(ctx, `UPDATE download_grants SET downloads=downloads+1,updated=? WHERE hash=? AND revoked=0 AND (expires IS NULL OR expires>?) AND (max_downloads IS NULL OR downloads<max_downloads)`, time.Now().Unix(), grantHash(token), time.Now().Unix())
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n != 1 {
		return ErrDenied
	}
	return err
}
