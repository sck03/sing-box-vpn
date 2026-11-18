package manage

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/sck03/sing-box-vpn/internal/application"
)

func appliedDownloadOrigin(c PlatformConfig, fallback string) (string, error) {
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

type grantOptions struct {
	ListenerEnabled bool                 `json:"listener_enabled"`
	Pending         bool                 `json:"pending"`
	Outputs         []application.Option `json:"outputs"`
	Unavailable     string               `json:"unavailable,omitempty"`
	origin          string
}

func (s *PlatformService) grantOptions(ctx context.Context, fallback string) (grantOptions, error) {
	v := grantOptions{Outputs: []application.Option{}}
	state, err := s.Store.State(ctx)
	if err != nil {
		return v, err
	}
	c, err := decodePlatform(state.Applied)
	if err != nil {
		return v, err
	}
	v.Pending = state.Revision != state.AppliedRevision
	v.ListenerEnabled = c.Listener.Enabled
	v.origin, err = appliedDownloadOrigin(c, fallback)
	if err != nil {
		v.Unavailable = err.Error()
	}
	for _, o := range c.Subscriptions.Outputs {
		v.Outputs = append(v.Outputs, application.Option{ID: o.ID, Name: o.Name})
	}
	return v, nil
}

func (v grantOptions) validate(outputs []string) error {
	if v.Unavailable != "" {
		return errors.New(v.Unavailable)
	}
	if len(outputs) > 0 && !v.ListenerEnabled {
		return errors.New("enable and apply the download listener first")
	}
	for _, id := range outputs {
		if !slices.ContainsFunc(v.Outputs, func(o application.Option) bool { return o.ID == id }) {
			return errors.New("output is not applied")
		}
	}
	return nil
}
