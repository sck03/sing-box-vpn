package manage

import "testing"

func TestPortOwnershipUsesScopeRatherThanProfileCountry(t *testing.T) {
	for _, kind := range Protocols {
		app := App{Profile: Profile{Country: "JP", Ports: map[string]int{kind: 20001}},
			Inbounds: []Object{{"type": kind, "listen_port": 20001}}}
		settings := VPNSettings{ActiveCountry: "JP", Profiles: map[string]Profile{
			"JP": {Ports: map[string]int{kind: 22001}},
		}}
		for _, scope := range []string{normalScope, vpngateScope} {
			port := "22001"
			if scope == vpngateScope {
				port = "20001"
			}
			if err := validatePortMutation("", app, settings, scope, "port", []string{kind, port}); err == nil {
				t.Fatalf("%s %s reused the other scope's main port", scope, kind)
			}
		}
	}
}
