package controlstore

import (
	"path/filepath"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

func TestProbeDoesNotBlockConfigurationOrSubscriptions(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Initialize(t.Context(), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	probe, err := s.QueueTask(t.Context(), "probe", "outbound-probe", domain.TaskSubmission{Actor: "admin", Source: domain.TaskUser}, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	background, err := s.ClaimLane(t.Context(), "probe")
	if err != nil || background.ID != probe {
		t.Fatal(background, err)
	}
	subscription, err := s.QueueTask(t.Context(), "subscription", "subscription", domain.TaskSubmission{Actor: "scheduler", Source: domain.TaskScheduler}, []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	refresh, err := s.ClaimLane(t.Context(), "subscription")
	if err != nil || refresh.ID != subscription {
		t.Fatal(refresh, err)
	}
	apply, err := s.Queue(t.Context(), "apply", domain.TaskSubmission{Actor: "admin", Source: domain.TaskUser}, []byte(`{}`), 1, []byte(`{"next":true}`))
	if err != nil {
		t.Fatal(err)
	}
	control, err := s.ClaimLane(t.Context(), "control")
	if err != nil || control.ID != apply {
		t.Fatal(control, err)
	}
	if _, err = s.Queue(t.Context(), "apply", domain.TaskSubmission{Actor: "admin", Source: domain.TaskUser}, []byte(`{}`), 2, []byte(`{}`)); err == nil {
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
		if _, err = s.Queue(t.Context(), "subscription", domain.TaskSubmission{Actor: "scheduler", Source: domain.TaskScheduler}, []byte(`{"refresh":true}`), 0, nil); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err = s.db.QueryRow(`SELECT count(*) FROM control_tasks`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}
