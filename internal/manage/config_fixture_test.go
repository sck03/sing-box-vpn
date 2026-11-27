package manage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sck03/sing-box-vpn/internal/application"
	"github.com/sck03/sing-box-vpn/internal/serverconfig"
	sub "github.com/sck03/sing-box-vpn/internal/subscription"
	"path/filepath"
	"testing"
)

// Fixtures use the production compiler; only synthetic credentials and explicit
// inline subscription sources are supplied here. No alternate mutation API.
func fixtureConfig(root string, app App) (PlatformConfig, error) {
	vpn, secrets, err := loadVPN(root)
	if err != nil {
		return PlatformConfig{}, err
	}
	subscriptions, _, err := loadSubscriptionState(root)
	if err != nil {
		return PlatformConfig{}, err
	}
	if len(subscriptions.Sources) == 0 && len(subscriptions.Outputs) == 0 {
		subscriptions = fixtureSubscriptionConfig()
	}
	var gateway Object
	if _, err := ReadJSON(filepath.Join(root, "vpngate/gateway.json"), &gateway); err != nil {
		return PlatformConfig{}, err
	}
	c := PlatformConfig{Schema: PlatformSchema, App: app, VPN: vpn, VPNSecrets: secrets, Gateway: gateway, Subscriptions: subscriptions, Argo: TunnelSettings{Mode: "off"}, Listener: subscriptionSettings{Listen: "127.0.0.1", Port: 2097}}
	c.normalize()
	return c, nil
}
func publishFixtureConfig(ctx context.Context, root, core string, c PlatformConfig) error {
	local, err := fixtureSubscriptionNodes(c.App, c.VPN, c.VPNSecrets)
	if err != nil {
		return err
	}
	for i, source := range c.Subscriptions.Sources {
		if source.Kind == "inline" && (source.ID == normalScope || source.ID == vpngateScope) {
			data, err := json.Marshal(local[source.ID])
			if err != nil {
				return err
			}
			c.Subscriptions.Sources[i].Content = string(data)
		}
	}
	service := PlatformService{Root: root, Core: core}
	files, err := service.build(ctx, c)
	if err != nil {
		return err
	}
	return publish(root, files)
}
func publishFixtureApp(ctx context.Context, root, core string, app *App) error {
	c, err := fixtureConfig(root, *app)
	if err != nil {
		return err
	}
	return publishFixtureConfig(ctx, root, core, c)
}
func prepareFixtureCountry(ctx context.Context, root, core string, app App, country string) (VPNSettings, error) {
	c, err := fixtureConfig(root, app)
	if err != nil {
		return c.VPN, err
	}
	service := PlatformService{Root: root, Core: core}
	c, err = service.PrepareCountry(ctx, c, country)
	if err != nil {
		return c.VPN, err
	}
	return c.VPN, publishFixtureConfig(ctx, root, core, c)
}
func readArtifact(t *testing.T, root, id string) []byte {
	t.Helper()
	var snapshot sub.Snapshot
	if _, err := ReadJSON(filepath.Join(root, subscriptionsSnapshot), &snapshot); err != nil {
		t.Fatal(err)
	}
	artifact, ok := snapshot.Artifacts[id]
	if !ok {
		t.Fatal("missing published output", id)
	}
	return artifact.Data
}
func readArtifactObject(t *testing.T, root, id string) Object {
	t.Helper()
	var value Object
	if err := json.Unmarshal(readArtifact(t, root, id), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func fixtureSubscriptionNodes(app App, settings VPNSettings, private map[string]Secrets) (map[string][]sub.Node, error) {
	local := map[string][]sub.Node{normalScope: {}, vpngateScope: {}}
	cache := certificateCache{}
	add := func(in Object, p Profile, name, scope string) error {
		managed, err := managedNodes(App{Nodes: fixtureNodes([]Object{in})})
		if err != nil {
			return err
		}
		hopping := []string(nil)
		if number(in["listen_port"]) == p.Ports["hysteria2"] {
			hopping = p.Hopping
		}
		entry := serverconfig.Node(managed[0])
		entry.UDPPorts = hopping
		credential := fixtureCredential(entry.Protocol)
		credential.ID, credential.RuntimeID = entry.ID, scope
		node, err := application.AccountNode(entry, app.Address, name, credential, cache.get)
		if err != nil {
			return err
		}
		if scope == normalScope && node.Protocol == "vmess" && app.CDNAddress != "" {
			node.Server = app.CDNAddress
		}
		if err := node.Validate(); err != nil {
			return err
		}
		local[scope] = append(local[scope], node)
		return nil
	}
	for _, in := range app.Inbounds() {
		if in["_disabled"] == true {
			continue
		}
		kind := text(in["type"])
		if Labels[kind] == "" {
			return nil, errors.New("未知入口协议")
		}
		if err := add(in, app.fixtureProfile(), Labels[kind]+"-普通-"+app.Name, normalScope); err != nil {
			return nil, err
		}
	}
	if cc := settings.ActiveCountry; cc != "" {
		p, exists := settings.Profiles[cc]
		if !exists {
			return nil, errors.New("当前 VPNGate 国家缺少入口配置")
		}
		secret, exists := private[cc]
		if !exists {
			return nil, fmt.Errorf("国家 %s 缺少独立凭据", cc)
		}
		p.Secrets = secret
		label := Countries[cc]
		if label == "" {
			label = cc
		}
		for _, in := range MakeInbounds(p, "vg-"+cc+"-") {
			if err := add(in, p, "VPNGate-"+cc+"-"+label+"-"+Labels[text(in["type"])]+"-家宽候选", vpngateScope); err != nil {
				return nil, err
			}
		}
	}
	return local, nil
}

// Apply through the same revision checks and durable task used by HTTP clients.
func applyFixture(t *testing.T, s *PlatformService, c PlatformConfig, revision int64) error {
	t.Helper()
	_, err := s.QueueApply(t.Context(), c, revision, "test")
	if err != nil {
		return err
	}
	task, err := s.Store.ClaimLane(t.Context(), "control")
	if err != nil {
		return err
	}
	var input applyInput
	if err = json.Unmarshal(task.Input, &input); err != nil {
		return err
	}
	err = s.apply(t.Context(), task.ID, input)
	return errors.Join(err, s.Store.Finish(t.Context(), task.ID, err))
}
