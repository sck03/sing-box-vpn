package manage

import (
	"context"
	"errors"
	"io"

	"github.com/sck03/sing-box-vpn/internal/application"
)

type SubscriptionService struct{ Root, Core string }

func (s SubscriptionService) Execute(ctx context.Context, command application.SubscriptionCommand, output io.Writer) error {
	switch command.Operation {
	case "check":
		return checkSubscriptionOutput(ctx, s.Root, s.Core, command.ID, command.ProbeURL, output)
	case "deliver":
		return deliverSubscriptions(ctx, s.Root, command.ID, false, output)
	case "refresh":
		return s.refresh(ctx, true, output)
	default:
		return errors.New("unsupported subscription operation")
	}
}

func (s SubscriptionService) refresh(ctx context.Context, force bool, output io.Writer) error {
	var result application.RefreshResult
	err := refreshSubscriptionsResult(ctx, s.Root, s.Core, force, &result)
	return errors.Join(err, jsonEncoder(output).Encode(result))
}
