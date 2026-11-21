package accounting

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/domain"
)

func TestAccountUsageMatchesListWithoutActivatingFirstUse(t *testing.T) {
	s, _ := fixture(t, 1024)
	ctx, now := t.Context(), time.Now().UTC().Truncate(time.Second)
	start := now.Add(-48 * time.Hour)
	quota := int64(1024)
	if err := s.SaveSubscriber(ctx, Subscriber{ID: "person", Name: "Person", Disabled: true}); err != nil {
		t.Fatal(err)
	}
	for _, account := range []domain.Account{
		{ID: "disabled-parent", Name: "Disabled parent", SubscriberID: "person"},
		{ID: "rollover", Name: "Rollover", QuotaBytes: &quota, StartMode: "scheduled", StartsAt: &start, Period: domain.PeriodPolicy{Kind: "days", Days: 1, Timezone: "UTC"}},
		{ID: "first-use", Name: "First use", StartMode: "first_use", DurationSeconds: 86400},
	} {
		if err := s.SaveAccount(ctx, account); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`UPDATE accounts SET charged=1024,upload=1024 WHERE id='rollover'`); err != nil {
		t.Fatal(err)
	}
	for id, state := range map[string]string{"disabled-parent": "subscriber_disabled", "rollover": "active", "first-use": "not_started"} {
		detail, err := s.AccountUsage(ctx, id, now)
		if err != nil || detail.State != state {
			t.Fatal(id, detail, err)
		}
		page, err := s.AccountPage(ctx, application.PageQuery{Search: id, Limit: 20}, now, false)
		if err != nil || len(page.Items) != 1 || !reflect.DeepEqual(detail, page.Items[0]) {
			t.Fatal(id, detail, page, err)
		}
		if id == "rollover" && (detail.Charged != 0 || detail.PeriodID <= start.Unix()) {
			t.Fatal(detail)
		}
	}
	u, err := s.Account(ctx, "first-use")
	if err != nil || u.StartedAt != nil {
		t.Fatal("query activated first use", u, err)
	}
	if _, err = s.VisibleAccount(ctx, "disabled-parent", now); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
}

func TestCreationCollisionsAreDomainErrors(t *testing.T) {
	s, _ := fixture(t, 100)
	if err := s.SaveAccount(t.Context(), domain.Account{ID: "alice", Name: "Duplicate"}); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatal(err)
	}
	person := Subscriber{ID: "person", Name: "Person"}
	if err := s.SaveSubscriber(t.Context(), person); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSubscriber(t.Context(), person); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatal(err)
	}
	grant := Grant{ID: "link", AccountID: "alice", Profiles: []string{"uri"}}
	if _, err := s.CreateGrant(t.Context(), grant); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateGrant(t.Context(), grant); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatal(err)
	}
}

func TestAuthorizationScratchDoesNotRetainRevokedDecisions(t *testing.T) {
	s, _ := fixture(t, 100)
	ctx := t.Context()
	if err := s.BindBatch(ctx, []domain.Credential{{ID: "credential", AccountID: "alice", RuntimeID: "normal", InboundID: "in", Protocol: "vless", Generation: 1, UUID: "aabbccdd-1122-4333-8444-012345678901"}}); err != nil {
		t.Fatal(err)
	}
	key := authorizationKey{"credential", 1}
	wanted := map[authorizationKey]bool{key: true}
	var scratch authorizationRead
	first, err := s.authorizationSnapshot(ctx, "normal", time.Now(), wanted, &scratch)
	if err != nil || first[key] == nil {
		t.Fatal(first, err)
	}
	retained := *first[key]
	scratch.reset()
	u, err := s.Account(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	u.Disabled = true
	if err = s.SaveAccount(ctx, u.Account); err != nil {
		t.Fatal(err)
	}
	second, err := s.authorizationSnapshot(ctx, "normal", time.Now(), wanted, &scratch)
	if err != nil || len(second) != 0 {
		t.Fatal("revoked decision reused", second, err)
	}
	if retained.ID != "alice" || retained.StartedAt == nil {
		t.Fatal("published value aliased scratch", retained)
	}
	scratch.reset()
}
