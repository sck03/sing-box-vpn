package manage

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/domain"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func TestExportIgnoresUnconfirmedAndBrokenProjections(t *testing.T) {
	root, core, _ := appFixture(t)
	s, confirmed, _ := platformFixture(t, root, core)
	if err := os.WriteFile(filepath.Join(root, ".platform-v5"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	service := s.accountService()
	if err := s.Accounts.SaveAccount(t.Context(), domain.Account{ID: "a", Name: "Alice"}); err != nil {
		t.Fatal(err)
	}
	if err := service.Execute(t.Context(), application.AccountCommand{Operation: "bind", ID: "a", Scope: "normal"}, new(bytes.Buffer)); err != nil {
		t.Fatal(err)
	}
	candidate, err := decodePlatform(mustJSON(t, confirmed))
	if err != nil {
		t.Fatal(err)
	}
	candidate.App.Address = "192.0.2.77"
	for i := range candidate.App.Nodes {
		candidate.App.Nodes[i].Port++
	}
	if err = AtomicJSON(filepath.Join(root, "app.json"), candidate.App); err != nil {
		t.Fatal(err)
	}
	for _, corrupt := range []bool{false, true} {
		if corrupt {
			if err = os.WriteFile(filepath.Join(root, "app.json"), []byte("broken"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		nodes, err := service.accountNodes(t.Context(), "a", "normal", 0)
		if err != nil || len(nodes) != 5 {
			t.Fatal(len(nodes), err)
		}
		for _, node := range nodes {
			if node.Server != confirmed.App.Address || node.Port != confirmed.App.fixtureProfile().Ports[node.Protocol] {
				t.Fatal("mixed configuration versions", node)
			}
		}
	}
}

func TestAccountSubscriptionRecoversWithoutRemoteSources(t *testing.T) {
	for _, cause := range []string{"disabled", "quota", "subscriber", "period"} {
		t.Run(cause, func(t *testing.T) {
			root, core, _ := appFixture(t)
			s, c, _ := platformFixture(t, root, core)
			if err := os.WriteFile(filepath.Join(root, ".platform-v5"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			quota := int64(100)
			a := domain.Account{ID: "a", Name: "Alice", QuotaBytes: &quota}
			if cause == "period" {
				a.Period = domain.PeriodPolicy{Kind: "days", Days: 1, Timezone: "UTC"}
			}
			if cause == "subscriber" {
				if err := s.Accounts.SaveSubscriber(ctx, accounting.Subscriber{ID: "person", Name: "Person", Disabled: true}); err != nil {
					t.Fatal(err)
				}
				a.SubscriberID = "person"
			}
			a.Disabled = cause == "disabled"
			if err := s.Accounts.SaveAccount(ctx, a); err != nil {
				t.Fatal(err)
			}
			if err := s.accountService().Execute(ctx, application.AccountCommand{Operation: "bind", ID: "a", Scope: "normal"}, new(bytes.Buffer)); err != nil {
				t.Fatal(err)
			}
			if cause == "quota" || cause == "period" {
				if _, err := s.Accounts.Reserve(ctx, "lease", "a", "normal", quota, time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			c.Subscriptions = sub.Config{Version: sub.SchemaVersion, Revision: 1,
				Sources:     []sub.Source{{ID: "account", Name: "Account", Kind: "account", AccountID: "a", Scope: "normal", Required: true}},
				Collections: []sub.Collection{{ID: "all", Name: "All", Sources: []string{"account"}}},
				Templates:   []sub.Template{{ID: "uri", Version: 1, Client: "uri", Mode: "nodes"}},
				Outputs:     []sub.Output{{ID: "out", Name: "Output", Collection: "all", Template: "uri"}}}
			files, err := s.build(ctx, c)
			if err != nil {
				t.Fatal(err)
			}
			if err = publish(root, files); err != nil {
				t.Fatal(err)
			}
			before, err := subscriptionReadSnapshot(ctx, root)
			if err != nil || len(before.Artifacts) != 0 {
				t.Fatal("inactive output published", before, err)
			}
			u, err := s.Accounts.Account(ctx, "a")
			if err != nil {
				t.Fatal(err)
			}
			switch cause {
			case "disabled":
				u.Disabled = false
				err = s.Accounts.SaveAccount(ctx, u.Account)
			case "quota":
				larger := int64(1000)
				u.QuotaBytes = &larger
				err = s.Accounts.SaveAccount(ctx, u.Account)
			case "subscriber":
				err = s.Accounts.SaveSubscriber(ctx, accounting.Subscriber{ID: "person", Name: "Person", Revision: 1})
			case "period":
				_, err = s.Accounts.Advance(ctx, "a", time.Now().Add(25*time.Hour), false)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = s.scheduleSubscriptions(ctx); err != nil {
				t.Fatal(err)
			}
			task, err := s.Store.ClaimLane(ctx, "control")
			if err != nil {
				t.Fatal("account recovery was not queued", err)
			}
			if err = s.runTask(ctx, task); err != nil {
				t.Fatal(err)
			}
			finished, err := s.Store.Task(ctx, task.ID)
			if err != nil || finished.State != "succeeded" {
				t.Fatal(finished, err)
			}
			after, err := subscriptionReadSnapshot(ctx, root)
			if err != nil || len(after.Artifacts) != 1 {
				t.Fatal("account output did not recover", after, err)
			}
			if err = s.scheduleSubscriptions(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err = s.Store.ClaimLane(ctx, "control"); !errors.Is(err, sql.ErrNoRows) {
				t.Fatal("unchanged state queued redundant work", err)
			}
		})
	}
}
