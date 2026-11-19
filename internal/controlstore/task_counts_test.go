package controlstore

import (
	"path/filepath"
	"testing"
)

func TestAttentionCountsIncludeOlderAndPartialTasks(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 20; i++ {
		state, outcome := "succeeded", "passed"
		if i == 0 {
			state = "failed"
		}
		if i == 1 {
			outcome = "partial"
		}
		if i == 2 {
			outcome = "cached"
		}
		if _, err = s.db.ExecContext(t.Context(), `INSERT INTO control_tasks(kind,actor,state,phase,input,created,updated,outcome) VALUES('service','test',?,'done','{}',0,0,?)`, state, outcome); err != nil {
			t.Fatal(err)
		}
	}
	counts, err := s.TaskCounts(t.Context())
	if err != nil || counts.Attention != 3 || counts.Failed != 1 {
		t.Fatal(counts, err)
	}
	if err = s.Acknowledge(t.Context(), 2); err != nil {
		t.Fatal(err)
	}
	counts, err = s.TaskCounts(t.Context())
	if err != nil || counts.Attention != 2 {
		t.Fatal(counts, err)
	}
	recent, err := s.RecentTaskSummaries(t.Context())
	if err != nil || len(recent) != 12 {
		t.Fatal(recent, err)
	}
}
