package accounting

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

func fixture(t *testing.T, quota int64) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "accounts.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err = s.SaveAccount(t.Context(), domain.Account{ID: "alice", Name: "Alice", QuotaBytes: &quota}); err != nil {
		t.Fatal(err)
	}
	return s, path
}

func TestSharedReservationNeverOverspends(t *testing.T) {
	s, path := fixture(t, 100000)
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	var wg sync.WaitGroup
	amounts := make(chan int64, 2)
	for i, db := range []*Store{s, other} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := db.Reserve(t.Context(), string(rune('a'+i)), "alice", "runtime", ReservationBytes, time.Now())
			if err != nil {
				t.Error(err)
			}
			amounts <- n
		}()
	}
	wg.Wait()
	close(amounts)
	var sum int64
	for n := range amounts {
		sum += n
	}
	if sum != 100000 {
		t.Fatal(sum)
	}
	if _, err = s.Reserve(t.Context(), "c", "alice", "runtime", 1, time.Now()); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
}

func TestReplayCrashAndCleanClose(t *testing.T) {
	s, path := fixture(t, 200000)
	ctx := t.Context()
	if _, err := s.Reserve(ctx, "epoch1", "alice", "normal", ReservationBytes, time.Now()); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.Checkpoint(ctx, "epoch1", 1000, 2000, false); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	restored, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	u, err := restored.Account(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if u.Charged != ReservationBytes || u.Upload != 1000 || u.Download != 2000 {
		t.Fatal(u)
	}
	if err = restored.Checkpoint(ctx, "epoch1", 1000, 2000, true); err != nil {
		t.Fatal(err)
	}
	if err = restored.Checkpoint(ctx, "epoch1", 1000, 2000, true); err != nil {
		t.Fatal(err)
	}
	u, _ = restored.Account(ctx, "alice")
	if u.Charged != 3000 {
		t.Fatal(u)
	}
	if _, err = restored.Reserve(ctx, "epoch1", "alice", "normal", 1, time.Now()); err == nil {
		t.Fatal("closed epoch reopened")
	}
}

func TestExpiryDisableRevisionAndGeneration(t *testing.T) {
	s, _ := fixture(t, 100000)
	ctx := context.Background()
	c := domain.Credential{ID: "c1", AccountID: "alice", RuntimeID: "normal", InboundID: "vless", Protocol: "vless", Generation: 1, UUID: "aabbccdd-1122-4333-8444-5566778899aa"}
	if err := s.Bind(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authorized(ctx, "c1", 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.Rotate(ctx, "c1", "bbbbccdd-1122-4333-8444-5566778899aa", "", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authorized(ctx, "c1", 1, time.Now()); !errors.Is(err, ErrDenied) {
		t.Fatal("old generation accepted", err)
	}
	u, _ := s.Account(ctx, "alice")
	a := u.Account
	a.Disabled = true
	if err := s.SaveAccount(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveAccount(ctx, a); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err := s.Authorized(ctx, "c1", 2, time.Now()); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	if _, err := s.Reserve(ctx, "l", "alice", "normal", 1, time.Now()); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	u, _ = s.Account(ctx, "alice")
	a = u.Account
	a.Disabled = false
	expired := time.Now().Add(-time.Second)
	a.ExpiresAt = &expired
	if err := s.SaveAccount(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reserve(ctx, "l", "alice", "normal", 1, time.Now()); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
}

func TestInvalidCheckpointCannotRefundQuota(t *testing.T) {
	s, _ := fixture(t, 100000)
	ctx := t.Context()
	if _, err := s.Reserve(ctx, "l", "alice", "normal", 100, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, values := range [][2]int64{{-1, 0}, {100, 1}, {1 << 62, 1 << 62}} {
		if err := s.Checkpoint(ctx, "l", values[0], values[1], true); err == nil {
			t.Fatal(values)
		}
	}
	u, _ := s.Account(ctx, "alice")
	if u.Charged != 100 {
		t.Fatal(u)
	}
}
