//go:build !windows

package manage

import (
	"fmt"
	"os"
	"syscall"
)

var errLockBusy error = syscall.EWOULDBLOCK

func lockFile(file *os.File) error {
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("另一个配置或核心更新正在进行: %w", err)
	}
	return nil
}
