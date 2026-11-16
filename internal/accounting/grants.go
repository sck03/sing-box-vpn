package accounting

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/sck03/sing-box-vpn/internal/domain"
	"slices"
	"time"
)

type Grant struct {
	ID           string     `json:"id"`
	AccountID    string     `json:"account_id"`
	Scope        string     `json:"scope"`
	Clients      []string   `json:"clients"`
	ExpiresAt    *time.Time `json:"expires_at"`
	MaxDownloads *int64     `json:"max_downloads"`
	Downloads    int64      `json:"downloads"`
	Revoked      bool       `json:"revoked"`
}

func grantHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func (s *Store) CreateGrant(ctx context.Context, g Grant) (string, error) {
	if !domain.ResourceID.MatchString(g.ID) || !domain.ResourceID.MatchString(g.AccountID) || !slices.Contains([]string{"", "normal", "vpngate"}, g.Scope) || len(g.Clients) == 0 || len(g.Clients) > 3 {
		return "", errors.New("invalid download grant")
	}
	for _, client := range g.Clients {
		if !slices.Contains([]string{"sing-box", "mihomo", "uri"}, client) {
			return "", errors.New("unsupported client")
		}
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
	r, err := s.db.ExecContext(ctx, `INSERT INTO account_grants(id,account,scope,clients,expires,max_downloads,hash) SELECT ?,?,?,?,?,?,? WHERE (SELECT COUNT(*) FROM account_grants)<10000`, g.ID, g.AccountID, g.Scope, string(clients), timestamp(g.ExpiresAt), g.MaxDownloads, grantHash(token))
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
	return token, nil
}
func readGrant(row scanner) (g Grant, err error) {
	var clients string
	var expires, limit sql.NullInt64
	err = row.Scan(&g.ID, &g.AccountID, &g.Scope, &clients, &expires, &limit, &g.Downloads, &g.Revoked)
	if err != nil {
		return
	}
	err = json.Unmarshal([]byte(clients), &g.Clients)
	if expires.Valid {
		t := time.Unix(expires.Int64, 0).UTC()
		g.ExpiresAt = &t
	}
	if limit.Valid {
		g.MaxDownloads = &limit.Int64
	}
	return
}

const grantColumns = `id,account,scope,clients,expires,max_downloads,downloads,revoked`

func (s *Store) Grants(ctx context.Context) ([]Grant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+grantColumns+` FROM account_grants ORDER BY id LIMIT 10000`)
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
	_, err := s.db.ExecContext(ctx, `UPDATE account_grants SET revoked=1 WHERE id=?`, id)
	return err
}
func (s *Store) Grant(ctx context.Context, token string) (Grant, Usage, error) {
	if len(token) != 64 {
		return Grant{}, Usage{}, ErrDenied
	}
	g, err := readGrant(s.db.QueryRowContext(ctx, `SELECT `+grantColumns+` FROM account_grants WHERE hash=?`, grantHash(token)))
	if err != nil {
		return g, Usage{}, ErrDenied
	}
	now := time.Now()
	if g.Revoked || (g.ExpiresAt != nil && !now.Before(*g.ExpiresAt)) {
		return g, Usage{}, ErrDenied
	}
	u, err := s.Advance(ctx, g.AccountID, now, false)
	if err != nil {
		return g, u, err
	}
	if u.Disabled || (u.ExpiresAt != nil && !now.Before(*u.ExpiresAt)) {
		return g, u, ErrDenied
	}
	if u.SubscriberID != "" {
		var disabled bool
		if err = s.db.QueryRowContext(ctx, `SELECT disabled FROM subscribers WHERE id=?`, u.SubscriberID).Scan(&disabled); err != nil || disabled {
			return g, u, ErrDenied
		}
	}
	return g, u, nil
}
func (s *Store) CountDownload(ctx context.Context, token, client string) error {
	g, _, err := s.Grant(ctx, token)
	if err != nil {
		return err
	}
	if !slices.Contains(g.Clients, client) {
		return ErrDenied
	}
	r, err := s.db.ExecContext(ctx, `UPDATE account_grants SET downloads=downloads+1 WHERE hash=? AND revoked=0 AND (expires IS NULL OR expires>?) AND (max_downloads IS NULL OR downloads<max_downloads)`, grantHash(token), time.Now().Unix())
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n != 1 {
		return ErrDenied
	}
	return err
}
