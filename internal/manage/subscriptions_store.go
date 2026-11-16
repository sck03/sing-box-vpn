package manage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

const subscriptionsConfig = "subscriptions/config.json"
const subscriptionsRuntime = "subscriptions/runtime.json"
const subscriptionsLocal = "subscriptions/local.json"
const subscriptionsSnapshot = "subscriptions/published.json"

func loadSubscriptions(root string) (sub.Config, sub.Runtime, map[string][]sub.Node, error) {
	c := sub.DefaultConfig()
	if _, err := ReadJSON(filepath.Join(root, subscriptionsConfig), &c); err != nil {
		return c, sub.Runtime{}, nil, err
	}
	if err := c.Validate(); err != nil {
		return c, sub.Runtime{}, nil, err
	}
	r := sub.Runtime{Sources: map[string]sub.SourceCache{}}
	if _, err := ReadJSON(filepath.Join(root, subscriptionsRuntime), &r); err != nil {
		return c, r, nil, err
	}
	local := map[string][]sub.Node{}
	_, err := ReadJSON(filepath.Join(root, subscriptionsLocal), &local)
	return c, r, local, err
}

func buildSubscriptions(ctx context.Context, root, core string, c sub.Config, r sub.Runtime, local map[string][]sub.Node) (fileBundle, sub.Snapshot, map[string]sub.Preview, error) {
	snapshot, previews, err := sub.Build(c, r, local, time.Now())
	if err != nil {
		return nil, snapshot, previews, err
	}
	snapshot.CoreIdentity, err = coreIdentity(core)
	if err != nil {
		return nil, snapshot, previews, err
	}
	var previous sub.Snapshot
	if _, err := ReadJSON(filepath.Join(root, subscriptionsSnapshot), &previous); err != nil {
		return nil, snapshot, previews, err
	}
	templates := map[string]sub.Template{}
	for _, t := range c.Templates {
		templates[t.ID] = t
	}
	for _, output := range c.Outputs {
		artifact, exists := snapshot.Artifacts[output.ID]
		if !exists {
			continue
		}
		if old, exists := previous.Artifacts[output.ID]; exists && previous.CoreIdentity == snapshot.CoreIdentity && old.ETag == artifact.ETag {
			continue
		}
		if t := templates[output.Template]; t.Client == "sing-box" && t.Mode == "config" {
			var config Object
			if err := json.Unmarshal(artifact.Data, &config); err != nil {
				return nil, snapshot, previews, err
			}
			if err := CheckJSON(ctx, core, root, config); err != nil {
				return nil, snapshot, previews, fmt.Errorf("subscription %s: %w", output.ID, err)
			}
		}
	}
	files := fileBundle{}
	for name, value := range map[string]any{subscriptionsConfig: c, subscriptionsRuntime: r, subscriptionsLocal: local, subscriptionsSnapshot: snapshot} {
		if err := files.json(name, value); err != nil {
			return nil, snapshot, previews, err
		}
		if len(files[name]) > maxJSONFile {
			return nil, snapshot, previews, errors.New("subscription state exceeds 16 MiB")
		}
	}
	if err := appendDefaultExports(files, snapshot, previews); err != nil {
		return nil, snapshot, previews, err
	}
	return files, snapshot, previews, nil
}

func validateSubscriptionsRevision(current, candidate sub.Config) error {
	if current.Revision != candidate.Revision {
		return errors.New("订阅配置已更改，请重新预览后提交")
	}
	return nil
}
