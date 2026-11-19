package hostservice

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

func UnitQuote(value string) (string, error) {
	if strings.ContainsAny(value, "\r\n\x00\"%$") {
		return "", errors.New("systemd path contains unsupported characters")
	}
	return `"` + value + `"`, nil
}

// PlatformUnits renders complete units without filesystem or process side effects.
func PlatformUnits(root, runtime, core string) (map[string]string, error) {
	paths := []string{root, runtime, core, filepath.Join(root, "control.db"), filepath.Join(root, "sb.json"), filepath.Join(root, "vpngate", "server.json"), filepath.Join(root, "vpngate", "gateway.json")}
	quoted := make([]string, len(paths))
	for i, path := range paths {
		var err error
		quoted[i], err = UnitQuote(path)
		if err != nil {
			return nil, err
		}
	}
	manager := "/usr/local/bin/yg-vpngate"
	commands := map[string]string{
		"sb-core":            fmt.Sprintf("ExecStart=%s --config %s --database %s --instance normal\n", quoted[1], quoted[4], quoted[3]),
		"sb-vpngate-core":    fmt.Sprintf("ExecStart=%s --config %s --database %s --instance vpngate\n", quoted[1], quoted[5], quoted[3]),
		"sb-vpngate-gateway": fmt.Sprintf("ExecStart=%s daemon --config %s\nExecStopPost=%s cleanup --config %s\n", manager, quoted[6], manager, quoted[6]),
		"sb-subscription":    fmt.Sprintf("ExecStart=%s subscription --root %s\n", manager, quoted[0]),
		"sb-panel":           fmt.Sprintf("ExecStart=%s panel --root %s --core %s --runtime %s serve\n", manager, quoted[0], quoted[2], quoted[1]),
	}
	units := map[string]string{}
	for name, command := range commands {
		extra, pre := "", ""
		if name == "sb-vpngate-core" {
			extra = "Requires=sb-vpngate-gateway.service\nAfter=sb-vpngate-gateway.service\n"
		}
		if name == "sb-core" || name == "sb-vpngate-core" {
			pre = fmt.Sprintf("ExecStartPre=%s firewall --root %s --wait\n", manager, quoted[0])
		}
		units[name+".service"] = "# Managed by sing-box-vpn v4\n[Unit]\nDescription=sing-box-vpn " + name + "\nAfter=network-online.target\nWants=network-online.target\nStartLimitIntervalSec=300\nStartLimitBurst=10\n" + extra + "[Service]\nUMask=0077\nLogNamespace=sb-vpn\nRestart=always\nRestartSec=15\nTimeoutStopSec=30\nLimitNOFILE=65536\n" + pre + command + "WorkingDirectory=" + root + "\n[Install]\nWantedBy=multi-user.target\n"
	}
	units["sb-recovery.service"] = fmt.Sprintf("# Managed by sing-box-vpn v4\n[Unit]\nDescription=sing-box-vpn failed service recovery\n[Service]\nType=oneshot\nUMask=0077\nTimeoutStartSec=50\nExecStart=%s panel --root %s recover-services\n", manager, quoted[0])
	units["sb-recovery.timer"] = "# Managed by sing-box-vpn v4\n[Unit]\nDescription=sing-box-vpn recovery schedule\n[Timer]\nOnBootSec=5min\nOnUnitActiveSec=5min\nRandomizedDelaySec=30\nUnit=sb-recovery.service\n[Install]\nWantedBy=timers.target\n"
	return units, nil
}
