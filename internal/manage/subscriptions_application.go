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
		var result application.RefreshResult
		if err := refreshSubscriptionsResult(ctx, s.Root, s.Core, true, &result); err != nil {
			return err
		}
		return jsonEncoder(output).Encode(result)
	case "preview":
		result, err := s.Preview(ctx, command.Config)
		if err != nil {
			return err
		}
		return jsonEncoder(output).Encode(result)
	default:
		return errors.New("unsupported subscription operation")
	}
}
