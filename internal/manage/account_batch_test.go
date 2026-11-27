package manage

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
	"github.com/sck03/sing-box-vpn/internal/domain"
)

func TestAccountBatchPublishesOnceAndPreservesIndividualResults(t *testing.T) {
	runtime := os.Getenv("MANAGED_CORE_BINARY")
	if runtime == "" {
		t.Skip("set MANAGED_CORE_BINARY for credential batch publication")
	}
	root, core, _ := appFixture(t)
	s, c, _ := platformFixture(t, root, core)
	s.Runtime = runtime
	for _, id := range []string{"alice", "bob"} {
		if err := s.Accounts.SaveAccount(t.Context(), domain.Account{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"alice", "missing", "bob"} {
		action := application.TaskCommand{Operation: "account", Account: application.AccountCommand{Operation: "bind", ID: id, Scope: "normal", InboundIDs: []string{c.App.Nodes[0].ID}}}
		data, _ := json.Marshal(action)
		if _, err := s.Store.Queue(t.Context(), "account", domain.TaskSubmission{Actor: "admin", Source: domain.TaskUser}, data, 0, nil); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.Store.ClaimLane(t.Context(), "control")
	if err != nil {
		t.Fatal(err)
	}
	rest, err := s.Store.ClaimAccountFollowers(t.Context(), first.ID)
	if err != nil || len(rest) != 2 {
		t.Fatal(rest, err)
	}
	tasks := append([]controlstore.ClaimedTask{first}, rest...)
	if err = s.runAccountBatch(t.Context(), tasks); err != nil {
		t.Fatal(err)
	}
	for i, task := range tasks {
		got, err := s.Store.Task(t.Context(), task.ID)
		if err != nil {
			t.Fatal(err)
		}
		expected := "succeeded"
		if i == 1 {
			expected = "failed"
		}
		if got.State != expected {
			t.Fatal(i, got.State, got.Error)
		}
	}
	publications, err := s.Accounts.CredentialPublications(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range publications {
		if v.Runtime == "normal" && (v.Revision != 2 || v.Published != 2) {
			t.Fatal("batch did not converge on the final credentials", v)
		}
	}
}
