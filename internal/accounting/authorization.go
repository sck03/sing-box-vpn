package accounting

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// VisibleAccount never activates first use; exhausted accounts remain visible.
func (s *Store) VisibleAccount(ctx context.Context, id string, now time.Time) (Usage, error) {
	u, err := s.AccountUsage(ctx, id, now)
	if err != nil {
		return u, err
	}
	if u.Archived || u.State == "disabled" || u.State == "expired" || u.SubscriberDisabled {
		return u, ErrDenied
	}
	return u, nil
}

// DownloadDenial applies to a visible entitlement and its optional grant.
func (u Usage) DownloadDenial(g Grant) string {
	switch u.State {
	case "archived", "disabled", "expired", "subscriber_disabled":
		return u.State
	case "exhausted":
		return "quota_exhausted"
	}
	if g.MaxDownloads != nil && g.Downloads >= *g.MaxDownloads {
		return "download_limit"
	}
	return ""
}

func (s *Store) DownloadAccount(ctx context.Context, id string, now time.Time) (Usage, error) {
	u, err := s.VisibleAccount(ctx, id, now)
	if err == nil && u.DownloadDenial(Grant{}) != "" {
		err = ErrDenied
	}
	return u, err
}

type scanFunc func(...any) error

func (f scanFunc) Scan(values ...any) error { return f(values...) }

type authorizationKey struct {
	id         string
	generation int64
}

// Owned exclusively by Meter.maintenance; rows are cleared after each tick.
// Reuse container capacity, never authorization decisions between snapshots.
type authorizationRead struct {
	ids         []string
	credentials map[authorizationKey]*Usage
	accounts    map[string]*Usage
	values      []Usage
}

func (r *authorizationRead) reset() {
	clear(r.ids)
	r.ids = r.ids[:0]
	clear(r.credentials)
	clear(r.accounts)
	clear(r.values)
	r.values = r.values[:0]
}

func permitted(u Usage, now time.Time) bool {
	// Existing durable reservations remain usable when charged equals quota.
	return !u.Archived && !u.Disabled && u.StartedAt != nil && !now.Before(*u.StartedAt) &&
		(u.EffectiveExpiresAt == nil || now.Before(*u.EffectiveExpiresAt)) && (u.QuotaBytes == nil || u.Charged <= *u.QuotaBytes)
}

// One bounded read replaces a query per active credential on each meter tick.
// No transaction is held while advancing a period or updating in-memory caches.
func (s *Store) authorizationSnapshot(ctx context.Context, runtime string, now time.Time, wanted map[authorizationKey]bool, scratch *authorizationRead) (map[authorizationKey]*Usage, error) {
	for key := range wanted {
		scratch.ids = append(scratch.ids, key.id)
	}
	selected, err := json.Marshal(scratch.ids)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,c.generation,a.id,a.disabled,a.expires,a.quota,a.charged,a.duration,a.period_kind,a.period_days,a.period_day,a.timezone,a.started,a.period,a.archived
 FROM credentials c JOIN accounts a ON a.id=c.account
 WHERE c.runtime=? AND c.id IN (SELECT value FROM json_each(?)) AND NOT EXISTS (SELECT 1 FROM subscribers s WHERE s.id=a.subscriber AND s.disabled=1) LIMIT 10001`, runtime, string(selected))
	if err != nil {
		return nil, err
	}
	if scratch.credentials == nil {
		scratch.credentials = make(map[authorizationKey]*Usage, len(wanted))
		scratch.accounts = make(map[string]*Usage, len(wanted))
	}
	if cap(scratch.values) < len(wanted) {
		scratch.values = make([]Usage, 0, len(wanted))
	}
	result := scratch.credentials
	var key authorizationKey
	// Only authorization and period fields enter this hot path. advanceUsage
	// rereads the full account under its write transaction if rollover is due.
	var expires, quota, started sql.NullInt64
	var u Usage
	for rows.Next() {
		u = Usage{}
		e := rows.Scan(&key.id, &key.generation, &u.ID, &u.Disabled, &expires, &quota, &u.Charged, &u.DurationSeconds, &u.Period.Kind, &u.Period.Days, &u.Period.Day, &u.Period.Timezone, &started, &u.PeriodID, &u.Archived)
		if e != nil {
			rows.Close()
			return nil, e
		}
		if expires.Valid {
			v := time.Unix(expires.Int64, 0).UTC()
			u.ExpiresAt = &v
		}
		if quota.Valid {
			v := quota.Int64
			u.QuotaBytes = &v
		}
		if started.Valid {
			v := time.Unix(started.Int64, 0).UTC()
			u.StartedAt = &v
		}
		if wanted[key] {
			scratch.values = append(scratch.values, u)
			result[key] = &scratch.values[len(scratch.values)-1]
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	accounts := scratch.accounts
	for key, u := range result {
		if cached, ok := accounts[u.ID]; ok {
			u = cached
		} else {
			*u, err = s.advanceUsage(ctx, *u, now, false)
			if err != nil {
				return nil, err
			}
			accounts[u.ID] = u
		}
		if permitted(*u, now) {
			result[key] = u
		} else {
			delete(result, key)
		}
	}
	return result, nil
}
