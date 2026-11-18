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
	origin := strings.TrimRight(fallback, "/")
	if c.Listener.Enabled {
		if c.Listener.PublicBaseURL == "" && loopbackHost(c.Listener.Listen) {
			return "", errors.New("独立下载监听只绑定本机，请填写公网下载 URL 或关闭独立监听并配置「公网访问」")
		}
		origin = strings.TrimRight(subscriptionBase(c.Listener, c.App.Address), "/")
	}
	if err := shareableURL(origin); err != nil {
		return "", err
	}
	return origin, nil
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
