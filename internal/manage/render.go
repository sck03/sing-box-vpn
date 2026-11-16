package manage

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
)

func NewProfile(root string, used map[int]bool, suffix string, excluded ...portRange) (Profile, error) {
	directory := root
	if suffix != "" {
		directory = filepath.Join(root, "vpngate")
	}
	p := Profile{Ports: map[string]int{}, Certificate: filepath.ToSlash(filepath.Join(directory, suffix+"cert.pem")), Key: filepath.ToSlash(filepath.Join(directory, suffix+"private.key")), RealitySNI: "www.apple.com", TLSSNI: "www.bing.com", SelfSigned: true, WSPath: "/" + hex.EncodeToString(randomBytes(16)), Secrets: Secrets{UUID: map[string]string{}, Passwords: map[string]string{}}}
	if err := EnsureCertificate(p.Certificate, p.Key, p.TLSSNI); err != nil {
		return p, err
	}
	for _, protocol := range Protocols {
		port, err := AvailablePort(used, excluded...)
		if err != nil {
			return p, err
		}
		p.Ports[protocol] = port
	}
	for _, protocol := range []string{"vless", "vmess", "tuic"} {
		p.Secrets.UUID[protocol] = randomUUID()
	}
	for _, protocol := range []string{"hysteria2", "tuic", "anytls"} {
		p.Secrets.Passwords[protocol] = randomSecret()
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

func MakeInbounds(p Profile, prefix string) []Object {
	tls := Object{"enabled": true, "certificate_path": p.Certificate, "key_path": p.Key}
	handshakeHost, handshakePort := p.HandshakeHost, p.HandshakePort
	if handshakeHost == "" {
		handshakeHost = p.RealitySNI
	}
	if handshakePort == 0 {
		handshakePort = 443
	}
	s := p.Secrets
	return []Object{
		{"type": "vless", "tag": prefix + Tags["vless"], "listen": "::", "listen_port": p.Ports["vless"], "users": []Object{{"uuid": s.UUID["vless"], "flow": "xtls-rprx-vision"}}, "tls": Object{"enabled": true, "server_name": p.RealitySNI, "reality": Object{"enabled": true, "handshake": Object{"server": handshakeHost, "server_port": handshakePort}, "private_key": s.Reality.Private, "short_id": []string{s.Reality.ShortID}}}},
		{"type": "vmess", "tag": prefix + Tags["vmess"], "listen": "::", "listen_port": p.Ports["vmess"], "users": []Object{{"uuid": s.UUID["vmess"], "alterId": 0}}, "transport": Object{"type": "ws", "path": p.WSPath, "max_early_data": 2048, "early_data_header_name": "Sec-WebSocket-Protocol"}, "tls": merge(tls, Object{"enabled": p.VMessTLS, "server_name": p.TLSSNI})},
		{"type": "hysteria2", "tag": prefix + Tags["hysteria2"], "listen": "::", "listen_port": p.Ports["hysteria2"], "users": []Object{{"password": s.Passwords["hysteria2"]}}, "ignore_client_bandwidth": false, "tls": merge(tls, Object{"alpn": []string{"h3"}})},
		{"type": "tuic", "tag": prefix + Tags["tuic"], "listen": "::", "listen_port": p.Ports["tuic"], "users": []Object{{"uuid": s.UUID["tuic"], "password": s.Passwords["tuic"]}}, "congestion_control": "bbr", "tls": merge(tls, Object{"alpn": []string{"h3"}})},
		{"type": "anytls", "tag": prefix + Tags["anytls"], "listen": "::", "listen_port": p.Ports["anytls"], "users": []Object{{"password": s.Passwords["anytls"]}}, "tls": merge(tls)},
	}
}

func ServerConfig(inbounds []Object, routing map[string][]string, warp Object, socksPort int) (Object, error) {
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
		if name == "warp" && len(warp) == 0 {
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
	if len(warp) > 0 {
		result["endpoints"] = []Object{merge(clone(warp).(Object), Object{"type": "wireguard", "tag": "warp-out"})}
	}
	return result, nil
}
