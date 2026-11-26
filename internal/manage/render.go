package manage

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"

	"path/filepath"
	"slices"
)

func NewProfile(root string, used map[int]bool, suffix string, excluded ...portRange) (Profile, error) {
	return newProfile(root, used, suffix, Protocols, excluded...)
}

func newProfile(root string, used map[int]bool, suffix string, protocols []string, excluded ...portRange) (Profile, error) {
	directory := root
	if suffix != "" {
		directory = filepath.Join(root, "vpngate")
	}
	p := Profile{Ports: map[string]int{}, Certificate: filepath.ToSlash(filepath.Join(directory, suffix+"cert.pem")), Key: filepath.ToSlash(filepath.Join(directory, suffix+"private.key")), RealitySNI: "www.apple.com", TLSSNI: "www.bing.com", WSPath: "/" + hex.EncodeToString(randomBytes(16))}
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
	p.Secrets.Reality = Reality{Private: base64.RawURLEncoding.EncodeToString(key.Bytes()), ShortID: hex.EncodeToString(randomBytes(4))}
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
		n := ManagedNode{ID: prefix + Tags[kind], Protocol: kind, Port: p.Ports[kind], UDPPorts: append([]string(nil), forwardPorts(p, kind)...), TLS: ManagedTLS{Enabled: true, ServerName: p.TLSSNI, Certificate: p.Certificate, Key: p.Key}}
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

// Ordinary routing and outbounds have one source: App.Advanced. An empty
// configuration defaults to direct egress and the system DNS resolver.
func defaultServerConfig(inbounds []Object) Object {
	return Object{
		"log":       Object{"level": "info", "timestamp": true},
		"inbounds":  inbounds,
		"outbounds": []Object{{"type": "direct", "tag": "direct"}},
		"route":     Object{"rules": []Object{}, "final": "direct", "default_domain_resolver": "dns-local"},
		"dns":       Object{"servers": []Object{{"type": "local", "tag": "dns-local"}}},
	}
}
