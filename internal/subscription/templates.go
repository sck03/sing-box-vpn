package subscription

import "strings"

func groups(nodes []Node) (normal, gateway []string) {
	for _, n := range nodes {
		if n.Gateway {
			gateway = append(gateway, n.Name)
		} else {
			normal = append(normal, n.Name)
		}
	}
	return
}

func finalGroup(nodes []Node) string {
	normal, _ := groups(nodes)
	if len(normal) == 0 {
		return "VPNGate"
	}
	return "PROXY"
}
func templateDNS(t Template) []string {
	if len(t.DNS) > 0 {
		return t.DNS
	}
	return []string{"1.1.1.1", "8.8.8.8"}
}

func singBoxTemplate(nodes []Node, native []Object, t Template) Object {
	normal, gateway := groups(nodes)
	if len(normal) > 0 {
		native = append(native, Object{"type": "selector", "tag": "PROXY", "outbounds": normal})
	}
	if len(gateway) > 0 {
		native = append(native, Object{"type": "selector", "tag": "VPNGate", "outbounds": gateway})
	}
	if len(normal) > 0 {
		native = append(native, Object{"type": "direct", "tag": "DIRECT"})
	}
	final := finalGroup(nodes)
	rules := []Object{{"port": 53, "action": "hijack-dns"}}
	for _, rule := range t.Rules {
		r := Object{rule.Type: []string{rule.Value}, "action": "route", "outbound": rule.Target}
		if rule.Target == "REJECT" {
			r["action"] = "reject"
			delete(r, "outbound")
		}
		if rule.Target == "PROXY" {
			r["outbound"] = final
		}
		rules = append(rules, r)
	}
	route := Object{"final": final, "rules": rules, "auto_detect_interface": true, "default_domain_resolver": "bootstrap"}
	if t.Preset == "split" {
		route["rule_set"] = []Object{{"type": "remote", "tag": "cn-domains", "format": "binary", "url": "https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/cn.srs"}, {"type": "remote", "tag": "cn-ips", "format": "binary", "url": "https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geoip/cn.srs"}}
		route["rules"] = append(rules, Object{"rule_set": []string{"cn-domains", "cn-ips"}, "action": "route", "outbound": "DIRECT"})
	}
	inbounds := []Object{{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": 7890}}
	if t.TUN {
		inbounds = append(inbounds, Object{"type": "tun", "tag": "tun-in", "address": []string{"172.19.0.1/30"}, "auto_route": true, "strict_route": true, "mtu": 1420})
	}
	return Object{"log": Object{"level": "warn"}, "inbounds": inbounds, "outbounds": native, "route": route, "dns": Object{"servers": []Object{{"type": "udp", "tag": "bootstrap", "server": templateDNS(t)[0]}, {"type": "udp", "tag": "remote", "server": templateDNS(t)[0], "detour": final}}, "final": "remote"}}
}

func clashTemplate(nodes []Node, native []Object, t Template) Object {
	normal, gateway := groups(nodes)
	group := []Object{}
	if len(normal) > 0 {
		group = append(group, Object{"name": "PROXY", "type": "select", "proxies": normal})
	}
	if len(gateway) > 0 {
		group = append(group, Object{"name": "VPNGate", "type": "select", "proxies": gateway})
	}
	if t.ProbeURL != "" && len(normal) > 0 {
		group = append(group, Object{"name": "AUTO", "type": "url-test", "proxies": normal, "url": t.ProbeURL, "interval": 600})
		group[0]["proxies"] = append([]string{"AUTO"}, normal...)
	}
	rules := []string{}
	for _, r := range t.Rules {
		target := r.Target
		if target == "PROXY" {
			target = finalGroup(nodes)
		}
		rules = append(rules, textRule(r, target))
	}
	if t.Preset == "split" {
		rules = append(rules, "GEOIP,CN,DIRECT")
	}
	rules = append(rules, "MATCH,"+finalGroup(nodes))
	result := Object{"mixed-port": 7890, "allow-lan": false, "mode": "rule", "log-level": "warning", "proxies": native, "proxy-groups": group, "rules": rules, "dns": Object{"enable": true, "respect-rules": true, "enhanced-mode": "fake-ip", "nameserver": templateDNS(t), "proxy-server-nameserver": templateDNS(t)}}
	if t.TUN {
		result["tun"] = Object{"enable": true, "stack": "mixed", "auto-route": true, "auto-detect-interface": true, "dns-hijack": []string{"any:53"}}
	}
	return result
}

func textRule(r Rule, target string) string {
	kind := map[string]string{"domain": "DOMAIN", "domain_suffix": "DOMAIN-SUFFIX", "ip_cidr": "IP-CIDR"}[r.Type]
	if r.Type == "ip_cidr" && strings.Contains(r.Value, ":") {
		kind = "IP-CIDR6"
	}
	return kind + "," + r.Value + "," + target
}

func stashNode(n Node) Object {
	m := n.Mihomo()
	if n.Protocol == "hysteria2" {
		m["auth"] = n.Password
		delete(m, "password")
	}
	if n.Protocol == "tuic" {
		m["version"] = 5
	}
	// Stash has its own schema; only its documented common fields are emitted.
	delete(m, "client-fingerprint")
	delete(m, "fingerprint")
	return m
}

func egernNode(n Node) Object {
	kind := n.Protocol
	if kind == "ss" {
		kind = "shadowsocks"
	}
	m := Object{"name": n.Name, "server": n.Server, "port": n.Port}
	if n.Password != "" {
		m["password"] = n.Password
	}
	if n.TLS.Enabled {
		m["sni"], m["skip_tls_verify"] = n.TLS.ServerName, n.TLS.Insecure
	}
	switch n.Protocol {
	case "vmess", "vless":
		m["user_id"] = n.UUID
		if n.Protocol == "vmess" {
			m["security"] = fallback(n.Cipher, "auto")
		}
		if n.Flow != "" {
			m["flow"] = n.Flow
		}
		tls := Object{"sni": n.TLS.ServerName, "skip_tls_verify": n.TLS.Insecure}
		if n.TLS.RealityKey != "" {
			tls["reality"] = Object{"public_key": n.TLS.RealityKey, "short_id": n.TLS.RealityShortID}
		}
		if n.Transport.Type == "ws" {
			tls["path"], tls["headers"] = n.Transport.Path, Object{"Host": n.Transport.Host}
			k := "ws"
			if n.TLS.Enabled {
				k = "wss"
			}
			m["transport"] = Object{k: tls}
		} else if n.TLS.Enabled {
			m["transport"] = Object{"tls": tls}
		}
		delete(m, "sni")
		delete(m, "skip_tls_verify")
	case "hysteria2":
		m["auth"] = n.Password
		delete(m, "password")
		if len(n.Ports) > 0 {
			m["port_hopping"] = strings.ReplaceAll(strings.Join(n.Ports, ","), ":", "-")
		}
	case "tuic":
		m["uuid"], m["alpn"] = n.UUID, n.TLS.ALPN
	case "ss":
		m["method"] = n.Cipher
	case "trojan":
		if n.Transport.Type == "ws" {
			m["websocket"] = Object{"path": n.Transport.Path, "host": n.Transport.Host}
		}
	}
	return Object{kind: m}
}

func egernTemplate(nodes []Node, native []Object, t Template) Object {
	normal, gateway := groups(nodes)
	policy := []Object{}
	if len(normal) > 0 {
		policy = append(policy, Object{"select": Object{"name": "PROXY", "policies": normal}})
	}
	if len(gateway) > 0 {
		policy = append(policy, Object{"select": Object{"name": "VPNGate", "policies": gateway}})
	}
	rules := []Object{}
	for _, r := range t.Rules {
		target := r.Target
		if target == "PROXY" {
			target = finalGroup(nodes)
		}
		key := map[string]string{"domain": "domain", "domain_suffix": "domain_suffix", "ip_cidr": "ip_cidr"}[r.Type]
		rules = append(rules, Object{key: Object{"match": r.Value, "policy": target}})
	}
	if t.Preset == "split" {
		rules = append(rules, Object{"geoip": Object{"match": "CN", "policy": "DIRECT"}})
	}
	rules = append(rules, Object{"default": Object{"policy": finalGroup(nodes)}})
	return Object{"proxies": native, "policy_groups": policy, "rules": rules}
}

func shadowrocketRules(t Template) string {
	var b strings.Builder
	b.WriteString("[General]\nbypass-system = true\n\n[Rule]\n")
	for _, r := range t.Rules {
		b.WriteString(textRule(r, r.Target))
		b.WriteByte('\n')
	}
	if t.Preset == "split" {
		b.WriteString("GEOIP,CN,DIRECT\n")
	}
	b.WriteString("FINAL,PROXY\n")
	return b.String()
}
