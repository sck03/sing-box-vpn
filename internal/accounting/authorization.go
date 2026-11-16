package accounting

import (
	"context"
	"time"
)

// VisibleAccount checks ownership and validity without starting a first-use
// entitlement. Exhausted accounts remain visible on the usage page.
func (s *Store) VisibleAccount(ctx context.Context, id string, now time.Time) (Usage, error) {
	u, err := s.Advance(ctx, id, now, false)
	if err != nil {
		return u, err
	}
	if u.State == "disabled" || u.State == "expired" {
		return u, ErrDenied
	}
	if u.SubscriberID != "" {
		var disabled bool
		if err = s.db.QueryRowContext(ctx, `SELECT disabled FROM subscribers WHERE id=?`, u.SubscriberID).Scan(&disabled); err != nil {
			return u, err
		}
		if disabled {
			return u, ErrDenied
		}
	}
	return u, nil
}

// DownloadAccount is shared by exports, downloads and outbound delivery.
func (s *Store) DownloadAccount(ctx context.Context, id string, now time.Time) (Usage, error) {
	u, err := s.VisibleAccount(ctx, id, now)
	if err == nil && u.State == "exhausted" {
		err = ErrDenied
	}
	return u, err
}
