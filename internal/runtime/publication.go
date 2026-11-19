package runtime

import (
	"context"

	"github.com/sck03/sing-box-vpn/internal/accounting"
)

// RefreshCredentials acknowledges only a successfully loaded, published snapshot.
// A concurrent mutation is left for the next poll instead of loading unpublished
// credentials. The caller retains loaded on any error and applies bounded retry.
func RefreshCredentials(ctx context.Context, store *accounting.Store, core *Core, scope string, loaded int64) (int64, error) {
	publication, err := store.CredentialPublication(ctx, scope)
	if err != nil || publication.Revision <= loaded || publication.Published < publication.Revision {
		return loaded, err
	}
	credentials, revision, err := store.CredentialSnapshot(ctx, scope)
	if err != nil || revision > publication.Published {
		return loaded, err
	}
	if err = core.ReloadCredentials(credentials); err != nil {
		return loaded, err
	}
	if err = store.CredentialsLoaded(ctx, scope, revision); err != nil {
		return loaded, err
	}
	return revision, nil
}
