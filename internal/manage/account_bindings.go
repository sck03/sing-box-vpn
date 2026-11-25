package manage

import (
	"context"
	"sync"

	"github.com/sck03/sing-box-vpn/internal/accounting"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
)

type bindingAvailability map[accounting.Binding]bool

// Only configuration-derived availability is cached. Account permissions,
// expiry, quotas and credential bindings are read live for every request.
type accountBindingCache struct {
	mu        sync.Mutex
	revision  int64
	available bindingAvailability
}

type appliedChanges interface {
	AppliedStateSince(context.Context, int64) (controlstore.AppliedState, error)
}

func (s AccountService) availableBindings(ctx context.Context) (bindingAvailability, error) {
	source, incremental := s.Control.(appliedChanges)
	if s.Config != nil || s.bindings == nil || !incremental {
		config, err := s.configSnapshot(ctx)
		if err != nil {
			return nil, err
		}
		return configuredBindings(config)
	}
	cache := s.bindings
	cache.mu.Lock()
	defer cache.mu.Unlock()
	revision := cache.revision
	if cache.available == nil {
		revision = -1
	}
	state, err := source.AppliedStateSince(ctx, revision)
	if err != nil {
		return nil, err
	}
	if state.Revision == revision {
		return cache.available, nil
	}
	config, err := decodePlatform(state.Config)
	if err != nil {
		return nil, err
	}
	available, err := configuredBindings(config)
	if err != nil {
		return nil, err
	}
	cache.revision, cache.available = state.Revision, available
	return available, nil
}

func configuredBindings(config PlatformConfig) (bindingAvailability, error) {
	available := bindingAvailability{}
	for _, scope := range []string{normalScope, vpngateScope} {
		if scope == vpngateScope && config.VPN.ActiveCountry == "" {
			continue
		}
		entries, err := configuredEntries(config, scope)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.Disabled {
				available[accounting.Binding{Runtime: scope, Inbound: entry.ID, Protocol: entry.Protocol}] = true
			}
		}
	}

	return available, nil
}
