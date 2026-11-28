//go:build with_quic && with_utls && with_wireguard && with_gvisor

package runtime

import (
	"fmt"
	"testing"

	"github.com/sagernet/sing-box/option"
	sjson "github.com/sagernet/sing/common/json"
	"github.com/sck03/sing-box-vpn/internal/runtimeconfig"
)

func TestDeclaredCapabilitiesHaveRegistrations(t *testing.T) {
	caps := runtimeconfig.Supported()
	ctx := Context(t.Context(), nil)
	for _, action := range caps.RouteActions {
		options := ""
		if action == "route-options" {
			options = `,"override_port":8443`
		}
		raw := fmt.Sprintf(`{"route":{"rules":[{"domain":"example.com","action":%q%s}]}}`, action, options)
		if _, err := sjson.UnmarshalExtendedContext[option.Options](ctx, []byte(raw)); err != nil {
			t.Errorf("advertised route action %s cannot be decoded: %v", action, err)
		}
	}
	for category, kinds := range map[string][]string{"inbounds": caps.Inbounds, "outbounds": caps.Outbounds, "endpoints": caps.Endpoints, "services": caps.Services, "dns": caps.DNS} {
		for _, kind := range kinds {
			raw := fmt.Sprintf(`{"%s":[{"type":%q}]}`, category, kind)
			if category == "dns" {
				raw = fmt.Sprintf(`{"dns":{"servers":[{"type":%q}]}}`, kind)
			}
			if _, err := sjson.UnmarshalExtendedContext[option.Options](ctx, []byte(raw)); err != nil {
				t.Errorf("advertised %s %s has no usable registration: %v", category, kind, err)
			}
		}
	}
}
