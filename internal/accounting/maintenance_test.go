package accounting

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMeterCompactionAndReplay(t *testing.T) {
	s, m, h, _ := meterFixture(t, 1000000)
	old := h.pool.id
	n, err := h.Take(321)
	if err != nil {
		t.Fatal(err)
	}
	h.Complete(n, n, true)
	h.Close()
	if err = m.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM leases`).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	if _, err = s.Reserve(t.Context(), old, "alice", "normal", 1, time.Now()); !errors.Is(err, ErrDenied) {
		t.Fatal("retired lease replayed", err)
	}
	h, err = m.Open(t.Context(), "credential.1", "in", &closedCounter{})
	if err != nil {
		t.Fatal(err)
	}
	n, err = h.Take(123)
	if err != nil {
		t.Fatal(err)
	}
	h.Complete(n, n, false)
	if err = m.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Reserve(t.Context(), m.epoch+"/1000", "alice", "normal", 1, time.Now()); !errors.Is(err, ErrDenied) {
		t.Fatal("closed scope replayed", err)
	}
	u, err := s.Account(t.Context(), "alice")
	if err != nil || u.Charged != 444 {
		t.Fatal(u, err)
	}
}

func TestMeterFailedCheckpointCanSettleOnClose(t *testing.T) {
	s, m, h, _ := meterFixture(t, 1000000)
	n, err := h.Take(99)
	if err != nil {
		t.Fatal(err)
	}
	h.Complete(n, n, true)
	h.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err = m.Tick(ctx); err == nil {
		t.Fatal("expected canceled checkpoint")
	}
	if err = m.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	u, err := s.Account(t.Context(), "alice")
	if err != nil || u.Charged != 99 {
		t.Fatal(u, err)
	}
}

func TestMeterConcurrentSettlement(t *testing.T) {
	s, m, h, _ := meterFixture(t, 10000000)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 200 {
				n, err := h.Take(31)
				if err != nil {
					t.Error(err)
					return
				}
				h.Complete(n, n, true)
			}
		})
	}
	wg.Go(func() {
		for range 8 {
			if err := m.Tick(t.Context()); err != nil {
				t.Error(err)
				return
			}
		}
	})
	wg.Wait()
	if err := m.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	u, err := s.Account(t.Context(), "alice")
	if err != nil || u.Charged != 8*200*31 {
		t.Fatal(u, err)
	}
}

func TestOutputGrantConcurrentLimitsAndRevocation(t *testing.T) {
	s, path := fixture(t, 1000)
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	limit := int64(7)
	token, err := s.CreateGrant(t.Context(), Grant{ID: "output", Outputs: []string{"custom"}, MaxDownloads: &limit})
	if err != nil {
		t.Fatal(err)
	}
	var passed atomic.Int32
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			if err := []*Store{s, other}[i%2].CountDownload(t.Context(), token, "custom"); err == nil {
				passed.Add(1)
			} else if !errors.Is(err, ErrDenied) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if passed.Load() != 7 {
		t.Fatal(passed.Load())
	}
	zero := int64(0)
	for _, limit := range []*int64{&zero, nil} {
		id := "disabled"
		if limit == nil {
			id = "unlimited"
		}
		token, err := s.CreateGrant(t.Context(), Grant{ID: id, Outputs: []string{"custom"}, MaxDownloads: limit})
		if err != nil {
			t.Fatal(err)
		}
		err = other.CountDownload(t.Context(), token, "custom")
		if (err == nil) != (limit == nil) {
			t.Fatal(err)
		}
		if err = s.RevokeGrant(t.Context(), id); err != nil {
			t.Fatal(err)
		}
		if _, err = other.LookupGrant(t.Context(), token); !errors.Is(err, ErrDenied) {
			t.Fatal(err)
		}
	}
}

func TestMeterIdleTickDoesNotWriteCheckpoint(t *testing.T) {
	s, m, h, _ := meterFixture(t, 1000000)
	n, err := h.Take(1)
	if err != nil {
		t.Fatal(err)
	}
	h.Complete(n, n, true)
	if err = m.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	var before, after int64
	if err = s.db.QueryRow(`SELECT total_changes()`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err = m.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow(`SELECT total_changes()`).Scan(&after); err != nil || before != after {
		t.Fatal(before, after, err)
	}
	if err = m.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestMeterObservesCrossStoreRevocation(t *testing.T) {
	s, m, h, _ := meterFixture(t, 1000000)
	n, err := h.Take(1)
	if err != nil {
		t.Fatal(err)
	}
	h.Complete(n, n, true)
	var path string
	var seq int
	var name string
	if err = s.db.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	u, err := other.Account(t.Context(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	u.Disabled = true
	if err = other.SaveAccount(t.Context(), u.Account); err != nil {
		t.Fatal(err)
	}
	time.Sleep(AuthorizationInterval + 30*time.Millisecond)
	if _, err = h.Take(1); !errors.Is(err, ErrDenied) {
		t.Fatal("stale cross-process authorization", err)
	}
	if err = m.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
}
