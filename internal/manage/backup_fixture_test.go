package manage

import (
	"bytes"
	"context"
)

// Byte-backed helpers are confined to fixtures; production backup/restore streams.
func (s *PlatformService) Backup(ctx context.Context, password []byte) ([]byte, error) {
	var out bytes.Buffer
	err := s.BackupTo(ctx, &out, password)
	return out.Bytes(), err
}

// RestoreDisaster accepts an empty root only. All proxy users are disabled and
// all download/session tokens are revoked until the operator reconciles usage.
func RestoreDisaster(ctx context.Context, root string, data, password []byte, admin, newPassword string) error {
	return restoreDisasterFrom(ctx, root, bytes.NewReader(data), password, admin, newPassword)
}
