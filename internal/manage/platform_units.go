package manage

import (
	"context"
	"errors"
	"fmt"
	"github.com/sck03/sing-box-vpn/internal/command"
	"github.com/sck03/sing-box-vpn/internal/hostservice"
	"path/filepath"
	"strings"
)

func unitQuote(v string) (string, error) {
	if strings.ContainsAny(v, "\r\n\x00\"%$") {
		return "", errors.New("systemd path contains unsupported characters")
	}
	return `"` + v + `"`, nil
}
func (s *PlatformService) WriteUnits(ctx context.Context) error {
	journalPath := hostservice.JournalLimitsPath
	if err := hostservice.CheckJournalOwnership(journalPath); err != nil {
		return err
	}
	root, err := unitQuote(s.Root)
	if err != nil {
		return err
	}
	binary, err := unitQuote(s.Runtime)
	if err != nil {
		return err
	}
	db, err := unitQuote(filepath.Join(s.Root, "control.db"))
	if err != nil {
		return err
	}
	manager := "/usr/local/bin/yg-vpngate"
	config, err := unitQuote(filepath.Join(s.Root, "sb.json"))
	if err != nil {
		return err
	}
	vpnConfig, err := unitQuote(filepath.Join(s.Root, "vpngate", "server.json"))
	if err != nil {
		return err
	}
	gwConfig, err := unitQuote(filepath.Join(s.Root, "vpngate", "gateway.json"))
	if err != nil {
		return err
	}
	core, err := unitQuote(s.Core)
	if err != nil {
		return err
	}
	units := map[string]string{
		"sb-core":            fmt.Sprintf("ExecStart=%s --config %s --database %s --instance normal\n", binary, config, db),
		"sb-vpngate-core":    fmt.Sprintf("ExecStart=%s --config %s --database %s --instance vpngate\n", binary, vpnConfig, db),
		"sb-vpngate-gateway": fmt.Sprintf("ExecStart=%s daemon --config %s\nExecStopPost=%s cleanup --config %s\n", manager, gwConfig, manager, gwConfig),
		"sb-subscription":    fmt.Sprintf("ExecStart=%s subscription --root %s\n", manager, root),
		"sb-panel":           fmt.Sprintf("ExecStart=%s panel --root %s --core %s --runtime %s serve\n", manager, root, core, binary),
	}
	paths := []string{"/etc/systemd/system/sb-argo.service"}
	for name := range units {
		paths = append(paths, filepath.Join("/etc/systemd/system", name+".service"))
	}
	for name := range recoveryUnits(root) {
		paths = append(paths, filepath.Join("/etc/systemd/system", name))
	}
	if err := hostservice.CheckUnitOwnership(paths, hostservice.ReadUnit); err != nil {
		return err
	}
	if err := AtomicBytes(journalPath, []byte(hostservice.JournalLimits), 0644); err != nil {
		return err
	}
	for name, body := range units {
		extra := ""
		if name == "sb-vpngate-core" {
			extra = "Requires=sb-vpngate-gateway.service\nAfter=sb-vpngate-gateway.service\n"
		}
		pre := ""
		if name == "sb-core" || name == "sb-vpngate-core" {
			pre = fmt.Sprintf("ExecStartPre=%s firewall --root %s --wait\n", manager, root)
		}
		// WorkingDirectory is a single path, unlike the quoted ExecStart argv.
		data := "# Managed by sing-box-vpn v4\n[Unit]\nDescription=sing-box-vpn " + name + "\nAfter=network-online.target\nWants=network-online.target\nStartLimitIntervalSec=300\nStartLimitBurst=10\n" + extra + "[Service]\nUMask=0077\nLogNamespace=sb-vpn\nRestart=always\nRestartSec=15\nTimeoutStopSec=30\nLimitNOFILE=65536\n" + pre + body + "WorkingDirectory=" + s.Root + "\n[Install]\nWantedBy=multi-user.target\n"
		path := filepath.Join("/etc/systemd/system", name+".service")
		if err = AtomicBytes(path, []byte(data), 0644); err != nil {
			return err
		}
	}
	for name, data := range recoveryUnits(root) {
		path := filepath.Join("/etc/systemd/system", name)
		if err = AtomicBytes(path, []byte(data), 0644); err != nil {
			return err
		}
	}
	if _, err = command.Output(ctx, nil, "systemctl", "daemon-reload"); err != nil {
		return err
	}
	_, err = command.Output(ctx, nil, "systemctl", "enable", "--now", "sb-recovery.timer")
	return err
}
