package accounting

import (
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"
)

func TestMeterCloseBatchesAndPreservesReplayBarrier(t *testing.T) {
	s, _ := fixture(t, 1000000)
	m := NewMeter(s, "normal")
	// More than two batches, including detached pools from failed ticks.
	for i := 1; i <= 130; i++ {
		id := m.epoch + "/" + strconv.Itoa(i)
		if _, err := s.Reserve(t.Context(), id, "alice", "normal", 1000, time.Now()); err != nil {
			t.Fatal(err)
		}
		p := &pool{id: id, sequence: uint64(i), reserved: true, upload: int64(i), changed: make(chan struct{})}
		if i%2 == 0 {
			m.detached = append(m.detached, p)
		} else {
			m.pools[fmt.Sprint(i)] = p
		}
	}
	m.poolSequence = 130
	for range 2 {
		if err := m.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	u, err := s.Account(t.Context(), "alice")
	if err != nil || u.Charged != 130*131/2 || u.Upload != u.Charged {
		t.Fatal(u, err)
	}
	if _, err := s.Reserve(t.Context(), m.epoch+"/131", "alice", "normal", 1, time.Now()); !errors.Is(err, ErrDenied) {
		t.Fatal("closed epoch replayed", err)
	}
}

func TestReservationsRejectUnscopedLeaseIDs(t *testing.T) {
	s, _ := fixture(t, 1000)
	u, err := s.Advance(t.Context(), "alice", time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.reservePeriod(t.Context(), "legacy-id", "alice", "normal", 1, time.Now(), u.PeriodID); !errors.Is(err, ErrDenied) {
		t.Fatal("unscoped lease accepted", err)
	}
}
