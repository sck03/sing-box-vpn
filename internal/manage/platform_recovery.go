package manage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/sck03/sing-box-vpn/internal/command"
	"github.com/sck03/sing-box-vpn/internal/hostservice"
)

func recoverServices(ctx context.Context, root string) error {
	lock, err := os.OpenFile(filepath.Join(root, ".service-recovery.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = lockFile(lock); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	return hostservice.Recover(ctx, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return command.Output(ctx, nil, name, args...)
	}, hostservice.ReadUnit, append(append([]string{}, platformServices...), "sb-panel"))
}

// Shared with the installer/upgrade shell. A retry controller must never race
// an intentional service transition or its rollback.
func withServiceLock(ctx context.Context, root string, operation func() error) error {
	lock, err := os.OpenFile(filepath.Join(root, ".service-recovery.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := lockFile(lock)
		if err == nil {
			return operation()
		}
		if !errors.Is(err, errLockBusy) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
