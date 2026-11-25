package manage

import (
	"context"
	"errors"
	"github.com/sck03/sing-box-vpn/internal/accounting"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
	"slices"
	"time"
)

// Authorization precedes conditional downloads and outbound delivery. Published
// bytes can outlive an entitlement; cached files never imply current permission.
func checkOutputAccounts(ctx context.Context, c sub.Config, output string, store *accounting.Store) error {
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
		if store == nil {
			return errors.New("account repository dependency is required")
		}
		_, err := store.DownloadAccount(ctx, source.AccountID, time.Now())
		if err != nil {
			return err
		}
	}
	return nil
}
