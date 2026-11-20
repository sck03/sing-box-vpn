package manage

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"path/filepath"
	"slices"
	"strings"
)

func NewProfile(root string, used map[int]bool, suffix string, excluded ...portRange) (Profile, error) {
	return newProfile(root, used, suffix, Protocols, excluded...)
}

func newProfile(root string, used map[int]bool, suffix string, protocols []string, excluded ...portRange) (Profile, error) {
	directory := root
	if suffix != "" {
		directory = filepath.Join(root, "vpngate")
	}
	p := Profile{Ports: map[string]int{}, Certificate: filepath.ToSlash(filepath.Join(directory, suffix+"cert.pem")), Key: filepath.ToSlash(filepath.Join(directory, suffix+"private.key")), RealitySNI: "www.apple.com", TLSSNI: "www.bing.com", SelfSigned: true, WSPath: "/" + hex.EncodeToString(randomBytes(16))}
	if slices.ContainsFunc(protocols, func(protocol string) bool { return protocol != "vless" && protocol != "vmess" }) {
		if err := EnsureCertificate(p.Certificate, p.Key, p.TLSSNI); err != nil {
			return p, err
		}
	}
	for _, protocol := range protocols {
		port, err := AvailablePort(used, excluded...)
		if err != nil {
			return p, err
		}
		p.Ports[protocol] = port
	}
	if !slices.Contains(protocols, "vless") {
		return p, nil
	}
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return p, err
	}
	p.Secrets.Reality = Reality{Private: base64.RawURLEncoding.EncodeToString(key.Bytes()), Public: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), ShortID: hex.EncodeToString(randomBytes(4))}
	return p, nil
}

func merge(values ...Object) Object {
	out := Object{}
	for _, value := range values {
		for key, item := range value {
			out[key] = item
		}
	}
	return out
}

func MakeNodes(p Profile, prefix string) []ManagedNode {
	host, port := p.HandshakeHost, p.HandshakePort
	if host == "" {
		host = p.RealitySNI
	}
	if port == 0 {
		port = 443
	}
	nodes := make([]ManagedNode, 0, len(Protocols))
	for _, kind := range Protocols {
		n := ManagedNode{ID: prefix + Tags[kind], Protocol: kind, Port: p.Ports[kind], TLS: ManagedTLS{Enabled: true, ServerName: p.TLSSNI, Certificate: p.Certificate, Key: p.Key}}
		if kind == "vless" {
			n.Flow = "xtls-rprx-vision"
			n.TLS = ManagedTLS{Enabled: true, ServerName: p.RealitySNI, RealityPrivate: p.Secrets.Reality.Private, RealityShortID: p.Secrets.Reality.ShortID, HandshakeHost: host, HandshakePort: port}
		}
		if kind == "vmess" {
			n.TLS.Enabled = p.VMessTLS
			n.Transport = ManagedTransport{Type: "ws", Path: p.WSPath, Host: p.TLSSNI}
		}
		nodes = append(nodes, n)
	}
	return nodes
}

func MakeInbounds(p Profile, prefix string) []Object {
	return (App{Nodes: MakeNodes(p, prefix)}).Inbounds()
}

func ServerConfig(inbounds []Object, routing map[string][]string, endpoints []Object, socksPort int) (Object, error) {
	if socksPort == 0 {
		socksPort = 40000
	}
	rules := []Object{}
	for _, name := range []string{"socks", "warp", "direct4", "direct6"} {
		domains := routing[name]
		if len(domains) == 0 {
			continue
		}
		if len(rules) == 0 {
			rules = append(rules, Object{"action": "sniff"})
		}
		if strings.HasPrefix(name, "direct") {
			strategy := "prefer_ipv4"
			if name == "direct6" {
				strategy = "prefer_ipv6"
			}
			rules = append(rules, Object{"domain_suffix": domains, "action": "resolve", "strategy": strategy})
		}
		if name == "warp" && !slices.ContainsFunc(endpoints, func(endpoint Object) bool { return endpoint["type"] == "wireguard" && endpoint["tag"] == "warp-out" }) {
			return nil, errors.New("请先配置 WARP endpoint")
		}
		target := "direct"
		if name == "socks" {
			target = "socks-out"
		}
		if name == "warp" {
			target = "warp-out"
		}
		rules = append(rules, Object{"domain_suffix": domains, "action": "route", "outbound": target})
	}
	result := Object{"log": Object{"level": "info", "timestamp": true}, "inbounds": inbounds, "outbounds": []Object{{"type": "direct", "tag": "direct"}, {"type": "socks", "tag": "socks-out", "server": "127.0.0.1", "server_port": socksPort, "version": "5"}}, "route": Object{"rules": rules, "final": "direct"}}
	result["dns"] = Object{"servers": []Object{{"type": "local", "tag": "dns-local"}}}
	object(result["route"])["default_domain_resolver"] = "dns-local"
	if len(endpoints) > 0 {
		result["endpoints"] = endpoints
	}
	return result, nil
}
