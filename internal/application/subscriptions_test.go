package application

import (
	"context"
	"errors"
	"io"
	"testing"
)

type subscriptionRecorder struct {
	operation, id, probe string
	flag                 bool
	err                  error
}

func (r *subscriptionRecorder) Refresh(_ context.Context, force bool, _ io.Writer) error {
	r.operation, r.flag = "refresh", force
	return r.err
}
func (r *subscriptionRecorder) Check(_ context.Context, id, probe string, _ io.Writer) error {
	r.operation, r.id, r.probe = "check", id, probe
	return r.err
}
func (r *subscriptionRecorder) Deliver(_ context.Context, id string, automatic bool, _ io.Writer) error {
	r.operation, r.id, r.flag = "deliver", id, automatic
	return r.err
}

func TestSubscriptionExecutionPreservesScopeAndSchedulingPolicy(t *testing.T) {
	for _, test := range []struct {
		command         SubscriptionCommand
		automatic, flag bool
	}{
		{SubscriptionCommand{Operation: "refresh"}, false, true},
		{SubscriptionCommand{Operation: "refresh"}, true, false},
		{SubscriptionCommand{Operation: "deliver", ID: "device"}, false, false},
		{SubscriptionCommand{Operation: "deliver"}, true, true},
		{SubscriptionCommand{Operation: "check", ID: "output", ProbeURL: "https://example.test"}, false, false},
	} {
		t.Run(test.command.Operation, func(t *testing.T) {
			want := errors.New("operation result")
			r := &subscriptionRecorder{err: want}
			if err := ExecuteSubscription(t.Context(), r, test.command, test.automatic, io.Discard); !errors.Is(err, want) {
				t.Fatal(err)
			}
			if r.operation != test.command.Operation || r.id != test.command.ID || r.probe != test.command.ProbeURL || r.flag != test.flag {
				t.Fatal(r)
			}
		})
	}
}

func TestSubscriptionRejectsIgnoredFieldsBeforeExecuting(t *testing.T) {
	for _, c := range []SubscriptionCommand{
		{Operation: "refresh", ID: "source"},
		{Operation: "refresh", ProbeURL: "https://example.test"},
		{Operation: "check"},
		{Operation: "deliver", ProbeURL: "https://example.test"},
		{Operation: "unknown"},
	} {
		r := &subscriptionRecorder{}
		if err := ExecuteSubscription(t.Context(), r, c, false, io.Discard); err == nil || r.operation != "" {
			t.Fatal(c, r, err)
		}
		if (TaskCommand{Operation: "subscription", Subscription: c}).Validate() == nil {
			t.Fatal("task admitted invalid command", c)
		}
	}
	for _, c := range []SubscriptionCommand{{Operation: "check", ID: "output"}, {Operation: "deliver", ID: "device"}} {
		r := &subscriptionRecorder{}
		if err := ExecuteSubscription(t.Context(), r, c, true, io.Discard); err == nil || r.operation != "" {
			t.Fatal(c, r, err)
		}
	}
}
