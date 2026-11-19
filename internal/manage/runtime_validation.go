package manage

import (
	"context"
	"errors"
	"fmt"
	"github.com/sck03/sing-box-vpn/internal/command"
	"github.com/sck03/sing-box-vpn/internal/database"
	"os"
	"path/filepath"
)

func (s *PlatformService) checkRuntime(ctx context.Context, files fileBundle) error {
	if s.Runtime == "" {
		return errors.New("managed runtime binary is required")
	}
	for _, scope := range []string{"normal", "vpngate"} {
		name := "sb.json"
		if scope == "vpngate" {
			name = "vpngate/server.json"
		}
		data := files[name]
		if len(data) == 0 {
			continue
		}
		f, err := os.CreateTemp(s.Root, ".runtime-check-*.json")
		if err != nil {
			return err
		}
		path := f.Name()
		if err = f.Chmod(0600); err == nil {
			_, err = f.Write(data)
		}
		err = errors.Join(err, f.Close())
		var output []byte
		if err == nil {
			output, err = command.Output(ctx, nil, s.Runtime, "--check", "--config", path, "--database", filepath.Join(s.Root, database.Filename), "--instance", scope)
		}
		_ = os.Remove(path)
		if err != nil {
			return fmt.Errorf("managed %s configuration rejected: %w%s", scope, err, validationDiagnostic(output, data))
		}
	}
	return nil
}
