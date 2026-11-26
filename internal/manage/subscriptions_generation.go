package manage

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/sck03/sing-box-vpn/internal/application"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

// Read under the publication lock. Published files may belong to a candidate
// while services are activating; only the database confirms that generation.
func confirmedSubscriptionConfig(ctx context.Context, root string, config sub.Config, services ...AccountService) (*PlatformConfig, int64, error) {
	var revision int64
	found, err := ReadJSON(filepath.Join(root, "runtime-revision.json"), &revision)
	if err != nil {
		return nil, 0, err
	}
	if !found {
		return nil, 0, errors.New("runtime generation missing; rebuild the publication")
	}
	service, closeSource, err := subscriptionSource(root, services)
	if err != nil {
		return nil, 0, err
	}
	defer closeSource()
	store := service.Control
	state, err := store.AppliedState(ctx)
	if err != nil {
		return nil, 0, err
	}
	if revision != state.Revision {
		return nil, 0, application.ErrConflict
	}
	applied, err := decodePlatform(state.Config)
	if err != nil {
		return nil, 0, err
	}
	expected, _ := json.Marshal(applied.Subscriptions)
	current, _ := json.Marshal(config)
	if string(expected) != string(current) {
		return nil, 0, application.ErrConflict
	}
	applied, err = observedConfig(ctx, store, applied)
	return &applied, revision, err
}

func captureConfirmedSubscriptionSources(ctx context.Context, root string, config sub.Config, services ...AccountService) (map[string][]sub.Node, map[string]sub.AccountSourceState, int64, error) {
	service, closeSource, err := subscriptionSource(root, services)
	if err != nil {
		return nil, nil, 0, err
	}
	defer closeSource()
	applied, generation, err := confirmedSubscriptionConfig(ctx, root, config, service)
	if err != nil {
		return nil, nil, 0, err
	}
	service.Config = applied
	_, nodes, accounts, err := captureSubscriptionSources(ctx, root, config, nil, service)
	return nodes, accounts, generation, err
}
