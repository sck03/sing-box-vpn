package manage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type unitState struct{ Active, Enabled bool }
type restoreServices interface {
	states(context.Context) (map[string]unitState, error)
	stop(context.Context, map[string]unitState) error
	apply(context.Context, map[string]unitState, map[string]unitState) error
}
type systemRestoreServices struct{ root string }

var restoreUnits = []string{"sing-box", "vpngate", "vpngate-core", "subscription"}

func (s systemRestoreServices) states(ctx context.Context) (map[string]unitState, error) {
	states := map[string]unitState{}
	for _, unit := range restoreUnits {
		active, err := queryUnit(ctx, unit, "is-active", 3, 4)
		if err != nil {
			return nil, err
		}
		enabled, err := queryUnit(ctx, unit, "is-enabled", 1, 3, 4)
		if err != nil {
			return nil, err
		}
		states[unit] = unitState{active, enabled}
	}
	return states, nil
}
func (s systemRestoreServices) stop(ctx context.Context, states map[string]unitState) error {
	for i := len(restoreUnits) - 1; i >= 0; i-- {
		unit := restoreUnits[i]
		if states[unit].Active {
			if _, err := commandOutput(ctx, "systemctl", "stop", unit); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s systemRestoreServices) apply(ctx context.Context, states, before map[string]unitState) error {
	if err := withStartupLock(ctx, s.root, func() error { return (firewall{}).sync(ctx, s.root, false) }); err != nil {
		return err
	}
	for _, unit := range restoreUnits {
		state := states[unit]
		if state.Enabled || before[unit].Enabled {
			action := "disable"
			if state.Enabled {
				action = "enable"
			}
			if _, err := commandOutput(ctx, "systemctl", action, unit); err != nil {
				return err
			}
		}
		if state.Active {
			if err := (systemServices{}).restart(ctx, unit); err != nil {
				return err
			}
		}
	}
	return nil
}

// Caller owns the operation lock. Stop the gateway before replacing its paths so
// ExecStopPost cleans up the original namespace and workers with the old config.
func applySubscriptionRestore(ctx context.Context, root string, files fileBundle, services restoreServices) error {
	states, err := services.states(ctx)
	if err != nil {
		return err
	}
	before := fileBundle{}
	if err := withStartupLock(ctx, root, func() error {
		for name := range files {
			data, err := readLimitedRegularFile(filepath.Join(root, name), maxJSONFile)
			if errors.Is(err, os.ErrNotExist) {
				before[name] = nil
			} else if err != nil {
				return err
			} else {
				before[name] = data
			}
		}
		return nil
	}); err != nil {
		return err
	}
	rollback := func(cause error, published bool) error {
		recovery, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		var recoveryErr error
		if published {
			if stopErr := services.stop(recovery, states); stopErr != nil {
				recoveryErr = stopErr
			}
			if restoreErr := withStartupLock(recovery, root, func() error { return publish(root, before) }); restoreErr != nil {
				return errors.Join(cause, recoveryErr, restoreErr)
			}
		}
		recoveryErr = errors.Join(recoveryErr, services.apply(recovery, states, states))
		if recoveryErr != nil {
			return errors.Join(cause, fmt.Errorf("backup rollback needs attention: %w", recoveryErr))
		}
		return fmt.Errorf("restore failed; original files and services recovered: %w", cause)
	}
	if err := services.stop(ctx, states); err != nil {
		return rollback(err, false)
	}
	if err := withStartupLock(ctx, root, func() error { return publish(root, files) }); err != nil {
		return rollback(err, false)
	}
	desired := map[string]unitState{}
	for unit, state := range states {
		desired[unit] = state
	}
	desired["subscription"] = unitState{}
	var gateway struct {
		Enabled bool `json:"enabled"`
	}
	_ = json.Unmarshal(files["vpngate/gateway.json"], &gateway)
	if !gateway.Enabled || files["vpngate/server.json"] == nil {
		desired["vpngate"], desired["vpngate-core"] = unitState{}, unitState{}
	}
	if err := services.apply(ctx, desired, states); err != nil {
		return rollback(err, true)
	}
	return nil
}
