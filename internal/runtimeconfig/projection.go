package runtimeconfig

import "encoding/json"

// CoreProjection removes managed policy metadata for external sing-box option
// checks. The actual managed-runtime check still binds account users and flow.
func CoreProjection(raw []byte) (json.RawMessage, error) {
	var config map[string]json.RawMessage
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, err
	}
	if config["inbounds"] == nil {
		return raw, nil
	}
	var inbounds []map[string]json.RawMessage
	if err := json.Unmarshal(config["inbounds"], &inbounds); err != nil {
		return nil, err
	}
	for _, inbound := range inbounds {
		delete(inbound, "_managed_flow")
	}
	data, err := json.Marshal(inbounds)
	if err != nil {
		return nil, err
	}
	config["inbounds"] = data
	return json.Marshal(config)
}
