package accounting

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestReservationBatchIsolatesFailuresAndCommitsQuota(t *testing.T) {
	s, _ := fixture(t, ReservationBytes+100)
	u, err := s.Advance(t.Context(), "alice", time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	makeRequest := func(id, account string, ctx context.Context) *reservationRequest {
		return &reservationRequest{ctx: ctx, id: id, account: account, runtime: "normal", want: ReservationBytes, now: time.Now(), period: u.PeriodID, result: make(chan reservationResult, 1)}
	}
	batch := []*reservationRequest{makeRequest("first", "alice", t.Context()), makeRequest("missing", "missing", t.Context()), makeRequest("cancelled", "alice", cancelled), makeRequest("last", "alice", t.Context())}
	s.commitReservations(batch, make([]reservationResult, len(batch)))
	for i, r := range batch {
		result := <-r.result
		switch i {
		case 0:
			if result.err != nil || result.bytes != ReservationBytes {
				t.Fatal(result)
			}
		case 1:
			if result.err == nil {
				t.Fatal("missing account accepted")
			}
		case 2:
			if !errors.Is(result.err, context.Canceled) {
				t.Fatal(result)
			}
		case 3:
			if result.err != nil || result.bytes != 100 {
				t.Fatal(result)
			}
		}
	}
	u, err = s.Account(t.Context(), "alice")
	if err != nil || u.Charged != ReservationBytes+100 {
		t.Fatal(u.Charged, err)
	}
}

// Run unchanged against the baseline to compare concurrent durable reservations.
func BenchmarkConcurrentDurableReservations(b *testing.B) {
	_, _, handles := benchmarkMeter(b, 64)
	var index atomic.Uint64
	b.SetParallelism(2)
	b.SetBytes(ReservationBytes)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := int(index.Add(1)) - 1
		if i >= len(handles) {
			b.Error(fmt.Errorf("benchmark supports at most %d workers", len(handles)))
			return
		}
		h := handles[i]
		for pb.Next() {
			n, err := h.Take(int(ReservationBytes))
			if err != nil || n != int(ReservationBytes) {
				b.Error(n, err)
				return
			}
			h.Complete(n, n, true)
		}
	})
}
