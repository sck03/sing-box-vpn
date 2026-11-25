package manage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/domain"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestUninitializedSubscriptionPublicationIsRejected(t *testing.T) {
	if _, _, err := confirmedSubscriptionConfig(t.Context(), t.TempDir(), sub.DefaultConfig()); err == nil {
		t.Fatal("missing generation accepted")
	}
}

// Tests select the enabled-entry snapshot explicitly, like the new panel.
func testInboundIDs(t *testing.T, service AccountService) []string {
	t.Helper()
	entries, err := service.accountEntries(t.Context(), "normal")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, entry := range entries {
		if !entry.Disabled {
			ids = append(ids, entry.ID)
		}
	}
	if len(ids) == 0 {
		t.Fatal("fixture has no enabled inbounds")
	}
	return ids
}

func TestBindingRequiresExplicitSelection(t *testing.T) {
	root, core, _ := appFixture(t)
	s, _, _ := platformFixture(t, root, core)
	if err := s.Accounts.SaveAccount(t.Context(), domain.Account{ID: "a", Name: "A"}); err != nil {
		t.Fatal(err)
	}
	command := application.AccountCommand{Operation: "bind", ID: "a", Scope: "normal"}
	if err := (application.TaskCommand{Operation: "account", Account: command}).Validate(); err == nil {
		t.Fatal("empty selection queued")
	}
	if err := s.accountService().Execute(t.Context(), command, new(bytes.Buffer)); err == nil {
		t.Fatal("empty selection means all")
	}
	credentials, err := s.Accounts.Credentials(t.Context(), "normal")
	if err != nil || len(credentials) != 0 {
		t.Fatal("empty selection mutated credentials", err)
	}
	command.InboundIDs = testInboundIDs(t, s.accountService())[:1]
	if err := s.accountService().Execute(t.Context(), command, new(bytes.Buffer)); err != nil {
		t.Fatal(err)
	}
	credentials, err = s.Accounts.Credentials(t.Context(), "normal")
	if err != nil || len(credentials) != 1 || credentials[0].InboundID != command.InboundIDs[0] {
		t.Fatal("bound outside explicit snapshot", err)
	}
}

func TestBackgroundHealthIsBoundedSafeAndRecovers(t *testing.T) {
	var health backgroundHealth
	now := time.Now()
	health.record("credential-publication", context.DeadlineExceeded, now)
	health.record("subscription-schedule", errors.New("https://user:private@example.test/?token=private"), now)
	issues := health.snapshot()
	data, _ := json.Marshal(issues)
	if bytes.Contains(data, []byte("private")) || len(issues) != 2 || issues[0].Code != "timeout" {
		t.Fatal(string(data))
	}
	health.record("credential-publication", nil, now)
	if len(health.snapshot()) != 1 {
		t.Fatal("recovery not visible")
	}
	for i := range 30 {
		health.record(string(rune('a'+i)), errors.New("error"), now)
	}
	if len(health.snapshot()) != 16 {
		t.Fatal("unbounded background health")
	}
}
