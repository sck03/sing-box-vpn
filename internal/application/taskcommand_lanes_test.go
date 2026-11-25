package application

import "testing"

func TestTaskLanePolicy(t *testing.T) {
	for _, tc := range []struct{ operation, subscription, lane string }{
		{"outbound-probe", "", ProbeLane}, {"subscription", "check", ProbeLane},
		{"subscription", "refresh", SubscriptionLane}, {"subscription", "deliver", SubscriptionLane},
		{"service", "", ControlLane}, {"account", "", ControlLane},
	} {
		command := TaskCommand{Operation: tc.operation}
		command.Subscription.Operation = tc.subscription
		if got := command.Lane(); got != tc.lane {
			t.Fatalf("%+v: got %q", tc, got)
		}
	}
}
