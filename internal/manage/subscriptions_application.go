package manage

import (
	"context"
	"errors"
	"io"

	"github.com/sck03/sing-box-vpn/internal/application"
)

type subscriptionOperations struct{ root, core string }

func (s subscriptionOperations) Check(ctx context.Context, id, probeURL string, output io.Writer) error {
	return checkSubscriptionOutput(ctx, s.root, s.core, id, probeURL, output)
}

func (s subscriptionOperations) Deliver(ctx context.Context, id string, automatic bool, output io.Writer) error {
	return deliverSubscriptions(ctx, s.root, id, automatic, output)
}

func (s subscriptionOperations) Refresh(ctx context.Context, force bool, output io.Writer) error {
	var result application.RefreshResult
	err := refreshSubscriptionsResult(ctx, s.root, s.core, force, &result)
	return errors.Join(err, jsonEncoder(output).Encode(result))
}
