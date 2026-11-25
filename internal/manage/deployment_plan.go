package manage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/sck03/sing-box-vpn/internal/servicecatalog"
)

// ServiceChange is both the executor input and the public preview contract.
type ServiceChange struct {
	Name   string `json:"name"`
	Action string `json:"action"`
	Reason string `json:"reason"`
}

type DeploymentPreview struct {
	Changes         []ServiceChange `json:"changes"`
	RuntimeObserved bool            `json:"runtime_observed"`
	AppliedRevision int64           `json:"applied_revision"`
}

func serviceEnabled(c PlatformConfig, name string) bool {
	switch name {
	case servicecatalog.VPNCore, servicecatalog.Gateway:
		return c.VPN.ActiveCountry != "" && c.Gateway["enabled"] != false
	case servicecatalog.Subscription:
		return c.Listener.Enabled
	case servicecatalog.Argo:
		return c.Argo.Mode == "fixed" || c.Argo.Mode == "temporary"
	default:
		return true
	}
}

// tunnelOrigin is shared by unit generation and restart planning.
func tunnelOrigin(c PlatformConfig) (string, error) {
	for _, node := range c.App.Nodes {
		if node.ID == c.Argo.Inbound && node.Protocol == "vmess" && !node.Disabled {
			scheme := "http"
			if node.TLS.Enabled {
				scheme = "https"
			}
			return fmt.Sprintf("%s://127.0.0.1:%d", scheme, node.Port), nil
		}
	}
	return "", fmt.Errorf("tunnel inbound is missing")
}

// Inputs contain only what a running service actually consumes. Publishing
// addresses, inactive countries and gateway selection policy do not restart cores.
func deploymentInputs(c PlatformConfig) (map[string]json.RawMessage, error) {
	server, err := serverForApp(c.App)
	if err != nil {
		return nil, err
	}
	inputs := map[string]any{servicecatalog.Core: server, servicecatalog.Gateway: c.Gateway, servicecatalog.Subscription: c.Listener}
	if serviceEnabled(c, servicecatalog.VPNCore) {
		server, err := vpnServer(c.VPN, c.VPNSecrets)
		if err != nil {
			return nil, err
		}
		inputs[servicecatalog.VPNCore] = server
	}
	if serviceEnabled(c, servicecatalog.Argo) {
		tunnel := struct{ Mode, Token, Origin string }{Mode: c.Argo.Mode}
		if c.Argo.Mode == "fixed" {
			tunnel.Token = c.Argo.Token
		} else {
			tunnel.Origin, err = tunnelOrigin(c)
			if err != nil {
				return nil, err
			}
		}
		inputs[servicecatalog.Argo] = tunnel
	}
	encoded := make(map[string]json.RawMessage, len(inputs))
	for name, value := range inputs {
		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		encoded[name] = data
	}
	return encoded, nil
}

// Newly enabled components start; an operator-stopped component stays stopped.
func deploymentPlan(c, old PlatformConfig, active []string) ([]ServiceChange, error) {
	current, err := deploymentInputs(c)
	if err != nil {
		return nil, err
	}
	previous, err := deploymentInputs(old)
	if err != nil {
		return nil, err
	}
	plan := []ServiceChange{}
	for _, name := range []string{servicecatalog.VPNCore, servicecatalog.Gateway, servicecatalog.Subscription, servicecatalog.Argo} {
		if !serviceEnabled(c, name) && serviceEnabled(old, name) {
			plan = append(plan, ServiceChange{Name: name, Action: "disable-stop", Reason: "disabled"})
		}
	}
	for _, name := range []string{servicecatalog.Gateway, servicecatalog.Core, servicecatalog.VPNCore, servicecatalog.Subscription, servicecatalog.Argo} {
		if !serviceEnabled(c, name) {
			continue
		}
		if !serviceEnabled(old, name) {
			plan = append(plan, ServiceChange{Name: name, Action: "enable-start", Reason: "enabled"})
		} else if !bytes.Equal(current[name], previous[name]) && slices.Contains(active, name) {
			reason := "runtime-config"
			if name == servicecatalog.Argo {
				reason = "tunnel-connection"
			}
			if name == servicecatalog.Gateway {
				reason = "gateway-config"
			}
			if name == servicecatalog.Subscription {
				reason = "download-listener"
			}
			plan = append(plan, ServiceChange{Name: name, Action: "restart", Reason: reason})
		}
	}
	return plan, nil
}
