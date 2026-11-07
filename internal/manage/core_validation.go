package manage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var coreConfigFiles = [...]string{"sb.json", "vpngate/server.json", "sbox.json", "vpngate/sbox.json"}

// An empty digest records an absent file. Snapshot every path before invoking
// the core so newly created configurations cannot bypass compatibility checks.
func configFingerprints(ctx context.Context, root string) ([len(coreConfigFiles)]string, error) {
	var digests [len(coreConfigFiles)]string
	for i, name := range coreConfigFiles {
		if err := ctx.Err(); err != nil {
			return digests, err
		}
		digest, err := fingerprintFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return digests, err
		}
		digests[i] = digest
	}
	return digests, nil
}

func checkConfigs(ctx context.Context, root, candidate string) error {
	return validateConfigs(ctx, root, candidate, commandOutput)
}

func validateConfigs(ctx context.Context, root, candidate string, check func(context.Context, string, ...string) ([]byte, error)) error {
	before, err := configFingerprints(ctx, root)
	if err != nil {
		return err
	}
	for i, name := range coreConfigFiles {
		if before[i] == "" {
			continue
		}
		if _, err := check(ctx, candidate, "check", "-c", filepath.Join(root, filepath.FromSlash(name))); err != nil {
			return fmt.Errorf("新核心不兼容 %s，保留当前核心: %w", name, err)
		}
	}
	after, err := configFingerprints(ctx, root)
	if err != nil {
		return err
	}
	if before != after {
		return errors.New("校验期间配置发生变化，请重试更新")
	}
	return nil
}
