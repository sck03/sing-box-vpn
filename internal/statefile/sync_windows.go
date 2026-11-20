//go:build windows

package statefile

// Production runs on Linux; Windows cannot fsync directory handles this way.
func SyncDirectory(string) error { return nil }
