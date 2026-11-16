package accounting

import (
	"testing"
	"time"
)

func TestCheckpointBatchAtomicReplayAndRefund(t *testing.T) {
	s, _ := fixture(t, 1000000)
	for _, id := range []string{"a", "b"} {
		if _, err := s.Reserve(t.Context(), id, "alice", "normal", ReservationBytes, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	invalid := []UsageCheckpoint{{"a", 100, 200, true}, {"b", ReservationBytes + 1, 0, false}}
	if err := s.CheckpointBatch(t.Context(), invalid); err == nil {
		t.Fatal("invalid batch accepted")
	}
	u, _ := s.Account(t.Context(), "alice")
	if u.Upload != 0 || u.Charged != 2*ReservationBytes {
		t.Fatal("partial batch committed", u)
	}
	valid := []UsageCheckpoint{{"a", 100, 200, true}, {"b", 300, 400, true}}
	for range 2 {
		if err := s.CheckpointBatch(t.Context(), valid); err != nil {
			t.Fatal(err)
		}
	}
	u, _ = s.Account(t.Context(), "alice")
	if u.Upload != 400 || u.Download != 600 || u.Charged != 1000 {
		t.Fatal("replay or refund corrupted totals", u)
	}
}
