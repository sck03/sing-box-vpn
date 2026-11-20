package manage

import (
	"github.com/sck03/sing-box-vpn/internal/filelock"
	"os"
)

var errLockBusy = filelock.ErrBusy

func lockFile(file *os.File) error { return filelock.Try(file) }
