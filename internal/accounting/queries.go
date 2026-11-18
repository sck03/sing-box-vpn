package accounting

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/sck03/sing-box-vpn/internal/application"
)

func (s *Store) AccountOptions(ctx context.Context) ([]application.Option, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name FROM accounts WHERE archived=0 ORDER BY name,id LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []application.Option{}
	for rows.Next() {
		var v application.Option
		if err = rows.Scan(&v.ID, &v.Name); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

func (s *Store) AccountPage(ctx context.Context, q application.PageQuery, now time.Time, archived bool) (application.Page[Usage], error) {
	result := application.Page[Usage]{Items: []Usage{}, Offset: q.Offset, Limit: q.Limit}
	if err := q.Validate(); err != nil {
		return result, err
	}
	order := map[string]string{"": "created,id", "name": "name,id", "id": "id", "created": "created,id"}[q.Sort]
	if order == "" {
		return result, errors.New("unsupported account sort")
	}
	if q.Descending {
		order = strings.ReplaceAll(order, ",", " DESC,") + " DESC"
	}
	filter := `archived=? AND (?='' OR instr(lower(id||' '||name||' '||subscriber),lower(?))>0)`
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM accounts WHERE `+filter, archived, q.Search, q.Search).Scan(&result.Total); err != nil {
		return result, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+accountColumns+`,EXISTS(SELECT 1 FROM subscribers s WHERE s.id=accounts.subscriber AND s.disabled=1) FROM accounts WHERE `+filter+` ORDER BY `+order+` LIMIT ? OFFSET ?`, archived, q.Search, q.Search, q.Limit, q.Offset)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var disabled bool
		u, e := readAccount(scanFunc(func(values ...any) error { return rows.Scan(append(values, &disabled)...) }))
		if e != nil {
			rows.Close()
			return result, e
		}
		u.SubscriberDisabled = disabled
		result.Items = append(result.Items, u)
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	for i, u := range result.Items {
		next, e := s.advanceUsage(ctx, u, now, false)
		if e != nil {
			return result, e
		}
		next.SubscriberDisabled = u.SubscriberDisabled
		next.describe(now)
		result.Items[i] = next
	}
	return result, nil
}
