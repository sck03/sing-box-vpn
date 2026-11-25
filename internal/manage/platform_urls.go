package manage

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/publicaccess"
)

func appliedDownloadOrigin(c PlatformConfig, fallback string) (string, error) {
	origin := strings.TrimRight(fallback, "/")
	if c.Listener.Enabled {
		origin = strings.TrimRight(c.Listener.PublicBaseURL, "/")
	}
	if err := publicaccess.ShareableURL(origin); err != nil {
		return "", err
	}
	return origin, nil
}

type GrantOptions struct {
	ListenerEnabled bool                 `json:"listener_enabled"`
	Pending         bool                 `json:"pending"`
	Outputs         []application.Option `json:"outputs"`
	Unavailable     string               `json:"unavailable,omitempty"`
	origin          string
}

func (s *PlatformService) grantOptions(ctx context.Context, fallback string) (GrantOptions, error) {
	v := GrantOptions{Outputs: []application.Option{}}
	c, state, err := s.appliedConfiguration(ctx)
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

func (v GrantOptions) validate(outputs []string) error {
	if v.Unavailable != "" {
		return errors.New(v.Unavailable)
	}
	for _, id := range outputs {
		if !slices.ContainsFunc(v.Outputs, func(o application.Option) bool { return o.ID == id }) {
			return errors.New("output is not applied")
		}
	}
	return nil
}
