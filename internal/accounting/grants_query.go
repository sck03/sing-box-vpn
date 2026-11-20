package accounting

import (
	"context"
	"errors"
	"strings"
	"time"
)

type GrantPage struct {
	Items  []Grant `json:"items"`
	Total  int     `json:"total"`
	Offset int     `json:"offset"`
	Limit  int     `json:"limit"`
}

func (s *Store) GrantPage(ctx context.Context, query string, offset, limit int) (GrantPage, error) {
	p := GrantPage{Items: []Grant{}, Offset: offset, Limit: limit}
	if offset < 0 || offset > 11000 || limit < 1 || limit > 100 || len(query) > 256 {
		return p, errors.New("invalid grant pagination")
	}
	// The same state precedence is exposed by Grant.Status and searched here.
	const predicate = `instr(lower(id||' '||COALESCE(account,'')||' '||outputs||' '||scope||' '||profiles||' '||
 CASE WHEN revoked=1 THEN 'revoked 已撤销' WHEN expires<=? THEN 'expired 已到期' WHEN downloads>=max_downloads THEN 'exhausted 次数用尽' ELSE 'active 有效' END),?)>0`
	args := []any{time.Now().Unix(), strings.ToLower(strings.TrimSpace(query))}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM download_grants WHERE `+predicate, args...).Scan(&p.Total); err != nil {
		return p, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+grantColumns+` FROM download_grants WHERE `+predicate+` ORDER BY created DESC,id LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return p, err
	}
	for rows.Next() {
		g, e := readGrant(rows)
		if e != nil {
			rows.Close()
			return p, e
		}
		p.Items = append(p.Items, g)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return p, err
	}
	return p, tx.Commit()
}
