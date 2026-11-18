package accounting

import (
	"context"
	"database/sql"
	"errors"
	"github.com/sck03/sing-box-vpn/internal/domain"
	"time"
)

func (u *Usage) describe(now time.Time) {
	u.EffectiveExpiresAt = u.ExpiresAt
	u.State = "active"
	if u.StartedAt == nil || now.Before(*u.StartedAt) {
		u.State = "not_started"
	}
	if u.StartedAt != nil {
		_, u.NextReset = u.Period.Bounds(*u.StartedAt, now)
		if u.DurationSeconds > 0 {
			end := u.StartedAt.Add(time.Duration(u.DurationSeconds) * time.Second)
			if u.ExpiresAt == nil || end.Before(*u.ExpiresAt) {
				u.EffectiveExpiresAt = &end
			}
		}
	}
	if u.QuotaBytes != nil && u.Charged >= *u.QuotaBytes {
		u.State = "exhausted"
	}
	if u.EffectiveExpiresAt != nil && !now.Before(*u.EffectiveExpiresAt) {
		u.State = "expired"
	}
	if u.Disabled {
		u.State = "disabled"
	}
	if u.SubscriberDisabled {
		u.State = "subscriber_disabled"
	}
	if u.Archived {
		u.State = "archived"
		u.NextReset = nil
	}
}

// Advance is called by runtimes as well as queries. Downloading a subscription
// never activates a first-use entitlement.
func (s *Store) Advance(ctx context.Context, id string, now time.Time, activate bool) (Usage, error) {
	u, err := s.Account(ctx, id)
	if err != nil {
		return u, err
	}
	return s.advanceUsage(ctx, u, now, activate)
}

func (s *Store) advanceUsage(ctx context.Context, u Usage, now time.Time, activate bool) (Usage, error) {
	id := u.ID
	if u.Archived {
		return u, nil
	}
	needs := u.StartedAt == nil && activate
	if u.StartedAt != nil {
		start, _ := u.Period.Bounds(*u.StartedAt, now)
		needs = start.Unix() > u.PeriodID
	}
	if !needs {
		u.describe(now)
		return u, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return u, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE accounts SET period=period WHERE id=?`, id); err != nil {
		return u, err
	}
	u, err = readAccount(tx.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE id=?`, id))
	if err != nil {
		return u, err
	}
	if u.Archived {
		return u, nil
	}
	if u.StartedAt == nil && activate {
		if u.Disabled || (u.ExpiresAt != nil && !now.Before(*u.ExpiresAt)) {
			return u, ErrDenied
		}
		start := now.UTC().Truncate(time.Second)
		u.StartedAt = &start
		u.PeriodID = start.Unix()
		if _, err = tx.ExecContext(ctx, `UPDATE accounts SET started=?,period=? WHERE id=?`, u.PeriodID, u.PeriodID, id); err != nil {
			return u, err
		}
		if err = record(ctx, tx, id, "activate", "first authenticated proxy connection"); err != nil {
			return u, err
		}
	}
	if u.StartedAt != nil {
		start, _ := u.Period.Bounds(*u.StartedAt, now)
		if start.Unix() > u.PeriodID {
			if _, err = tx.ExecContext(ctx, `INSERT INTO usage_periods(account,period,charged,upload,download) VALUES(?,?,?,?,?) ON CONFLICT(account,period) DO NOTHING`, id, u.PeriodID, u.Charged, u.Upload, u.Download); err != nil {
				return u, err
			}
			u.PeriodID = start.Unix()
			u.Charged, u.Upload, u.Download = 0, 0, 0
			if _, err = tx.ExecContext(ctx, `UPDATE accounts SET period=?,charged=0,upload=0,download=0 WHERE id=?`, u.PeriodID, id); err != nil {
				return u, err
			}
			if err = record(ctx, tx, id, "period", "calendar quota reset"); err != nil {
				return u, err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return u, err
	}
	u.describe(now)
	return u, nil
}

func (s *Store) authorize(ctx context.Context, credential string, generation int64, now time.Time, activate bool) (Usage, error) {
	u, err := readAccount(s.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts
	 WHERE id=(SELECT account FROM credentials WHERE id=? AND generation=?)
	 AND NOT EXISTS (SELECT 1 FROM subscribers WHERE subscribers.id=accounts.subscriber AND subscribers.disabled=1)`, credential, generation))
	if errors.Is(err, sql.ErrNoRows) {
		return Usage{}, ErrDenied
	}
	if err != nil {
		return Usage{}, err
	}
	u, err = s.advanceUsage(ctx, u, now, activate)
	if err != nil {
		return u, err
	}
	// Outstanding reservations may still be spent when charged equals quota.
	if !permitted(u, now) {
		return u, ErrDenied
	}
	return u, nil
}

type Subscriber struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Disabled bool   `json:"disabled"`
	Revision int64  `json:"revision"`
}

func (s *Store) Subscribers(ctx context.Context) ([]Subscriber, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,disabled,revision FROM subscribers ORDER BY id LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Subscriber{}
	for rows.Next() {
		var v Subscriber
		if err = rows.Scan(&v.ID, &v.Name, &v.Disabled, &v.Revision); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

func (s *Store) SaveSubscriber(ctx context.Context, v Subscriber) error {
	defer s.authorizationEpoch.Add(1)
	if !domain.ResourceID.MatchString(v.ID) || len(v.Name) == 0 || len(v.Name) > 128 {
		return errors.New("invalid subscriber")
	}
	var r sql.Result
	var err error
	if v.Revision == 0 {
		r, err = s.db.ExecContext(ctx, `INSERT INTO subscribers(id,name,disabled) SELECT ?,?,? WHERE (SELECT COUNT(*) FROM subscribers)<1000`, v.ID, v.Name, v.Disabled)
	} else {
		r, err = s.db.ExecContext(ctx, `UPDATE subscribers SET name=?,disabled=?,revision=revision+1 WHERE id=? AND revision=?`, v.Name, v.Disabled, v.ID, v.Revision)
	}
	if err == nil {
		n, e := r.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return ErrConflict
		}
	}
	return err
}

func (s *Store) History(ctx context.Context, id string) ([]map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT period,charged,upload,download FROM usage_periods WHERE account=? ORDER BY period DESC LIMIT 120`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []map[string]int64{}
	for rows.Next() {
		var p, c, u, d int64
		if err = rows.Scan(&p, &c, &u, &d); err != nil {
			return nil, err
		}
		result = append(result, map[string]int64{"period_id": p, "charged_bytes": c, "upload_bytes": u, "download_bytes": d})
	}
	return result, rows.Err()
}
