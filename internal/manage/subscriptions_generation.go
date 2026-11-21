package manage

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/controlstore"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

// Read under the publication lock. Published files may belong to a candidate
// while services are activating; only the database confirms that generation.
func confirmedSubscriptionConfig(ctx context.Context, root string, config sub.Config) (*PlatformConfig, int64, error) {
	var revision int64
	found, err := ReadJSON(filepath.Join(root, "runtime-revision.json"), &revision)
	if err != nil {
		return nil, 0, err
	}
	if !found {
		// Uninitialized compiler fixtures have no platform generation. An installed
		// platform must rebuild a missing marker before any background publication.
		_, err := os.Stat(filepath.Join(root, ".platform-v5"))
		if err == nil {
			return nil, 0, errors.New("runtime generation missing; rebuild the publication")
		}
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
		return nil, 0, err
	}
	store, err := controlstore.Open(filepath.Join(root, "control.db"))
	if err != nil {
		return nil, 0, err
	}
	defer store.Close()
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

func captureConfirmedSubscriptionSources(ctx context.Context, root string, config sub.Config) (map[string][]sub.Node, map[string]sub.AccountSourceState, int64, error) {
	applied, generation, err := confirmedSubscriptionConfig(ctx, root, config)
	if err != nil {
		return nil, nil, 0, err
	}
	var services []AccountService
	if applied != nil && slices.ContainsFunc(config.Sources, func(source sub.Source) bool { return source.Kind == "account" }) {
		service, close, err := openAccountSource(root)
		if err != nil {
			return nil, nil, 0, err
		}
		defer close()
		service.Config = applied
		services = append(services, service)
	}
	_, nodes, accounts, err := captureSubscriptionSources(ctx, root, config, nil, services...)
	return nodes, accounts, generation, err
}
