package manage

import (
	"context"
	"encoding/json"
)

func (s *PlatformService) State(ctx context.Context, secret bool) (ConfigurationState, error) {
	var result ConfigurationState
	state, err := s.Store.DesiredState(ctx)
	if err != nil {
		return result, err
	}
	config, err := configurationView(state.Config, secret)
	if err != nil {
		return result, err
	}
	return ConfigurationState{state.Revision, state.AppliedRevision, config, s.Root}, nil
}
func redactPlatform(v any) {
	switch x := v.(type) {
	case map[string]any:
		for k, value := range x {
			if secretField(k) {
				x[k] = redactSecretValue(value)
			} else {
				redactPlatform(value)
			}
		}
	case []any:
		for _, item := range x {
			redactPlatform(item)
		}
	}
}

// Preserve the typed wire shape when hiding structured secrets such as headers
// and per-country credentials. Values are erased recursively, never exposed.
func redactSecretValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			v[key] = redactSecretValue(item)
		}
		return v
	case []any:
		for i, item := range v {
			v[i] = redactSecretValue(item)
		}
		return v
	case string:
		return "[redacted]"
	default:
		return nil
	}
}

func configurationView(data []byte, secret bool) (PlatformConfig, error) {
	if !secret {
		var object any
		if err := json.Unmarshal(data, &object); err != nil {
			return PlatformConfig{}, err
		}
		redactPlatform(object)
		var err error
		data, err = json.Marshal(object)
		if err != nil {
			return PlatformConfig{}, err
		}
	}
	return decodePlatform(data)
}
