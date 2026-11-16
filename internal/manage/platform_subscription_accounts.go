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
func checkOutputAccounts(ctx context.Context, root string, c sub.Config, output string, stores ...*accounting.Store) error {
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
	var store *accounting.Store
	owned := true
	if len(stores) > 0 {
		store = stores[0]
		owned = false
	}
	defer func() {
		if owned && store != nil {
			_ = store.Close()
		}
	}()
	for _, source := range c.Sources {
		if source.Kind != "account" || !slices.Contains(refs, source.ID) {
			continue
		}
		if store == nil {
			var err error
			store, err = accounting.Open(filepath.Join(root, "control.db"))
			if err != nil {
				return err
			}
		}
		_, err := store.DownloadAccount(ctx, source.AccountID, time.Now())
		if err != nil {
			return err
		}
	}
	return nil
}
