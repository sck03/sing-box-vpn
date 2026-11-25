package manage

import (
	"context"
	"sync"

	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
)

type appliedViewSource interface {
	AppliedViewSince(context.Context, int64) (controlstore.AppliedView, error)
}

// Cache only immutable configuration, never account authorization or observations.
// Readers retain their snapshot while a newer confirmed revision replaces it.
type configurationCache struct {
	mu       sync.Mutex
	revision int64
	config   *PlatformConfig
}

func (v *configurationCache) read(ctx context.Context, source appliedViewSource) (PlatformConfig, controlstore.Revisions, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	since := v.revision
	if v.config == nil {
		since = -1
	}
	state, err := source.AppliedViewSince(ctx, since)
	if err != nil {
		return PlatformConfig{}, controlstore.Revisions{}, err
	}
	if state.AppliedRevision != since {
		config, err := decodePlatform(state.Config)
		if err != nil {
			return PlatformConfig{}, state.Revisions, err
		}
		v.config, v.revision = &config, state.AppliedRevision
	}
	return *v.config, state.Revisions, nil
}

// Callers must not mutate maps or slices in this shared read model. Commands use
// their own candidate, and runtime observations are added to a value copy.
func (s *PlatformService) appliedConfiguration(ctx context.Context) (PlatformConfig, controlstore.Revisions, error) {
	return s.configuration.read(ctx, s.Store)
}

func (s *PlatformService) InboundOptions(ctx context.Context, scope string) ([]application.Option, error) {
	config, _, err := s.appliedConfiguration(ctx)
	if err != nil {
		return nil, err
	}
	options := []application.Option{}
	if scope == vpngateScope && config.VPN.ActiveCountry == "" {
		return options, nil
	}
	entries, err := configuredEntries(config, scope)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.Disabled {
			options = append(options, application.Option{ID: entry.ID, Name: entry.ID + " · " + entry.Protocol})
		}
	}
	return options, nil
}

func (s *PlatformService) SubscriptionActions(ctx context.Context) (SubscriptionActionOptions, error) {
	config, state, err := s.appliedConfiguration(ctx)
	result := SubscriptionActionOptions{Revision: state.AppliedRevision, Pending: state.Revision != state.AppliedRevision, Outputs: []application.Option{}, Deliveries: []application.Option{}}
	if err != nil {
		return result, err
	}
	for _, output := range config.Subscriptions.Outputs {
		result.Outputs = append(result.Outputs, application.Option{ID: output.ID, Name: output.Name})
	}
	for _, delivery := range config.Subscriptions.Deliveries {
		result.Deliveries = append(result.Deliveries, application.Option{ID: delivery.ID})
	}
	return result, nil
}
