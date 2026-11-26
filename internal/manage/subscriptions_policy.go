package manage

import (
	"context"
	"errors"
	"github.com/sck03/sing-box-vpn/internal/accounting"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
	"time"
)

var errOutputAccountUnavailable = errors.New("输出使用的账户权益不可用")

func checkOutputPolicy(ctx context.Context, policy sub.OutputPolicy, store *accounting.Store, now time.Time) error {
	if err := policy.Validate(now); err != nil {
		return err
	}
	for _, account := range policy.Accounts {
		if store == nil {
			return errors.New("账户服务暂不可用")
		}
		if _, err := store.DownloadAccount(ctx, account, now); err != nil {
			return errOutputAccountUnavailable
		}
	}
	return nil
}
