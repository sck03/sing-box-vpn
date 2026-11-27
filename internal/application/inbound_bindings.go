package application

import (
	"context"
	"fmt"

	"github.com/sck03/sing-box-vpn/internal/domain"
)

// BoundInbound is membership only: configuration validation never needs secrets.
type BoundInbound struct {
	ID       string
	Protocol string
}

type InboundBindingRepository interface {
	BoundInbounds(context.Context, string) ([]BoundInbound, error)
}

// ValidateInboundBindings keeps existing credentials attached to a stable
// identity. Disabled listeners retain membership; removal requires unbinding.
func ValidateInboundBindings(ctx context.Context, repository InboundBindingRepository, scope string, nodes []BoundInbound) error {
	bindings, err := repository.BoundInbounds(ctx, scope)
	if err != nil {
		return err
	}
	configured := make(map[string]string, len(nodes))
	for _, node := range nodes {
		configured[node.ID] = node.Protocol
	}
	for _, binding := range bindings {
		if configured[binding.ID] != binding.Protocol {
			return &domain.FieldError{Field: "app.nodes", Message: fmt.Sprintf("入站 %s 仍有关联凭据，不能删除、改名或更换协议；请先解除账户绑定，或保留 ID 并停用入站", binding.ID)}
		}
	}
	return nil
}
