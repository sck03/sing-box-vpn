package manage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

var coreConfigFiles = [...]string{"sb.json", "vpngate/server.json", "sbox.json", "vpngate/sbox.json"}

// Include the published inventory, its configuration and external dependencies.
// An empty digest records absence so creation during checking is a conflict.
func coreValidationInventory(ctx context.Context, root string) (map[string]string, map[string][]byte, error) {
	digests, configs := map[string]string{}, map[string][]byte{}
	add := func(path string) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data, err := readLimitedRegularFile(path, maxJSONFile)
		if errors.Is(err, os.ErrNotExist) {
			digests[path] = ""
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		digests[path] = sub.Digest(data)
		return data, nil
	}
	for _, name := range coreConfigFiles {
		path := filepath.Join(root, filepath.FromSlash(name))
		info, err := os.Lstat(path)
		if err == nil && !info.Mode().IsRegular() {
			return nil, nil, errors.New("core configuration must be a regular file")
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, nil, err
		}
		data, err := add(path)
		if err != nil {
			return nil, nil, err
		}
		if data != nil {
			configs[name] = data
		}
	}
	configData, err := add(filepath.Join(root, subscriptionsConfig))
	if err != nil {
		return nil, nil, err
	}
	snapshotData, err := add(filepath.Join(root, subscriptionsSnapshot))
	if err != nil {
		return nil, nil, err
	}
	if len(snapshotData) > 0 && len(configData) == 0 {
		return nil, nil, errors.New("published subscriptions have no configuration inventory")
	}
	if len(configData) != 0 {
		var c sub.Config
		if err := strictSubscriptionJSON(configData, &c); err != nil {
			return nil, nil, err
		}
		if err := c.Validate(); err != nil {
			return nil, nil, err
		}
		var snapshot sub.Snapshot
		if len(snapshotData) != 0 {
			if err := json.Unmarshal(snapshotData, &snapshot); err != nil {
				return nil, nil, err
			}
		}
		for _, output := range c.Outputs {
			target := slices.IndexFunc(c.Templates, func(t sub.Template) bool { return t.ID == output.Template })
			if target < 0 || c.Templates[target].Client != "sing-box" || c.Templates[target].Mode != "config" {
				continue
			}
			if artifact, ok := snapshot.Artifacts[output.ID]; ok {
				if artifact.ETag != `"`+sub.Digest(artifact.Data)+`"` {
					return nil, nil, errors.New("invalid published artifact digest")
				}
				configs["subscription:"+output.ID] = artifact.Data
			}
		}
	}
	var dependencies func(any) error
	dependencies = func(value any) error {
		switch value := value.(type) {
		case map[string]any:
			for key, item := range value {
				if key == "certificate_path" || key == "key_path" || key == "path" && value["type"] == "local" {
					if path, ok := item.(string); ok && path != "" {
						if !filepath.IsAbs(path) {
							path = filepath.Join(root, path)
						}
						if _, err := add(path); err != nil {
							return err
						}
					}
				}
				if err := dependencies(item); err != nil {
					return err
				}
			}
		case []any:
			for _, item := range value {
				if err := dependencies(item); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, data := range configs {
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			return nil, nil, err
		}
		if err := dependencies(value); err != nil {
			return nil, nil, err
		}
	}
	return digests, configs, nil
}

func checkConfigs(ctx context.Context, root, candidate string) error {
	return validateConfigs(ctx, root, candidate, commandOutput)
}

func validateConfigs(ctx context.Context, root, candidate string, check func(context.Context, string, ...string) ([]byte, error)) error {
	before, configs, err := coreValidationInventory(ctx, root)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp(root, ".core-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for i, name := range slices.Sorted(maps.Keys(configs)) {
		if err := ctx.Err(); err != nil {
			return err
		}
		path := filepath.Join(stage, fmt.Sprintf("%d.json", i))
		if err := AtomicBytes(path, configs[name], 0600); err != nil {
			return err
		}
		if _, err := check(ctx, candidate, "check", "-D", root, "-c", path); err != nil {
			return fmt.Errorf("新核心不兼容 %s，保留当前核心: %w", name, err)
		}
	}
	after, _, err := coreValidationInventory(ctx, root)
	if err != nil {
		return err
	}
	if !maps.Equal(before, after) {
		return errors.New("校验期间配置发生变化，请重试更新")
	}
	return nil
}
