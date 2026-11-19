package manage

import (
	"context"
	"errors"
	"github.com/sck03/sing-box-vpn/internal/command"
	"slices"
	"strings"
	"time"
)

func (s *PlatformService) activeServices(ctx context.Context) ([]string, error) {
	active, _, err := serviceSnapshot(ctx, false)
	return active, err
}
func (s *PlatformService) service(ctx context.Context, name, action string) error {
	if !slices.Contains(platformServices, name) || !slices.Contains([]string{"start", "stop", "restart", "try-restart", "enable", "disable"}, action) {
		return errors.New("unsupported service action")
	}
	started := time.Now().UnixNano()
	if action == "start" {
		if _, e := command.Output(ctx, nil, "systemctl", "is-active", "--quiet", name); e == nil {
			started = 0
		}
	}
	_, err := command.Output(ctx, nil, "systemctl", action, name)
	if err == nil && (action == "start" || action == "restart") {
		_, err = command.Output(ctx, nil, "systemctl", "is-active", "--quiet", name)
		if instance := serviceDetails(name).Runtime; err == nil && instance != "" {
			err = s.awaitRuntime(ctx, instance, started)
		}
	}
	return err
}
func readUnitStates(ctx context.Context) (map[string]Object, error) {
	states := map[string]Object{}
	args := append([]string{"show", "--all", "--property=Id,ActiveState,SubState,UnitFileState,LoadState,MainPID"}, platformServices...)
	data, queryErr := command.Output(ctx, nil, "systemctl", args...)
	for _, block := range strings.Split(strings.TrimSpace(string(data)), "\n\n") {
		v := Object{}
		for _, line := range strings.Split(block, "\n") {
			if k, value, ok := strings.Cut(line, "="); ok {
				v[k] = value
			}
		}
		states[strings.TrimSuffix(text(v["Id"]), ".service")] = v
	}
	return states, queryErr
}

func (s *PlatformService) ServiceStatus(ctx context.Context) ([]Object, error) {
	var states map[string]Object
	var queryErr error
	if !s.NoSystem {
		states, queryErr = readUnitStates(ctx)
	}
	result := []Object{}
	for _, name := range platformServices {
		definition := serviceDetails(name)
		v := Object{"name": name, "label": definition.Label, "description": definition.Description, "page": definition.Page, "section": definition.Section}
		if s.NoSystem {
			v["state"] = "unavailable"
		} else if state, ok := states[name]; ok {
			for key, value := range state {
				v[key] = value
			}
		} else {
			v["error"] = "service query failed"
			if queryErr == nil {
				v["error"] = "service missing from systemctl response"
			}
		}
		result = append(result, v)
	}
	return result, nil
}
