package manage

import (
	"context"
	"github.com/sck03/sing-box-vpn/internal/accounting"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
	"path/filepath"
	"slices"
	"time"
)

// Authorization precedes conditional downloads and outbound delivery. Published
// bytes can outlive an entitlement; cached files never imply current permission.
func checkOutputAccounts(ctx context.Context, root string, c sub.Config, output string) error {
	collection := ""
	for _, o := range c.Outputs {
		if o.ID == output {
			collection = o.Collection
		}
	}
	var refs []string
	for _, v := range c.Collections {
		if v.ID == collection {
			refs = v.Sources
		}
	}
	for _, source := range c.Sources {
		if source.Kind != "account" || !slices.Contains(refs, source.ID) {
			continue
		}
		store, err := accounting.Open(filepath.Join(root, "control.db"))
		if err != nil {
			return err
		}
		u, err := store.Advance(ctx, source.AccountID, time.Now(), false)
		store.Close()
		if err != nil {
			return err
		}
		if u.State == "disabled" || u.State == "expired" || u.State == "exhausted" {
			return accounting.ErrDenied
		}
	}
	return nil
}
