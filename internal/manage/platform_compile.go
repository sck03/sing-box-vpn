package manage

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sck03/sing-box-vpn/internal/runtimeconfig"
	"github.com/sck03/sing-box-vpn/internal/servicecatalog"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
	"path/filepath"
	"time"
)

func (s *PlatformService) build(ctx context.Context, c PlatformConfig) (fileBundle, error) {
	return s.buildConfig(ctx, c, sub.Available, nil)
}

func (s *PlatformService) buildApplied(ctx context.Context, c PlatformConfig) (fileBundle, error) {
	c.normalize()
	if err := c.validate(false); err != nil {
		return nil, err
	}
	return s.compileConfig(ctx, c, sub.Available, nil)
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
	c.normalize()
	if err := c.Validate(); err != nil {
		return nil, nil, err
	}
	if err := s.validateInboundBindings(ctx, c); err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	prepared, err := prepareSubscriptionSources(ctx, s.Root, c.Subscriptions)
	if err != nil {
		return nil, nil, err
	}
	files, err := s.buildConfig(ctx, c, sub.Candidate, prepared)
	if err == nil {
		err = s.checkRuntime(ctx, files)
	}
	return files, prepared, err
}

func (s *PlatformService) PreviewConfiguration(ctx context.Context, c PlatformConfig) (ConfigurationPreview, error) {
	files, _, err := s.prepareBuild(ctx, c)
	if err != nil {
		return ConfigurationPreview{}, err
	}
	var server Object
	var snapshot sub.Snapshot
	if err := json.Unmarshal(files["sb.json"], &server); err != nil {
		return ConfigurationPreview{}, err
	}
	if err := json.Unmarshal(files[subscriptionsSnapshot], &snapshot); err != nil {
		return ConfigurationPreview{}, err
	}
	redactPlatform(server)
	outputs := []ConfigurationOutputPreview{}
	var previous sub.Snapshot
	_, _ = ReadJSON(filepath.Join(s.Root, subscriptionsSnapshot), &previous)
	for _, output := range c.Subscriptions.Outputs {
		artifact, available := snapshot.Artifacts[output.ID]
		outputs = append(outputs, ConfigurationOutputPreview{ID: output.ID, Name: output.Name, Available: available, Bytes: len(artifact.Data), Diagnostics: snapshot.Diagnostics[output.ID], InputNodes: snapshot.InputCounts[output.ID], Nodes: snapshot.NodeCounts[output.ID], Delta: sub.CompareSelection(previous.NodeKeys[output.ID], snapshot.NodeKeys[output.ID]), Validation: snapshot.Validations[output.ID]})
	}
	state, err := s.Store.State(ctx)
	if err != nil {
		return ConfigurationPreview{}, err
	}
	old, err := decodePlatform(state.Applied)
	if err != nil {
		return ConfigurationPreview{}, err
	}
	active := servicecatalog.Names()
	if !s.NoSystem {
		active, _, err = serviceSnapshot(ctx, false)
		if err != nil {
			return ConfigurationPreview{}, err
		}
	}
	c.normalize()
	changes, err := deploymentPlan(c, old, active)
	if err != nil {
		return ConfigurationPreview{}, err
	}
	deployment := DeploymentPreview{Changes: changes, RuntimeObserved: !s.NoSystem, AppliedRevision: state.AppliedRevision}
	return ConfigurationPreview{Valid: true, OrdinaryServer: server, Outputs: outputs, Diagnostics: snapshot.Diagnostics, Deployment: deployment}, nil
}

func (s *PlatformService) PreviewOutput(ctx context.Context, c PlatformConfig, id string) (OutputPreview, error) {
	var err error
	c.Subscriptions, err = c.Subscriptions.OnlyOutput(id)
	if err != nil {
		return OutputPreview{}, err
	}
	files, _, err := s.prepareBuild(ctx, c)
	if err != nil {
		return OutputPreview{}, err
	}
	var snapshot sub.Snapshot
	if err = json.Unmarshal(files[subscriptionsSnapshot], &snapshot); err != nil {
		return OutputPreview{}, err
	}
	artifact, found := snapshot.Artifacts[id]
	if !found {
		return OutputPreview{}, errors.New("selected output is unavailable")
	}
	template := snapshot.Templates[c.Subscriptions.Outputs[0].Template]
	preview, err := sub.PreviewArtifact(artifact, template.Client, 64<<10)
	if err != nil {
		return OutputPreview{}, err
	}
	return OutputPreview{ID: id, Name: artifact.Name, MIME: artifact.MIME, Preview: preview, Nodes: snapshot.NodeCounts[id], Diagnostics: snapshot.Diagnostics[id]}, nil
}

func (s *PlatformService) buildConfig(ctx context.Context, c PlatformConfig, mode sub.BuildMode, prepared map[string]sub.SourceCache) (fileBundle, error) {
	c.normalize()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return s.compileConfig(ctx, c, mode, prepared)
}

func (s *PlatformService) compileConfig(ctx context.Context, c PlatformConfig, mode sub.BuildMode, prepared map[string]sub.SourceCache) (fileBundle, error) {
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
	data, err := json.Marshal(server)
	if err != nil {
		return nil, err
	}
	if err = runtimeconfig.ValidateTypes(data); err != nil {
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
	files, _, _, err := compileSubscriptions(ctx, s.Root, s.Core, c.Subscriptions, r, local, mode, AccountService{Root: s.Root, Core: s.Core, Store: s.Accounts, Control: s.Store, Config: &c})
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
