package manage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type subscriptionService interface {
	state(context.Context) (active, enabled bool, err error)
	apply(context.Context, bool, bool) error
	restore(context.Context, bool, bool) error
}

type systemSubscriptionService struct{ root string }

func querySubscriptionUnit(ctx context.Context, query string, negative ...int) (bool, error) {
	return queryUnit(ctx, "subscription", query, negative...)
}

func (s systemSubscriptionService) state(ctx context.Context) (bool, bool, error) {
	active, err := querySubscriptionUnit(ctx, "is-active", 3, 4)
	if err != nil {
		return false, false, err
	}
	enabled, err := querySubscriptionUnit(ctx, "is-enabled", 1, 3, 4)
	return active, enabled, err
}

func (s systemSubscriptionService) apply(ctx context.Context, enabled, restart bool) error {
	if err := withStartupLock(ctx, s.root, func() error { return (firewall{}).sync(ctx, s.root, false) }); err != nil {
		return err
	}
	if _, err := commandOutput(ctx, "systemctl", "daemon-reload"); err != nil {
		return err
	}
	if !enabled {
		_, err := commandOutput(ctx, "systemctl", "disable", "--now", "subscription")
		return err
	}
	if _, err := commandOutput(ctx, "systemctl", "enable", "subscription"); err != nil {
		return err
	}
	if restart {
		return (systemServices{}).restart(ctx, "subscription")
	}
	_, err := commandOutput(ctx, "systemctl", "start", "subscription")
	return err
}

func (s systemSubscriptionService) restore(ctx context.Context, active, enabled bool) error {
	if err := withStartupLock(ctx, s.root, func() error { return (firewall{}).sync(ctx, s.root, false) }); err != nil {
		return err
	}
	enable := "disable"
	if enabled {
		enable = "enable"
	}
	if _, err := commandOutput(ctx, "systemctl", enable, "subscription"); err != nil {
		return err
	}
	if active {
		return (systemServices{}).restart(ctx, "subscription")
	}
	_, err := commandOutput(ctx, "systemctl", "stop", "subscription")
	return err
}

func applySubscriptionChange(ctx context.Context, root string, change func() error, service subscriptionService) error {
	active, enabled, err := service.state(ctx)
	if err != nil {
		return err
	}
	before := fileBundle{}
	var previous, next subscriptionSettings
	err = withStartupLock(ctx, root, func() error {
		for _, name := range []string{"subscription.json", subscriptionsConfig} {
			data, err := readSubscriptionFile(filepath.Join(root, name))
			if errors.Is(err, os.ErrNotExist) {
				before[name] = nil
			} else if err != nil {
				return err
			} else {
				before[name] = data
			}
		}
		if _, err := ReadJSON(filepath.Join(root, "subscription.json"), &previous); err != nil {
			return err
		}
		if err := change(); err != nil {
			return errors.Join(err, publish(root, before))
		}
		_, err := ReadJSON(filepath.Join(root, "subscription.json"), &next)
		if err != nil {
			return errors.Join(err, publish(root, before))
		}
		return err
	})
	if err != nil {
		return err
	}
	if err = service.apply(ctx, next.Enabled, active && (previous.Port != next.Port || previous.Listen != next.Listen)); err == nil {
		return nil
	}
	rollback, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if restoreErr := withStartupLock(rollback, root, func() error { return publish(root, before) }); restoreErr != nil {
		return errors.Join(err, fmt.Errorf("订阅配置恢复失败: %w", restoreErr))
	}
	if restoreErr := service.restore(rollback, active, enabled); restoreErr != nil {
		return errors.Join(err, fmt.Errorf("原订阅配置已恢复，服务恢复失败: %w", restoreErr))
	}
	return fmt.Errorf("订阅应用失败，已恢复原配置、防火墙和服务状态: %w", err)
}
