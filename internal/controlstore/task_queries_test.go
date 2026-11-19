package controlstore

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestTaskOverviewBoundAndCursorHistory(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 65; i++ {
		state := "succeeded"
		if i == 0 {
			state = "failed"
		}
		if _, err = s.db.Exec(`INSERT INTO control_tasks(kind,actor,lane,state,phase,created,updated,input) VALUES('service','test','control',?,'executing',1,1,'{}')`, state); err != nil {
			t.Fatal(err)
		}
	}
	recent, err := s.RecentTaskSummaries(t.Context())
	if err != nil || len(recent) != 12 {
		t.Fatal(len(recent), err)
	}
	counts, err := s.TaskCounts(t.Context())
	if err != nil || counts.Failed != 1 {
		t.Fatal("older failure missing from counts", counts, err)
	}
	seen, before := map[int64]bool{}, int64(0)
	for {
		page, err := s.TaskPage(t.Context(), before, 17)
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range page.Items {
			if seen[task.ID] || (before != 0 && task.ID >= before) {
				t.Fatal("unstable cursor", task.ID)
			}
			seen[task.ID] = true
		}
		before = page.NextBefore
		if before == 0 {
			break
		}
	}
	if len(seen) != 65 {
		t.Fatal(fmt.Sprintf("history omitted %d records", 65-len(seen)))
	}
}
