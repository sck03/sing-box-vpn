package hostservice

import (
	"bytes"
	"errors"
	"fmt"
	"os"
)

const UnitOwner = "# Managed by sing-box-vpn v4\n"

func ReadUnit(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("unit is not a regular owned file: %s", path)
	}
	return os.ReadFile(path)
}

// Preflight every path before a deployment writes its first unit.
func CheckUnitOwnership(paths []string, read func(string) ([]byte, error)) error {
	for _, path := range paths {
		data, err := read(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !bytes.HasPrefix(data, []byte(UnitOwner)) {
			return fmt.Errorf("refusing to overwrite unrelated unit %s", path)
		}
	}
	return nil
}
