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

const anyTLSMinIdleSessions = 1

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

func outboundProfile(inbound Object, p Profile, cache certificateCache) (Profile, error) {
	tls := object(inbound["tls"])
	if text(inbound["type"]) != "vless" && tls["enabled"] == true {
		p.Certificate = text(tls["certificate_path"])
		info, err := cache.get(p.Certificate)
		if err != nil {
			return p, err
		}
		p.SelfSigned = info.SelfSigned
		if sni := text(tls["server_name"]); sni != "" {
			p.TLSSNI = sni
		}
	}
	return p, nil
}

func Outbound(inbound Object, p Profile, address, name string) (Object, error) {
	return outboundWithCache(inbound, p, address, name, certificateCache{})
}
func outboundWithCache(inbound Object, p Profile, address, name string, cache certificateCache) (Object, error) {
	kind := text(inbound["type"])
	n := Object{"type": kind, "tag": name, "server": address, "server_port": number(inbound["listen_port"])}
	users := objects(inbound["users"])
	if len(users) == 0 {
		return nil, errors.New("入口缺少用户凭据")
	}
	for _, key := range []string{"uuid", "password"} {
		if value, ok := users[0][key]; ok {
			n[key] = value
		}
	}
	serverTLS := object(inbound["tls"])
	if kind == "vless" {
		n["flow"] = "xtls-rprx-vision"
		n["tls"] = Object{"enabled": true, "server_name": serverTLS["server_name"], "utls": Object{"enabled": true, "fingerprint": "chrome"}, "reality": Object{"enabled": true, "public_key": p.Secrets.Reality.Public, "short_id": p.Secrets.Reality.ShortID}}
		return n, nil
	}
	var err error
	p, err = outboundProfile(inbound, p, cache)
	if err != nil {
		return nil, err
	}
	sni := text(serverTLS["server_name"])
	if sni == "" {
		sni = p.TLSSNI
	}
	tls := Object{"enabled": serverTLS["enabled"] == true, "server_name": sni}
	n["tls"] = tls
	if tls["enabled"] == true {
		if p.SelfSigned {
			info, err := cache.get(p.Certificate)
			if err != nil {
				return nil, err
			}
			tls["certificate_public_key_sha256"] = []string{info.Pin}
		}
		if kind == "hysteria2" || kind == "tuic" {
			tls["alpn"] = []string{"h3"}
		}
	}
	switch kind {
	case "vmess":
		path := text(object(inbound["transport"])["path"])
		if err := validateWebSocketPath(path); err != nil {
			return nil, err
		}
		n["security"] = "auto"
		n["transport"] = Object{"type": "ws", "path": path, "headers": Object{"Host": p.TLSSNI}}
	case "tuic":
		n["congestion_control"], n["udp_relay_mode"], n["zero_rtt_handshake"], n["heartbeat"] = "bbr", "native", false, "10s"
	case "anytls":
		n["idle_session_check_interval"], n["idle_session_timeout"], n["min_idle_session"] = "30s", "30s", anyTLSMinIdleSessions
	case "hysteria2":
		if len(p.Hopping) > 0 {
			ports := make([]string, len(p.Hopping))
			for i, value := range p.Hopping {
				ports[i] = value
				if !strings.Contains(value, ":") {
					ports[i] += ":" + value
				}
			}
			n["server_ports"] = ports
		}
	}
	return n, nil
}
