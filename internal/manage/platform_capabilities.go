package manage

import (
	"github.com/sck03/sing-box-vpn/internal/adminauth"
	"github.com/sck03/sing-box-vpn/internal/gatewayconfig"
	"github.com/sck03/sing-box-vpn/internal/runtimeconfig"
)

// Public, release-scoped metadata; never include installation state or secrets.
func panelCapabilities() Object {
	return Object{
		"protocols":       Protocols,
		"runtime":         runtimeconfig.Supported(),
		"countries":       Countries,
		"password_bytes":  gatewayconfig.Range{Min: adminauth.MinPasswordBytes, Max: adminauth.MaxPasswordBytes},
		"gateway":         Object{"defaults": gatewayconfig.Default(), "limits": gatewayconfig.Limits},
		"service_actions": []string{"start", "stop", "restart", "enable", "disable"},
		"services":        serviceDefinitions,
	}
}
