package accounting

import (
	"errors"
	"github.com/sck03/sing-box-vpn/internal/domain"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func periodFixture(t *testing.T, mode string) (*Store, string, domain.Credential) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "control.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	quota := int64(1000)
	a := domain.Account{ID: "alice", Name: "Alice", StartMode: mode, QuotaBytes: &quota, Period: domain.PeriodPolicy{Kind: "days", Days: 1, Timezone: "UTC"}}
	if mode == "scheduled" {
		start := time.Now().Add(time.Hour)
		a.StartsAt = &start
	}
	if e = s.SaveAccount(t.Context(), a); e != nil {
		t.Fatal(e)
	}
	c := domain.Credential{ID: "cred", AccountID: "alice", RuntimeID: "normal", InboundID: "vmess", Protocol: "vmess", UUID: "aabbccdd-1122-4333-8444-5566778899aa", Generation: 1}
	if e = s.BindBatch(t.Context(), []domain.Credential{c}); e != nil {
		t.Fatal(e)
	}
	return s, path, c
}
func TestFirstUseDownloadAndRevocation(t *testing.T) {
	s, _, c := periodFixture(t, "first_use")
	ctx := t.Context()
	token, e := s.CreateGrant(ctx, Grant{ID: "g", AccountID: "alice", Profiles: []string{"uri"}})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.CountDownload(ctx, token, "uri"); e != nil {
		t.Fatal(e)
	}
	u, _ := s.Account(ctx, "alice")
	if u.StartedAt != nil {
		t.Fatal("download activated entitlement")
	}
	if _, e = s.authorize(ctx, c.ID, 1, time.Now(), false); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
	u, e = s.authorize(ctx, c.ID, 1, time.Now(), true)
	if e != nil || u.StartedAt == nil {
		t.Fatal(u, e)
	}
	if e = s.Unbind(ctx, c.ID, 1); e != nil {
		t.Fatal(e)
	}
	if e = s.BindBatch(ctx, []domain.Credential{c}); e == nil {
		t.Fatal("revoked ID reused")
	}
	if _, e = s.authorize(ctx, c.ID, 1, time.Now(), false); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
}
func TestPeriodLateCheckpointAndClockRollback(t *testing.T) {
	s, path, c := periodFixture(t, "first_use")
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Second)
	if _, e := s.authorize(ctx, c.ID, 1, now, true); e != nil {
		t.Fatal(e)
	}
	other, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	if n, e := s.Reserve(ctx, "old-normal", "alice", "normal", 600, now); e != nil || n != 600 {
		t.Fatal(n, e)
	}
	if n, e := other.Reserve(ctx, "old-vpn", "alice", "vpngate", 600, now); e != nil || n != 400 {
		t.Fatal(n, e)
	}
	next := now.Add(24 * time.Hour)
	u, e := other.Advance(ctx, "alice", next, false)
	if e != nil || u.Charged != 0 {
		t.Fatal(u, e)
	}
	if _, e = s.Reserve(ctx, "new", "alice", "normal", 700, next); e != nil {
		t.Fatal(e)
	}
	for range 2 {
		if e = s.Checkpoint(ctx, "old-normal", 200, 100, true); e != nil {
			t.Fatal(e)
		}
	}
	if e = other.Checkpoint(ctx, "old-vpn", 100, 100, true); e != nil {
		t.Fatal(e)
	}
	u, e = s.Advance(ctx, "alice", now, false)
	if e != nil || u.Charged != 700 || u.PeriodID != next.Unix() {
		t.Fatal("rollback/refund changed current period", u, e)
	}
	history, e := s.History(ctx, "alice")
	if e != nil || len(history) != 1 || history[0]["charged_bytes"] != 500 {
		t.Fatal(history, e)
	}
	u.Account.Disabled = true
	if e = s.SaveAccount(ctx, u.Account); e != nil {
		t.Fatal(e)
	}
	if _, e = s.authorize(ctx, c.ID, 1, next.Add(24*time.Hour), false); !errors.Is(e, ErrDenied) {
		t.Fatal("reset re-enabled user", e)
	}
}
func TestScheduledAndConcurrentDownloadLimits(t *testing.T) {
	s, _, c := periodFixture(t, "scheduled")
	ctx := t.Context()
	if _, e := s.authorize(ctx, c.ID, 1, time.Now(), true); !errors.Is(e, ErrDenied) {
		t.Fatal("scheduled user activated early", e)
	}
	limit := int64(7)
	token, e := s.CreateGrant(ctx, Grant{ID: "limited", AccountID: "alice", Profiles: []string{"uri"}, MaxDownloads: &limit})
	if e != nil {
		t.Fatal(e)
	}
	var ok atomic.Int64
	var wg sync.WaitGroup
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.CountDownload(ctx, token, "uri") == nil {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 7 {
		t.Fatal("download race", ok.Load())
	}
	if e = s.CountDownload(ctx, token, "sing-box"); !errors.Is(e, ErrDenied) {
		t.Fatal("wrong client", e)
	}
	if e = s.RevokeGrant(ctx, "limited"); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.Grant(ctx, token); !errors.Is(e, ErrDenied) {
		t.Fatal("revoked grant", e)
	}
}
