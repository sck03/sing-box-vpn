package manage

import (
	"context"
	"errors"
	"io"

	"github.com/sck03/sing-box-vpn/internal/application"
)

type subscriptionOperations struct{ source AccountService }

func (s subscriptionOperations) Check(ctx context.Context, id, probeURL string, output io.Writer) error {
	return checkSubscriptionOutput(ctx, s.source.Root, s.source.Core, id, probeURL, output)
}

func (s subscriptionOperations) Deliver(ctx context.Context, id string, automatic bool, output io.Writer) error {
	return deliverSubscriptions(ctx, s.source.Root, id, automatic, output, s.source)
}

func (s subscriptionOperations) Refresh(ctx context.Context, force bool, output io.Writer) error {
	var result application.RefreshResult
	err := refreshSubscriptionsResult(ctx, s.source.Root, s.source.Core, force, &result, s.source)
	return errors.Join(err, jsonEncoder(output).Encode(result))
}
