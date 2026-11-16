package controlstore

import (
	"path/filepath"
	"testing"
)

func TestBackgroundTaskDoesNotBlockConfiguration(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Initialize(t.Context(), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	probe, err := s.QueueBackground(t.Context(), "outbound-probe", "admin", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	background, err := s.ClaimLane(t.Context(), "background")
	if err != nil || background.ID != probe {
		t.Fatal(background, err)
	}
	apply, err := s.Queue(t.Context(), "apply", "admin", []byte(`{}`), 1, []byte(`{"next":true}`))
	if err != nil {
		t.Fatal(err)
	}
	control, err := s.ClaimLane(t.Context(), "control")
	if err != nil || control.ID != apply {
		t.Fatal(control, err)
	}
	if _, err = s.Queue(t.Context(), "apply", "admin", []byte(`{}`), 2, []byte(`{}`)); err == nil {
		t.Fatal("overlapping configuration accepted")
	}
}

func TestAutomaticTasksAreDeduplicated(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Initialize(t.Context(), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err = s.Queue(t.Context(), "subscription", "scheduler", []byte(`{"refresh":true}`), 0, nil); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err = s.db.QueryRow(`SELECT count(*) FROM control_tasks`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}
