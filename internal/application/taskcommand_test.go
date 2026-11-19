package application

import (
	"encoding/json"
	"testing"
)

func TestTaskCommandRejectsIrrelevantFields(t *testing.T) {
	for _, command := range []TaskCommand{
		{Operation: "service", Service: "sb-core", Value: "stop", ID: "ignored"},
		{Operation: "runtime-rollback", Config: json.RawMessage(`{}`)},
		{Operation: "subscription", Subscription: SubscriptionCommand{Operation: "deliver", ProbeURL: "https://example.com"}},
		{Operation: "gateway", Config: json.RawMessage(`{"op":"swap","ignored":true}`)},
		{Operation: "account", Account: AccountCommand{Operation: "export"}},
	} {
		if command.Validate() == nil {
			t.Fatalf("accepted %#v", command)
		}
	}
}
