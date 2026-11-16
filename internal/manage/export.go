package manage

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	sub "github.com/sck03/sing-box-vpn/internal/subscription"
)

type fileBundle map[string][]byte

const normalScope = "normal"
const vpngateScope = "vpngate"

var exportNames = []string{"sbox.json", "clmi.yaml", "proxies.yaml", "jhsub.txt", "jhsub-base64.txt", "share.json"}

func exportDirectory(root, scope string) (string, error) {
	switch scope {
	case normalScope:
		return root, nil
	case vpngateScope:
		return filepath.Join(root, "vpngate"), nil
	default:
		return "", errors.New("节点类型必须为 normal 或 vpngate")
	}
}
func (files fileBundle) json(name string, value any) error {
	data, err := JSONBytes(value)
	if err == nil {
		files[name] = data
	}
	return err
}

func collectLocalSubscriptionNodes(app App, settings VPNSettings, private map[string]Secrets) (map[string][]sub.Node, error) {
	local := map[string][]sub.Node{normalScope: {}, vpngateScope: {}}
	cache := certificateCache{}
	add := func(in Object, p Profile, name, scope string) error {
		managed, err := managedNodes(App{Profile: p, Inbounds: []Object{in}})
		if err != nil {
			return err
		}
		hopping := []string(nil)
		if number(in["listen_port"]) == p.Ports["hysteria2"] {
			hopping = p.Hopping
		}
		node, err := managed[0].subscriptionNode(app.Address, name, scope, hopping, cache)
		if err != nil {
			return err
		}
		if scope == normalScope && node.Protocol == "vmess" && app.CDNAddress != "" {
			node.Server = app.CDNAddress
		}
		local[scope] = append(local[scope], node)
		return nil
	}
	for _, in := range app.Inbounds {
		if in["_disabled"] == true {
			continue
		}
		kind := text(in["type"])
		if Labels[kind] == "" {
			return nil, errors.New("未知入口协议")
		}
		if err := add(in, app.Profile, Labels[kind]+"-普通-"+app.Name, normalScope); err != nil {
			return nil, err
		}
	}
	if domain := text(app.Argo["domain"]); domain != "" {
		found := false
		for _, node := range local[normalScope] {
			if node.Protocol != "vmess" {
				continue
			}
			node.ID, node.Name, node.Server, node.Port = "normal-argo", "VMess-Argo-普通-"+app.Name, domain, 443
			if address := text(app.Argo["cdn_address"]); address != "" {
				node.Server = address
			}
			node.TLS = sub.TLS{Enabled: true, ServerName: domain}
			node.Transport.Host = domain
			local[normalScope] = append(local[normalScope], node)
			found = true
			break
		}
		if !found {
			return nil, errors.New("Argo 缺少 VMess 入口")
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

// Every delivery path uses these same validated artifacts. The conventional
// filenames are local exports, not a second conversion pipeline.
func exportBundle(ctx context.Context, root, core string, app App, settings VPNSettings, private map[string]Secrets) (fileBundle, map[string][]string, error) {
	local, err := collectLocalSubscriptionNodes(app, settings, private)
	if err != nil {
		return nil, nil, err
	}
	c, r, _, err := loadSubscriptions(root)
	if err != nil {
		return nil, nil, err
	}
	files, _, _, err := compileSubscriptions(ctx, root, core, c, r, local, true)
	if err != nil {
		return nil, nil, err
	}
	links := map[string][]string{normalScope: {}, vpngateScope: {}}
	for _, scope := range []string{normalScope, vpngateScope} {
		prefix := ""
		if scope == vpngateScope {
			prefix = "vpngate/"
		}
		if data := files[prefix+"jhsub.txt"]; len(data) > 0 {
			links[scope] = strings.Split(strings.TrimSpace(string(data)), "\n")
		}
	}
	return files, links, nil
}

func appendDefaultExports(files fileBundle, snapshot sub.Snapshot, previews map[string]sub.Preview) error {
	links := map[string][]string{normalScope: {}, vpngateScope: {}}
	for _, scope := range []string{normalScope, vpngateScope} {
		prefix := ""
		if scope == vpngateScope {
			prefix = "vpngate/"
		}
		for name, target := range map[string]string{"sbox.json": "sing-box", "clmi.yaml": "mihomo", "proxies.yaml": "provider", "jhsub.txt": "uri", "jhsub-base64.txt": "base64"} {
			artifact, found := snapshot.Artifacts[scope+"-"+target]
			if found {
				files[prefix+name] = artifact.Data
			} else {
				files[prefix+name] = nil
			}
		}
		if data := files[prefix+"jhsub.txt"]; len(data) > 0 {
			links[scope] = strings.Split(strings.TrimSpace(string(data)), "\n")
		}
		if len(links[scope]) == 0 {
			files[prefix+"share.json"] = nil
			continue
		}
		names := []string{}
		for _, node := range previews[scope].Nodes {
			names = append(names, node.Name)
		}
		share := Object{"links": links[scope], "normal": []string{}, "vpngate": []string{}}
		share[scope] = names
		if err := files.json(prefix+"share.json", share); err != nil {
			return err
		}
	}
	return nil
}
