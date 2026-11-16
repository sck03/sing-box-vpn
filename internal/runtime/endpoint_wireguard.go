//go:build with_wireguard

package runtime

import (
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/protocol/wireguard"
)

func registerEndpoints(registry *endpoint.Registry) { wireguard.RegisterEndpoint(registry) }
