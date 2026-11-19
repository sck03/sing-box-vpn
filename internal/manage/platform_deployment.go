package manage

import (
	"context"
	"errors"
	"fmt"
	"github.com/sck03/sing-box-vpn/internal/command"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"time"
)

type serviceChange struct{ Name, Action string }

func serviceSnapshot(ctx context.Context, withEnabled bool) ([]string, []string, error) {
	states, err := readUnitStates(ctx)
	if err != nil {
		return nil, nil, err
	}
	return deploymentStates(states, withEnabled)
}

func deploymentStates(states map[string]Object, withEnabled bool) ([]string, []string, error) {
	var active, enabled []string
	for _, name := range platformServices {
		unit := states[name]
		if name == "sb-argo" && unit["LoadState"] == "not-found" && unit["ActiveState"] == "inactive" {
			continue
		}
		if unit["LoadState"] != "loaded" {
			return nil, nil, fmt.Errorf("%s is missing or unavailable", name)
		}
		switch unit["ActiveState"] {
		case "active":
			active = append(active, name)
		case "inactive", "failed":
		default:
			return nil, nil, fmt.Errorf("%s is transitioning; retry after it settles", name)
		}
		if !withEnabled {
			continue
		}
		switch unit["UnitFileState"] {
		case "enabled":
			enabled = append(enabled, name)
		case "disabled", "", "static":
		default:
			return nil, nil, fmt.Errorf("%s has an unmanaged enable state", name)
		}
	}
	return active, enabled, nil
}
func (s *PlatformService) restoreEnabled(ctx context.Context, enabled []string) error {
	var result error
	for _, name := range platformServices {
		action := "disable"
		if slices.Contains(enabled, name) {
			action = "enable"
		}
		// A disabled tunnel may have no unit yet.
		if _, err := os.Stat(filepath.Join("/etc/systemd/system", name+".service")); errors.Is(err, os.ErrNotExist) {
			continue
		}
		result = errors.Join(result, s.service(ctx, name, action))
	}
	return result
}

// Only a newly enabled component is started automatically. Editing a component
// that an operator stopped preserves that stopped state.
func deploymentPlan(c, old PlatformConfig, active []string) []serviceChange {
	currentServer, _ := serverForApp(c.App)
	oldServer, _ := serverForApp(old.App)
	vpnChanged := !reflect.DeepEqual(c.VPN, old.VPN) || !reflect.DeepEqual(c.VPNSecrets, old.VPNSecrets)
	changed := map[string]bool{
		"sb-core":            !reflect.DeepEqual(currentServer, oldServer),
		"sb-vpngate-gateway": !reflect.DeepEqual(c.Gateway, old.Gateway),
		"sb-vpngate-core":    vpnChanged || !reflect.DeepEqual(c.Gateway, old.Gateway),
		"sb-subscription":    !reflect.DeepEqual(c.Listener, old.Listener),
		"sb-argo":            c.Argo != old.Argo || !reflect.DeepEqual(currentServer, oldServer),
	}
	enabled := func(v PlatformConfig, name string) bool {
		switch name {
		case "sb-vpngate-gateway", "sb-vpngate-core":
			return v.VPN.ActiveCountry != "" && v.Gateway["enabled"] != false
		case "sb-subscription":
			return v.Listener.Enabled
		case "sb-argo":
			return v.Argo.Mode == "fixed" || v.Argo.Mode == "temporary"
		default:
			return true
		}
	}
	plan := []serviceChange{}
	for _, name := range []string{"sb-vpngate-core", "sb-vpngate-gateway", "sb-subscription", "sb-argo"} {
		if !enabled(c, name) && enabled(old, name) {
			plan = append(plan, serviceChange{name, "disable-stop"})
		}
	}
	for _, name := range []string{"sb-vpngate-gateway", "sb-core", "sb-vpngate-core", "sb-subscription", "sb-argo"} {
		if !enabled(c, name) {
			continue
		}
		if !enabled(old, name) {
			plan = append(plan, serviceChange{name, "enable-start"})
		} else if changed[name] && slices.Contains(active, name) {
			plan = append(plan, serviceChange{name, "restart"})
		}
	}
	return plan
}

func (s *PlatformService) applyHost(ctx context.Context, c, old PlatformConfig, active []string, rollback bool) error {
	if runtime.GOOS != "linux" {
		return errors.New("deployment requires Linux systemd")
	}
	if err := s.WriteUnits(ctx); err != nil {
		return err
	}
	if err := s.writeTunnel(ctx, c); err != nil {
		return err
	}
	if err := withStartupLock(ctx, s.Root, func() error { return (firewall{}).sync(ctx, s.Root, false) }); err != nil {
		return err
	}
	plan := deploymentPlan(c, old, active)
	if rollback {
		plan = nil
		for _, name := range []string{"sb-vpngate-core", "sb-vpngate-gateway", "sb-subscription", "sb-argo", "sb-core"} {
			if !slices.Contains(active, name) {
				load, err := command.Output(ctx, nil, "systemctl", "show", name, "--property=LoadState", "--value")
				if err != nil {
					return err
				}
				if strings.TrimSpace(string(load)) == "not-found" {
					continue
				}
				plan = append(plan, serviceChange{name, "stop"})
			}
		}
		for _, name := range []string{"sb-vpngate-gateway", "sb-core", "sb-vpngate-core", "sb-subscription", "sb-argo"} {
			if slices.Contains(active, name) {
				plan = append(plan, serviceChange{name, "restart"})
			}
		}
	}
	for _, item := range plan {
		action := item.Action
		if action == "enable-start" || action == "disable-stop" {
			setting, operation := "enable", "start"
			if action == "disable-stop" {
				setting, operation = "disable", "stop"
			}
			if err := s.service(ctx, item.Name, setting); err != nil {
				return err
			}
			action = operation
		}
		if err := s.service(ctx, item.Name, action); err != nil {
			return err
		}
	}
	return nil
}

func (s *PlatformService) awaitRuntime(ctx context.Context, instance string, started int64) error {
	child, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		states, err := s.Store.Runtimes(child)
		if err != nil {
			return err
		}
		for _, state := range states {
			if state["instance"] == instance && state["started"].(int64) >= started && state["recent"] == true {
				return nil
			}
		}
		select {
		case <-child.Done():
			return fmt.Errorf("%s did not confirm startup: %w", instance, child.Err())
		case <-ticker.C:
		}
	}
}
