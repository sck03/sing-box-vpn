//go:build windows

package manage

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var lockFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")

func lockFile(file *os.File) error {
	var overlap syscall.Overlapped
	result, _, err := lockFileEx.Call(file.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(&overlap)))
	if result == 0 {
		return fmt.Errorf("另一个配置或核心更新正在进行: %w", err)
	}
	return nil
}
