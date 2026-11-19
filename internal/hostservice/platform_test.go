package hostservice

import (
	"strings"
	"testing"
)

func TestPlatformUnitsUseCurrentRecoveryPolicy(t *testing.T) {
	units, err := PlatformUnits("/srv/s-box", "/srv/s-box/bin/managed-core", "/srv/s-box/sing-box")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"sb-core", "sb-vpngate-core", "sb-vpngate-gateway", "sb-subscription", "sb-panel"} {
		data := units[name+".service"]
		for _, required := range []string{"Restart=always", "StartLimitBurst=10", "UMask=0077", "WorkingDirectory=/srv/s-box"} {
			if !strings.Contains(data, required) {
				t.Fatalf("%s missing %s", name, required)
			}
		}
	}
	if !strings.Contains(units["sb-vpngate-core.service"], "Requires=sb-vpngate-gateway.service") || !strings.Contains(units["sb-core.service"], "firewall --root \"/srv/s-box\" --wait") {
		t.Fatal("core recovery dependencies missing")
	}
	if !strings.Contains(units["sb-recovery.timer"], "Unit=sb-recovery.service") || !strings.Contains(units["sb-recovery.service"], "recover-services") {
		t.Fatal("recovery controller missing")
	}
	if _, ok := units["sb-argo.service"]; ok {
		t.Fatal("unit generation must preserve the separately managed Argo configuration")
	}
	if _, err := PlatformUnits("/srv/bad\npath", "/bin/core", "/bin/check"); err == nil {
		t.Fatal("unsafe systemd path accepted")
	}
}
