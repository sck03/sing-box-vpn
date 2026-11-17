package accounting

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/domain"
)

func TestAccountPageOrderingAndEffectiveSubscriberState(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := t.Context()
	if err = s.SaveSubscriber(ctx, Subscriber{ID: "person", Name: "Person", Disabled: true}); err != nil {
		t.Fatal(err)
	}
	for i := range 45 {
		if err = s.SaveAccount(ctx, domain.Account{ID: fmt.Sprintf("a%02d", i), Name: fmt.Sprintf("Account %02d", 44-i), SubscriberID: "person"}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.AccountPage(ctx, application.PageQuery{Search: "person", Offset: 20, Limit: 20, Sort: "name"}, time.Now())
	if err != nil || page.Total != 45 || len(page.Items) != 20 || page.Items[0].ID != "a24" || page.Items[0].State != "subscriber_disabled" {
		t.Fatal(page, err)
	}
	if err = s.SaveSubscriber(ctx, Subscriber{ID: "person", Name: "Person", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	page, err = s.AccountPage(ctx, application.PageQuery{Search: "a44", Limit: 20}, time.Now())
	if err != nil || page.Total != 1 || page.Items[0].State != "active" {
		t.Fatal(page, err)
	}
	for _, q := range []application.PageQuery{{Limit: 101}, {Limit: 20, Offset: -1}, {Limit: 20, Sort: "id; DROP TABLE accounts"}} {
		if _, err = s.AccountPage(ctx, q, time.Now()); err == nil {
			t.Fatal("invalid page accepted", q)
		}
	}
}
