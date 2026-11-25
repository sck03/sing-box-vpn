package manage

import (
	"context"
	"encoding/json"

	"github.com/sck03/sing-box-vpn/internal/hostservice"
)

type tunnelObservation struct {
	Settings TunnelSettings     `json:"settings"`
	Runtime  hostservice.Tunnel `json:"runtime"`
}

// Consumers depend only on the immutable configuration and observation reads.
type configurationReader interface {
	AppliedConfig(context.Context) (json.RawMessage, error)
	Observation(context.Context, string) (string, error)
}

// Runtime observations are versioned independently of desired configuration.
// A candidate can reuse them only when it targets the same tunnel settings.
func observedConfig(ctx context.Context, store configurationReader, c PlatformConfig) (PlatformConfig, error) {
	c.normalize()
	if c.Argo.Mode != "temporary" {
		return c, nil
	}
	c.tunnelDomain = ""
	if store == nil {
		return c, nil
	}
	raw, err := store.Observation(ctx, "argo-tunnel")
	if err != nil {
		return c, err
	}
	var observation tunnelObservation
	if raw != "" {
		if err = json.Unmarshal([]byte(raw), &observation); err != nil {
			return c, err
		}
	}
	domain := ""
	if observation.Settings == c.Argo {
		domain = observation.Runtime.Domain
	}
	c.tunnelDomain = domain
	return c, nil
}
