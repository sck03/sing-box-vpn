package manage

import (
	"context"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
	"github.com/sck03/sing-box-vpn/internal/domain"
	"sync"
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

func (s *PlatformService) InboundOptions(ctx context.Context, scope string) ([]domain.Option, error) {
	config, _, err := s.appliedConfiguration(ctx)
	if err != nil {
		return nil, err
	}
	options := []domain.Option{}
	if scope == vpngateScope && config.VPN.ActiveCountry == "" {
		return options, nil
	}
	entries, err := configuredEntries(config, scope)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.Disabled {
			options = append(options, domain.Option{ID: entry.ID, Name: entry.ID + " · " + entry.Protocol})
		}
	}
	return options, nil
}

func (s *PlatformService) SubscriptionActions(ctx context.Context) (SubscriptionActionOptions, error) {
	config, state, err := s.appliedConfiguration(ctx)
	result := SubscriptionActionOptions{Revision: state.AppliedRevision, Pending: state.Revision != state.AppliedRevision, Outputs: []domain.Option{}, Deliveries: []domain.Option{}}
	if err != nil {
		return result, err
	}
	for _, output := range config.Subscriptions.Outputs {
		result.Outputs = append(result.Outputs, domain.Option{ID: output.ID, Name: output.Name})
	}
	for _, delivery := range config.Subscriptions.Deliveries {
		result.Deliveries = append(result.Deliveries, domain.Option{ID: delivery.ID})
	}
	return result, nil
}
