package manage

import (
	"context"
	"errors"
	"slices"
	"strings"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

func (s *PlatformService) downloadOrigin(ctx context.Context, fallback string) (string, error) {
	state, err := s.Store.State(ctx)
	if err != nil {
		return "", err
	}
	c, err := decodePlatform(state.Applied)
	if err != nil {
		return "", err
	}
	if c.Listener.Enabled {
		return strings.TrimRight(subscriptionBase(c.Listener, c.App.Address), "/"), nil
	}
	return strings.TrimRight(fallback, "/"), nil
}

func (s *PlatformService) validateOutputGrant(ctx context.Context, outputs []string) error {
	state, err := s.Store.State(ctx)
	if err != nil {
		return err
	}
	c, err := decodePlatform(state.Applied)
	if err != nil {
		return err
	}
	if !c.Listener.Enabled {
		return errors.New("enable and apply the download listener first")
	}
	for _, id := range outputs {
		if !slices.ContainsFunc(c.Subscriptions.Outputs, func(o sub.Output) bool { return o.ID == id }) {
			return errors.New("output is not applied")
		}
	}
	return nil
}
