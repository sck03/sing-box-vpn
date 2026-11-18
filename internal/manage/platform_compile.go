package manage

import (
	"context"
	"encoding/json"
	"errors"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
	"path/filepath"
	"time"
)

func (s *PlatformService) build(ctx context.Context, c PlatformConfig) (fileBundle, error) {
	return s.buildConfig(ctx, c, sub.Available, nil)
}

func configurationOutcome(files fileBundle) ([]byte, error) {
	var snapshot sub.Snapshot
	var config sub.Config
	if err := json.Unmarshal(files[subscriptionsSnapshot], &snapshot); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(files[subscriptionsConfig], &config); err != nil {
		return nil, err
	}
	outcome := "passed"
	if len(snapshot.Artifacts) < len(config.Outputs) {
		outcome = "partial"
	}
	return json.Marshal(Object{"outcome": outcome, "outputs": len(snapshot.Artifacts), "expected_outputs": len(config.Outputs), "diagnostics": snapshot.Diagnostics})
}

// Candidate preparation owns fetched material until the durable apply task
// publishes it. Preview cannot overwrite live caches or applied configuration.
func (s *PlatformService) prepareBuild(ctx context.Context, c PlatformConfig) (fileBundle, map[string]sub.SourceCache, error) {
	if err := c.Validate(); err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	prepared, err := prepareSubscriptionSources(ctx, s.Root, c.Subscriptions)
	if err != nil {
		return nil, nil, err
	}
	files, err := s.buildConfig(ctx, c, sub.Candidate, prepared)
	return files, prepared, err
}

func (s *PlatformService) PreviewConfiguration(ctx context.Context, c PlatformConfig) (Object, error) {
	c.normalize()
	files, _, err := s.prepareBuild(ctx, c)
	if err != nil {
		return nil, err
	}
	var server any
	var snapshot sub.Snapshot
	if err := json.Unmarshal(files["sb.json"], &server); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(files[subscriptionsSnapshot], &snapshot); err != nil {
		return nil, err
	}
	redactPlatform(server)
	outputs := []Object{}
	for _, output := range c.Subscriptions.Outputs {
		artifact, available := snapshot.Artifacts[output.ID]
		outputs = append(outputs, Object{"id": output.ID, "name": output.Name, "available": available, "bytes": len(artifact.Data), "diagnostics": snapshot.Diagnostics[output.ID]})
	}
	return Object{"valid": true, "ordinary_server": server, "outputs": outputs, "diagnostics": snapshot.Diagnostics}, nil
}

func (s *PlatformService) buildConfig(ctx context.Context, c PlatformConfig, mode sub.BuildMode, prepared map[string]sub.SourceCache) (fileBundle, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	for _, path := range certificatePaths(c) {
		rel, err := filepath.Rel(s.Root, path)
		if err != nil || !filepath.IsLocal(rel) {
			return nil, errors.New("certificate files must be inside the managed root")
		}
	}
	c, err := observedConfig(ctx, s.Store, c)
	if err != nil {
		return nil, err
	}
	server, err := serverForApp(c.App)
	if err != nil {
		return nil, err
	}
	if err = validateEffectiveServer(server); err != nil {
		return nil, err
	}
	if err = CheckJSON(ctx, s.Core, s.Root, server); err != nil {
		return nil, err
	}
	// v4 publishes only explicitly authorized account sources and imported nodes.
	local := map[string][]sub.Node{}
	r, err := loadSubscriptionRuntime(s.Root)
	if err != nil {
		return nil, err
	}
	if r.Sources == nil {
		r.Sources = map[string]sub.SourceCache{}
	}
	for id, source := range prepared {
		r.Sources[id] = source
	}
	files, _, _, err := compileSubscriptions(ctx, s.Root, s.Core, c.Subscriptions, r, local, mode, AccountService{Root: s.Root, Store: s.Accounts, Control: s.Store, Config: &c})
	if err != nil {
		return nil, err
	}
	for name, value := range map[string]any{"app.json": c.App, "sb.json": server, "vpngate/settings.json": c.VPN, "vpngate/secrets.json": c.VPNSecrets, "vpngate/gateway.json": c.Gateway, "subscription.json": c.Listener} {
		if err = files.json(name, value); err != nil {
			return nil, err
		}
	}
	if c.VPN.ActiveCountry != "" {
		gateway, e := vpnServer(c.VPN, c.VPNSecrets)
		if e != nil {
			return nil, e
		}
		if e = CheckJSON(ctx, s.Core, filepath.Join(s.Root, "vpngate"), gateway); e != nil {
			return nil, e
		}
		if e = files.json("vpngate/server.json", gateway); e != nil {
			return nil, e
		}
	}
	return files, nil
}
