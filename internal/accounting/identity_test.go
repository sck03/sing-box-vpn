package accounting

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

func TestCredentialCanonicalIdentity(t *testing.T) {
	for _, spelling := range []string{
		"AABBCCDD-1122-4333-8444-5566778899AA",
		"aabbccdd1122433384445566778899aa",
		"{aabbccdd-1122-4333-8444-5566778899aa}",
		"urn:uuid:aabbccdd-1122-4333-8444-5566778899aa",
	} {
		t.Run(spelling, func(t *testing.T) {
			s, err := Open(filepath.Join(t.TempDir(), "control.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			ctx := t.Context()
			for _, id := range []string{"a", "b"} {
				if err = s.SaveAccount(ctx, domain.Account{ID: id, Name: id}); err != nil {
					t.Fatal(err)
				}
			}
			c := domain.Credential{ID: "one", AccountID: "a", RuntimeID: "normal", InboundID: "in", Protocol: "vless", Generation: 1, UUID: spelling}
			if err = s.BindBatch(ctx, []domain.Credential{c}); err != nil {
				t.Fatal(err)
			}
			list, err := s.Credentials(ctx, "normal")
			if err != nil || len(list) != 1 || list[0].UUID != "aabbccdd-1122-4333-8444-5566778899aa" {
				t.Fatal(list, err)
			}
			c.ID, c.AccountID, c.UUID = "two", "b", list[0].UUID
			if err = s.BindBatch(ctx, []domain.Credential{c}); err == nil {
				t.Fatal("equivalent UUID accepted for another account")
			}
			if err = s.Rotate(ctx, "one", strings.ToUpper(list[0].UUID), "", 1); err == nil {
				t.Fatal("equivalent UUID accepted as rotation")
			}
		})
	}
}

func TestTUICPasswordRotationPreservesUniqueUUID(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := t.Context()
	if err = s.SaveAccount(ctx, domain.Account{ID: "a", Name: "a"}); err != nil {
		t.Fatal(err)
	}
	c := domain.Credential{ID: "one", AccountID: "a", RuntimeID: "normal", InboundID: "tuic", Protocol: "tuic", Generation: 1, UUID: "aabbccdd-1122-4333-8444-5566778899aa", Password: "old-tuic-password"}
	if err = s.BindBatch(ctx, []domain.Credential{c}); err != nil {
		t.Fatal(err)
	}
	if err = s.Rotate(ctx, c.ID, strings.ToUpper(c.UUID), c.Password, 1); err == nil {
		t.Fatal("unchanged TUIC authentication accepted")
	}
	if err = s.Rotate(ctx, c.ID, strings.ToUpper(c.UUID), "new-tuic-password", 1); err != nil {
		t.Fatal("password-only rotation rejected", err)
	}
	list, err := s.Credentials(ctx, "normal")
	if err != nil || len(list) != 1 || list[0].Generation != 2 || list[0].UUID != c.UUID || list[0].Password != "new-tuic-password" {
		t.Fatal(list, err)
	}
	if _, err = s.authorize(ctx, c.ID, 1, time.Now(), false); err == nil {
		t.Fatal("old credential generation remains authorized")
	}
}
