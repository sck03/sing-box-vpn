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

type ServiceState struct {
	Name          string `json:"name"`
	Label         string `json:"label"`
	Description   string `json:"description"`
	Page          string `json:"page"`
	Section       string `json:"section"`
	ID            string `json:"Id,omitempty"`
	ActiveState   string `json:"ActiveState,omitempty"`
	SubState      string `json:"SubState,omitempty"`
	UnitFileState string `json:"UnitFileState,omitempty"`
	LoadState     string `json:"LoadState,omitempty"`
	MainPID       string `json:"MainPID,omitempty"`
	State         string `json:"state,omitempty"`
	Error         string `json:"error,omitempty"`
}

func (s *PlatformService) ServiceStatus(ctx context.Context) ([]ServiceState, error) {
	var states map[string]Object
	var queryErr error
	if !s.NoSystem {
		states, queryErr = readUnitStates(ctx)
	}
	result := []ServiceState{}
	for _, name := range platformServices {
		definition := serviceDetails(name)
		v := ServiceState{Name: name, Label: definition.Label, Description: definition.Description, Page: definition.Page, Section: definition.Section}
		if s.NoSystem {
			v.State = "unavailable"
		} else if state, ok := states[name]; ok {
			v.ID, v.ActiveState, v.SubState = text(state["Id"]), text(state["ActiveState"]), text(state["SubState"])
			v.UnitFileState, v.LoadState, v.MainPID = text(state["UnitFileState"]), text(state["LoadState"]), text(state["MainPID"])
		} else {
			v.Error = "service query failed"
			if queryErr == nil {
				v.Error = "service missing from systemctl response"
			}
		}
		result = append(result, v)
	}
	return result, nil
}
