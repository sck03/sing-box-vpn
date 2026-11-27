package accounting

import (
	"context"
	"database/sql"
	"errors"
	"github.com/sck03/sing-box-vpn/internal/domain"
	"strings"
	"time"
)

// AccountUsage is the effective, non-activating view used by all read APIs.
// Account remains the raw record reader used by accounting mutations.
func (s *Store) AccountUsage(ctx context.Context, id string, now time.Time) (Usage, error) {
	u, err := readAccountUsage(s.db.QueryRowContext(ctx, `SELECT `+accountUsageColumns+` FROM accounts WHERE id=?`, id))
	if err != nil {
		return u, err
	}
	return s.effectiveUsage(ctx, u, now)
}

const accountUsageColumns = accountColumns + `,EXISTS(SELECT 1 FROM subscribers s WHERE s.id=accounts.subscriber AND s.disabled=1)`

func readAccountUsage(row scanner) (Usage, error) {
	var disabled bool
	u, err := readAccount(scanFunc(func(values ...any) error { return row.Scan(append(values, &disabled)...) }))
	u.SubscriberDisabled = disabled
	return u, err
}

func (s *Store) effectiveUsage(ctx context.Context, u Usage, now time.Time) (Usage, error) {
	next, err := s.advanceUsage(ctx, u, now, false)
	next.SubscriberDisabled = u.SubscriberDisabled
	next.describe(now)
	return next, err
}

func (s *Store) AccountOptions(ctx context.Context) ([]domain.Option, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name FROM accounts WHERE archived=0 ORDER BY name,id LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []domain.Option{}
	for rows.Next() {
		var v domain.Option
		if err = rows.Scan(&v.ID, &v.Name); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

func (s *Store) AccountPage(ctx context.Context, q domain.PageQuery, now time.Time, archived bool) (domain.Page[Usage], error) {
	result := domain.Page[Usage]{Items: []Usage{}, Offset: q.Offset, Limit: q.Limit}
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
	rows, err := tx.QueryContext(ctx, `SELECT `+accountUsageColumns+` FROM accounts WHERE `+filter+` ORDER BY `+order+` LIMIT ? OFFSET ?`, archived, q.Search, q.Search, q.Limit, q.Offset)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		u, e := readAccountUsage(rows)
		if e != nil {
			rows.Close()
			return result, e
		}
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
		next, e := s.effectiveUsage(ctx, u, now)
		if e != nil {
			return result, e
		}
		result.Items[i] = next
	}
	return result, nil
}
