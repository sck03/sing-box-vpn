package manage

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"strconv"
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
		n["security"] = "auto"
		n["transport"] = Object{"type": "ws", "path": object(inbound["transport"])["path"], "headers": Object{"Host": p.TLSSNI}}
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

type queryPair struct{ key, value string }

func query(values []queryPair) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = url.QueryEscape(value.key) + "=" + url.QueryEscape(value.value)
	}
	return strings.Join(parts, "&")
}
func quote(value string) string { return strings.ReplaceAll(url.QueryEscape(value), "+", "%20") }

func webSocketHost(transport Object, fallback string) string {
	if host := text(object(transport["headers"])["Host"]); host != "" {
		return host
	}
	return fallback
}

func URI(n Object, p Profile) (string, error) { return uriWithCache(n, p, certificateCache{}) }
func uriWithCache(n Object, p Profile, cache certificateCache) (string, error) {
	kind, address := text(n["type"]), text(n["server"])
	if strings.Contains(address, ":") {
		address = "[" + address + "]"
	}
	endpoint := address + ":" + strconv.Itoa(number(n["server_port"]))
	name := quote(text(n["tag"]))
	tls := object(n["tls"])
	sni := text(tls["server_name"])
	if sni == "" {
		sni = p.TLSSNI
	}
	values := []queryPair{{"sni", sni}}
	insecure := "0"
	if p.SelfSigned {
		insecure = "1"
	}
	switch kind {
	case "vless":
		r := object(tls["reality"])
		values = append(values, queryPair{"encryption", "none"}, queryPair{"flow", "xtls-rprx-vision"}, queryPair{"security", "reality"}, queryPair{"fp", "chrome"}, queryPair{"pbk", text(r["public_key"])}, queryPair{"sid", text(r["short_id"])}, queryPair{"type", "tcp"})
		return "vless://" + text(n["uuid"]) + "@" + endpoint + "?" + query(values) + "#" + name, nil
	case "vmess":
		transport := object(n["transport"])
		host := webSocketHost(transport, p.TLSSNI)
		value := struct {
			V    string `json:"v"`
			PS   string `json:"ps"`
			Add  string `json:"add"`
			Port string `json:"port"`
			ID   string `json:"id"`
			AID  string `json:"aid"`
			SCY  string `json:"scy"`
			Net  string `json:"net"`
			Type string `json:"type"`
			Host string `json:"host"`
			Path string `json:"path"`
			TLS  string `json:"tls"`
			SNI  string `json:"sni"`
		}{"2", text(n["tag"]), text(n["server"]), strconv.Itoa(number(n["server_port"])), text(n["uuid"]), "0", "auto", "ws", "none", host, text(transport["path"]), "", text(tls["server_name"])}
		if tls["enabled"] == true {
			value.TLS = "tls"
		}
		data, err := json.Marshal(value)
		if err != nil {
			return "", err
		}
		return "vmess://" + base64.StdEncoding.EncodeToString(data), nil
	case "hysteria2":
		values = append(values, queryPair{"alpn", "h3"}, queryPair{"insecure", insecure})
		if !p.SelfSigned {
			info, err := cache.get(p.Certificate)
			if err != nil {
				return "", err
			}
			values = append(values, queryPair{"pinSHA256", info.Hash})
		}
		if len(p.Hopping) > 0 {
			values = append(values, queryPair{"mport", strings.ReplaceAll(strings.Join(p.Hopping, ","), ":", "-")})
		}
		return "hysteria2://" + quote(text(n["password"])) + "@" + endpoint + "?" + query(values) + "#" + name, nil
	case "tuic":
		values = append(values, queryPair{"congestion_control", "bbr"}, queryPair{"udp_relay_mode", "native"}, queryPair{"alpn", "h3"}, queryPair{"allow_insecure", insecure})
		return "tuic://" + text(n["uuid"]) + ":" + quote(text(n["password"])) + "@" + endpoint + "?" + query(values) + "#" + name, nil
	case "anytls":
		values = append(values, queryPair{"insecure", insecure})
		return "anytls://" + quote(text(n["password"])) + "@" + endpoint + "?" + query(values) + "#" + name, nil
	}
	return "", errors.New("未知协议")
}

