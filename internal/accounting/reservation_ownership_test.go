package accounting

import (
	"testing"
	"time"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

func TestReservationConflictDoesNotChargeWrongOwner(t *testing.T) {
	for _, tc := range []struct {
		name, account, runtime string
		close                  bool
	}{
		{"account", "bob", "normal", false}, {"runtime", "alice", "vpngate", false}, {"closed", "alice", "normal", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := fixture(t, 1<<30)
			ctx := t.Context()
			now := time.Now()
			if err := s.SaveAccount(ctx, domain.Account{ID: "bob", Name: "Bob"}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Reserve(ctx, "owned", "alice", "normal", 100, now); err != nil {
				t.Fatal(err)
			}
			if tc.close {
				if err := s.Checkpoint(ctx, "owned", 100, 0, true); err != nil {
					t.Fatal(err)
				}
			}
			before, err := s.DownloadAccount(ctx, tc.account, now)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.Reserve(ctx, "owned", tc.account, tc.runtime, 100, now); err == nil {
				t.Fatal("lease ownership/closure bypassed")
			}
			after, err := s.DownloadAccount(ctx, tc.account, now)
			if err != nil || after.Charged != before.Charged {
				t.Fatal("rejected refill changed charge", before.Charged, after.Charged, err)
			}
		})
	}
}
