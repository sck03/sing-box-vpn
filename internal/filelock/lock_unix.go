//go:build !windows

package filelock

import (
	"fmt"
	"os"
	"syscall"
)

var ErrBusy error = syscall.EWOULDBLOCK

func Try(file *os.File) error {
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("另一个配置或核心更新正在进行: %w", err)
	}
	return nil
}
