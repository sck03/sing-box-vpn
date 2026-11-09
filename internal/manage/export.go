package manage

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
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

func exportBundle(ctx context.Context, root, core string, app App, settings VPNSettings, private map[string]Secrets) (fileBundle, map[string][]string, error) {
	normal, vpn, mNormal, mVPN := []Object{}, []Object{}, []Object{}, []Object{}
	links := map[string][]string{normalScope: {}, vpngateScope: {}}
	cache := certificateCache{}
	add := func(inbound Object, profile Profile, name string, gateway bool) error {
		p, err := outboundProfile(inbound, profile, cache)
		if err != nil {
			return err
		}
		n, err := outboundWithCache(inbound, p, app.Address, name, cache)
		if err != nil {
			return err
		}
		if !gateway && text(n["type"]) == "vmess" && app.CDNAddress != "" {
			n["server"] = app.CDNAddress
		}
		link, err := uriWithCache(n, p, cache)
		if err != nil {
			return err
		}
		if gateway {
			links[vpngateScope] = append(links[vpngateScope], link)
			vpn = append(vpn, n)
			mVPN = append(mVPN, MihomoNode(n, p))
		} else {
			links[normalScope] = append(links[normalScope], link)
			normal = append(normal, n)
			mNormal = append(mNormal, MihomoNode(n, p))
		}
		return nil
	}
	for _, inbound := range app.Inbounds {
		kind := text(inbound["type"])
		if Labels[kind] == "" {
			return nil, nil, errors.New("未知入口协议")
		}
		if err := add(inbound, app.Profile, Labels[kind]+"-普通-"+app.Name, false); err != nil {
			return nil, nil, err
		}
	}
	if domain := text(app.Argo["domain"]); domain != "" {
		var vmess Object
		for _, node := range normal {
			if node["type"] == "vmess" {
				vmess = clone(node).(Object)
				break
			}
		}
		if vmess == nil {
			return nil, nil, errors.New("Argo 缺少 VMess 入口")
		}
		address := text(app.Argo["cdn_address"])
		if address == "" {
			address = domain
		}
		vmess["tag"], vmess["server"], vmess["server_port"] = "VMess-Argo-普通-"+app.Name, address, 443
		vmess["tls"] = Object{"enabled": true, "server_name": domain}
		ensureObject(vmess, "transport")["headers"] = Object{"Host": domain}
		p := app.Profile
		p.TLSSNI, p.SelfSigned = domain, false
		link, err := uriWithCache(vmess, p, cache)
		if err != nil {
			return nil, nil, err
		}
		links[normalScope] = append(links[normalScope], link)
		normal = append(normal, vmess)
		mNormal = append(mNormal, MihomoNode(vmess, p))
	}
	if cc := settings.ActiveCountry; cc != "" {
		p, exists := settings.Profiles[cc]
		if !exists {
			return nil, nil, errors.New("当前 VPNGate 国家缺少入口配置")
		}
		secret, exists := private[cc]
		if !exists {
			return nil, nil, fmt.Errorf("国家 %s 缺少独立凭据", cc)
		}
		p.Secrets = secret
		label := Countries[cc]
		if label == "" {
			label = cc
		}
		for _, inbound := range MakeInbounds(p, "vg-"+cc+"-") {
			if err := add(inbound, p, "VPNGate-"+cc+"-"+label+"-"+Labels[text(inbound["type"])]+"-家宽候选", true); err != nil {
				return nil, nil, err
			}
		}
	}
	files := fileBundle{}
	for _, set := range []struct {
		scope         string
		nodes, mihomo []Object
	}{{normalScope, normal, mNormal}, {vpngateScope, vpn, mVPN}} {
		gateway := set.scope == vpngateScope
		prefix := ""
		if gateway {
			prefix = "vpngate/"
			if len(set.nodes) == 0 {
				for _, name := range exportNames {
					files[prefix+name] = nil
				}
				continue
			}
		}
		client := ClientConfig(set.nodes, gateway)
		if err := CheckJSON(ctx, core, root, client); err != nil {
			return nil, nil, err
		}
		if err := files.json(prefix+"sbox.json", client); err != nil {
			return nil, nil, err
		}
		if err := files.json(prefix+"clmi.yaml", MihomoConfig(set.mihomo, gateway)); err != nil {
			return nil, nil, err
		}
		// A provider carries only native nodes; OpenClash owns router DNS, TUN,
		// policy groups and routing. Reuse the same nodes as the full config.
		if err := files.json(prefix+"proxies.yaml", Object{"proxies": set.mihomo}); err != nil {
			return nil, nil, err
		}
		joined := strings.Join(links[set.scope], "\n")
		files[prefix+"jhsub.txt"] = []byte(joined + "\n")
		files[prefix+"jhsub-base64.txt"] = []byte(base64.StdEncoding.EncodeToString([]byte(joined)) + "\n")
		share := Object{"links": links[set.scope], "normal": []string{}, "vpngate": []string{}}
		names := make([]string, 0, len(set.nodes))
		for _, n := range set.nodes {
			names = append(names, text(n["tag"]))
		}
		share[set.scope] = names
		if err := files.json(prefix+"share.json", share); err != nil {
			return nil, nil, err
		}
	}
	return files, links, nil
}