func ClientConfig(nodes []Object, gateway bool) Object {
	names := make([]string, 0, len(nodes))
	for _, node := range nodes {
		names = append(names, text(node["tag"]))
	}
	selectors := []Object{{"type": "selector", "tag": "proxy", "outbounds": names}}
	if !gateway {
		selectors = []Object{{"type": "urltest", "tag": "auto", "outbounds": names, "url": "https://www.gstatic.com/generate_204", "interval": "10m", "tolerance": 30}, {"type": "selector", "tag": "proxy", "outbounds": append([]string{"auto"}, names...), "default": "auto"}}
	}
	outbounds := append(append([]Object{}, nodes...), selectors...)
	outbounds = append(outbounds, Object{"type": "direct", "tag": "direct"})
	result := Object{"log": Object{"level": "info"}, "http_clients": []Object{{"tag": "rule-set-direct"}},
		"dns":          Object{"servers": []Object{{"type": "fakeip", "tag": "fakeip", "inet4_range": "198.18.0.0/15"}, {"type": "udp", "tag": "dns-cn", "server": "223.5.5.5"}, {"type": "https", "tag": "dns-proxy", "server": "1.1.1.1", "detour": "proxy"}}, "rules": []Object{{"rule_set": []string{"cn-domains"}, "action": "route", "server": "dns-cn"}, {"query_type": []string{"A", "AAAA"}, "action": "route", "server": "fakeip"}}, "final": "dns-proxy", "strategy": "ipv4_only", "reverse_mapping": true},
		"inbounds":     []Object{{"type": "tun", "tag": "tun-in", "address": []string{"172.19.0.1/30"}, "auto_route": true, "strict_route": true, "mtu": 1420}},
		"route":        Object{"default_http_client": "rule-set-direct", "default_domain_resolver": "dns-cn", "auto_detect_interface": true, "rule_set": []Object{{"type": "remote", "tag": "cn-domains", "format": "binary", "url": "https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/cn.srs"}, {"type": "remote", "tag": "cn-ips", "format": "binary", "url": "https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geoip/cn.srs"}}, "rules": []Object{{"port": 53, "action": "hijack-dns"}, {"clash_mode": "Global", "action": "route", "outbound": "proxy"}, {"clash_mode": "Direct", "action": "route", "outbound": "direct"}, {"ip_is_private": true, "action": "route", "outbound": "direct"}, {"rule_set": []string{"cn-domains", "cn-ips"}, "action": "route", "outbound": "direct"}}, "final": "proxy"},
		"experimental": Object{"cache_file": Object{"enabled": true, "store_dns": true}, "clash_api": Object{"external_controller": "127.0.0.1:9090", "default_mode": "Rule"}}, "outbounds": outbounds}
	if gateway {
		delete(result, "http_clients")
		route := object(result["route"])
		delete(route, "default_http_client")
		delete(route, "rule_set")
		route["rules"] = []Object{{"port": 53, "action": "hijack-dns"}, {"ip_is_private": true, "action": "route", "outbound": "direct"}}
		object(result["dns"])["rules"] = []Object{{"query_type": []string{"A", "AAAA"}, "action": "route", "server": "fakeip"}}
	}
	return result
}

func MihomoNode(n Object, p Profile) Object {
	kind := text(n["type"])
	out := Object{"name": n["tag"], "type": kind, "server": n["server"], "port": n["server_port"], "udp": true}
	for _, key := range []string{"uuid", "password"} {
		if value, ok := n[key]; ok {
			out[key] = value
		}
	}
	tls := object(n["tls"])
	sni := text(tls["server_name"])
	if sni == "" {
		sni = p.TLSSNI
	}
	switch kind {
	case "vless":
		out = merge(out, Object{"network": "tcp", "tls": true, "flow": "xtls-rprx-vision", "servername": tls["server_name"], "client-fingerprint": "chrome", "reality-opts": Object{"public-key": object(tls["reality"])["public_key"], "short-id": object(tls["reality"])["short_id"]}})
	case "vmess":
		transport := object(n["transport"])
		out = merge(out, Object{"network": "ws", "tls": tls["enabled"], "cipher": "auto", "alterId": 0, "servername": tls["server_name"], "ws-opts": Object{"path": transport["path"], "headers": Object{"Host": webSocketHost(transport, p.TLSSNI)}}, "skip-cert-verify": p.SelfSigned})
	default:
		out["sni"], out["skip-cert-verify"] = sni, p.SelfSigned
		if kind == "hysteria2" || kind == "tuic" {
			out["alpn"] = []string{"h3"}
		}
		if kind == "tuic" {
			out["congestion-controller"], out["udp-relay-mode"] = "bbr", "native"
		}
		if kind == "anytls" {
			out["idle-session-check-interval"], out["idle-session-timeout"], out["min-idle-session"] = 30, 30, anyTLSMinIdleSessions
		}
		if kind == "hysteria2" && len(p.Hopping) > 0 {
			out["ports"] = strings.ReplaceAll(strings.Join(p.Hopping, ","), ":", "-")
		}
	}
	return out
}

func MihomoConfig(nodes []Object, gateway bool) Object {
	names := []string{}
	for _, node := range nodes {
		names = append(names, text(node["name"]))
	}
	choices := append([]string{"自动选择"}, names...)
	choices = append(choices, "DIRECT")
	groups := []Object{{"name": "节点选择", "type": "select", "proxies": choices}, {"name": "自动选择", "type": "url-test", "proxies": names, "url": "https://www.gstatic.com/generate_204", "interval": 600}}
	rules := []string{"IP-CIDR,127.0.0.0/8,DIRECT,no-resolve", "IP-CIDR,192.168.0.0/16,DIRECT,no-resolve"}
	if gateway {
		groups = []Object{{"name": "临时家宽", "type": "select", "proxies": names}}
		rules = append(rules, "MATCH,临时家宽")
	} else {
		rules = append(rules, "GEOIP,CN,DIRECT", "MATCH,节点选择")
	}
	return Object{"mixed-port": 7890, "allow-lan": false, "mode": "rule", "log-level": "info", "dns": Object{"enable": true, "ipv6": false, "enhanced-mode": "fake-ip", "fake-ip-range": "198.18.0.1/16", "respect-rules": true, "default-nameserver": []string{"223.5.5.5"}, "nameserver": []string{"https://1.1.1.1/dns-query"}, "proxy-server-nameserver": []string{"https://223.5.5.5/dns-query"}}, "proxies": nodes, "proxy-groups": groups, "rules": rules}
}
