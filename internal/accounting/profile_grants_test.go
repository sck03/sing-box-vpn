package accounting

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

func TestGrantAuthorizesExactOutputPurpose(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.SaveAccount(t.Context(), domain.Account{ID: "account", Name: "Test"}); err != nil {
		t.Fatal(err)
	}
	token, err := store.CreateGrant(t.Context(), Grant{ID: "share", AccountID: "account", Profiles: []string{"mihomo-provider", "uri"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.CountDownload(t.Context(), token, "mihomo-config"); !errors.Is(err, ErrDenied) {
		t.Fatal("purpose escalation", err)
	}
	if err = store.CountDownload(t.Context(), token, "mihomo-provider"); err != nil {
		t.Fatal(err)
	}
	grant, err := store.LookupGrant(t.Context(), token)
	if err != nil || grant.Downloads != 1 {
		t.Fatal("rejected downloads consumed allowance", grant, err)
	}
	if _, err = store.CreateGrant(t.Context(), Grant{ID: "old", AccountID: "account", Profiles: []string{"mihomo"}}); err == nil {
		t.Fatal("ambiguous legacy profile accepted")
	}
}
