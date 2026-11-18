package manage

import (
	"context"

	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/backuparchive"
)

func refreshSubscriptions(ctx context.Context, root, core string, force bool) error {
	return refreshSubscriptionsResult(ctx, root, core, force, &application.RefreshResult{})
}

func decodePlatformBackup(data, password []byte) (platformBackup, error) {
	return backuparchive.Read(data, password)
}
