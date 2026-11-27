package manage

import (
	"testing"

	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/domain"
)

func TestQueuedTasksNotifyTheirOwnLane(t *testing.T) {
	s, err := OpenPlatform(t.TempDir(), "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	commands := []application.TaskCommand{
		{Operation: "service", Service: "sb-core", Value: "restart"},
		{Operation: "subscription", Subscription: application.SubscriptionCommand{Operation: "refresh"}},
		{Operation: "outbound-probe", ID: "example"},
	}
	for _, command := range commands {
		id, err := s.queueTask(t.Context(), taskInput{TaskCommand: command}, "test")
		if err != nil {
			t.Fatal(err)
		}
		// Simulate unrelated idle workers checking for notifications first.
		for _, lane := range []string{domain.ProbeLane, domain.SubscriptionLane, domain.ControlLane} {
			if lane != command.Lane() {
				select {
				case <-s.wake[lane]:
					t.Fatalf("%s task woke %s", command.Operation, lane)
				default:
				}
			}
		}
		select {
		case <-s.wake[command.Lane()]:
		default:
			t.Fatalf("%s task requires the polling fallback", command.Operation)
		}
		task, err := s.Store.ClaimLane(t.Context(), command.Lane())
		if err != nil || task.ID != id {
			t.Fatal(task.ID, id, err)
		}
		if err = s.Store.Finish(t.Context(), id, nil); err != nil {
			t.Fatal(err)
		}
	}
}
