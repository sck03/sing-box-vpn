//go:build windows

package manage

// Production runs on Linux; Windows cannot fsync directory handles this way.
func syncDirectory(string) error { return nil }
