package controlstore

import (
	"path/filepath"
	"testing"
)

func TestAccountBatchRespectsControlOrder(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Initialize(t.Context(), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, kind := range []string{"account", "account", "service", "account"} {
		id, err := s.Queue(t.Context(), kind, "admin", []byte(`{}`), 0, nil)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	lead, err := s.ClaimLane(t.Context(), "control")
	if err != nil {
		t.Fatal(err)
	}
	followers, err := s.ClaimAccountFollowers(t.Context(), lead.ID)
	if err != nil || len(followers) != 1 || followers[0].ID != ids[1] {
		t.Fatal(followers, err)
	}
	if _, err = s.ClaimAccountFollowers(t.Context(), lead.ID); err == nil {
		t.Fatal("overlapping batch accepted")
	}
	if err = s.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids[:2] {
		task, err := s.Task(t.Context(), id)
		if err != nil || task.State != "failed" {
			t.Fatal(task, err)
		}
	}
	next, err := s.ClaimLane(t.Context(), "control")
	if err != nil || next.ID != ids[2] {
		t.Fatal("batch crossed ordering barrier", next, err)
	}
}
