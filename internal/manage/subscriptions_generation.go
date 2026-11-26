package manage

import (
	"bytes"
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
	service, closeSource, err := subscriptionSource(root, services)
	if err != nil {
		return nil, 0, err
	}
	defer closeSource()
	applied, revision, err := confirmedSubscriptionState(ctx, root, service)
	if err != nil {
		return nil, 0, err
	}
	if err := checkSubscriptionConfig(applied.Subscriptions, config); err != nil {
		return nil, 0, err
	}
	applied, err = observedConfig(ctx, service.Control, applied)
	return &applied, revision, err
}

// Status uses the same confirmed generation without reading secret-bearing
// subscription caches or artifacts. The caller holds the publication lock.
func confirmedSubscriptionState(ctx context.Context, root string, service AccountService) (PlatformConfig, int64, error) {
	var revision int64
	found, err := ReadJSON(filepath.Join(root, "runtime-revision.json"), &revision)
	if err != nil {
		return PlatformConfig{}, 0, err
	}
	if !found {
		return PlatformConfig{}, 0, errors.New("runtime generation missing; rebuild the publication")
	}
	applied, confirmed, err := service.appliedSnapshot(ctx)
	if err != nil {
		return PlatformConfig{}, 0, err
	}
	if revision != confirmed {
		return PlatformConfig{}, 0, application.ErrConflict
	}
	return applied, revision, nil
}

func checkSubscriptionConfig(applied, published sub.Config) error {
	expected, err := json.Marshal(applied)
	if err != nil {
		return err
	}
	current, err := json.Marshal(published)
	if err != nil {
		return err
	}
	if !bytes.Equal(expected, current) {
		return application.ErrConflict
	}
	return nil
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
