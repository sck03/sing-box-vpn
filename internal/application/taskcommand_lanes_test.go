package application

import (
	"github.com/sck03/sing-box-vpn/internal/domain"
	"testing"
)

func TestTaskLanePolicy(t *testing.T) {
	for _, tc := range []struct{ operation, subscription, lane string }{
		{"outbound-probe", "", domain.ProbeLane}, {"subscription", "check", domain.ProbeLane},
		{"subscription", "refresh", domain.SubscriptionLane}, {"subscription", "deliver", domain.SubscriptionLane},
		{"service", "", domain.ControlLane}, {"account", "", domain.ControlLane},
	} {
		command := TaskCommand{Operation: tc.operation}
		command.Subscription.Operation = tc.subscription
		if got := command.Lane(); got != tc.lane {
			t.Fatalf("%+v: got %q", tc, got)
		}
	}
}
