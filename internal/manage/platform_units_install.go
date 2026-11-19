package manage

// Install units rendered by the independent hostservice package.

import (
	"context"
	"github.com/sck03/sing-box-vpn/internal/command"
	"github.com/sck03/sing-box-vpn/internal/hostservice"
	"maps"
	"path/filepath"
	"slices"
)

func unitQuote(value string) (string, error) { return hostservice.UnitQuote(value) }

func (s *PlatformService) WriteUnits(ctx context.Context) error {
	journalPath := hostservice.JournalLimitsPath
	if err := hostservice.CheckJournalOwnership(journalPath); err != nil {
		return err
	}
	units, err := hostservice.PlatformUnits(s.Root, s.Runtime, s.Core)
	if err != nil {
		return err
	}
	names := slices.Sorted(maps.Keys(units))
	paths := []string{"/etc/systemd/system/sb-argo.service"}
	for _, name := range names {
		paths = append(paths, filepath.Join("/etc/systemd/system", name))
	}
	if err = hostservice.CheckUnitOwnership(paths, hostservice.ReadUnit); err != nil {
		return err
	}
	if err = AtomicBytes(journalPath, []byte(hostservice.JournalLimits), 0644); err != nil {
		return err
	}
	for _, name := range names {
		if err = AtomicBytes(filepath.Join("/etc/systemd/system", name), []byte(units[name]), 0644); err != nil {
			return err
		}
	}
	if _, err = command.Output(ctx, nil, "systemctl", "daemon-reload"); err != nil {
		return err
	}
	_, err = command.Output(ctx, nil, "systemctl", "enable", "--now", "sb-recovery.timer")
	return err
}
