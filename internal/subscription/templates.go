package subscription

import (
	"fmt"
	"strings"
)

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
	for _, g := range t.Groups {
		kind := g.Type
		if kind == "select" {
			kind = "selector"
		}
		entry := Object{"type": kind, "tag": g.ID, "outbounds": policyMembers(g, nodes)}
		if g.Type == "urltest" {
			endpoint, interval := policyDefaults(g)
			entry["url"], entry["interval"], entry["tolerance"] = endpoint, fmt.Sprintf("%ds", interval), g.Tolerance
		}
		native = append(native, entry)
		if g.Scope != "vpngate" && len(normal) > 0 {
			normal = append(normal, g.ID)
		} else if len(gateway) > 0 {
			gateway = append(gateway, g.ID)
		}
	}
	if len(normal) > 0 {
		native = append(native, Object{"type": "selector", "tag": "PROXY", "outbounds": normal})
	}
	if len(gateway) > 0 {
		native = append(native, Object{"type": "selector", "tag": "VPNGate", "outbounds": gateway})
	}
	if len(normal) > 0 {
		native = append(native, Object{"type": "direct", "tag": "DIRECT"})
	}
	final := templateFinal(nodes, t)
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
	}
	inbounds := []Object{{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": 7890}}
	for _, provider := range t.Providers {
		format := "source"
		if provider.Format == "srs" {
			format = "binary"
		}
		sets, _ := route["rule_set"].([]Object)
		route["rule_set"] = append(sets, Object{"type": "remote", "tag": provider.ID, "format": format, "url": provider.URL})
		target := provider.Target
		if target == "PROXY" {
			target = final
		}
		rules, _ := route["rules"].([]Object)
		rule := Object{"rule_set": provider.ID, "action": "route", "outbound": target}
		if target == "REJECT" {
			rule = Object{"rule_set": provider.ID, "action": "reject"}
		}
		route["rules"] = append(rules, rule)
	}
	if t.PrivateNetwork {
		current, _ := route["rules"].([]Object)
		route["rules"] = append(current, Object{"ip_is_private": true, "action": "route", "outbound": "DIRECT"}, Object{"domain_suffix": []string{"lan", "local"}, "action": "route", "outbound": "DIRECT"})
	}
	if t.Preset == "split" {
		current, _ := route["rules"].([]Object)
		route["rules"] = append(current, Object{"rule_set": []string{"cn-domains", "cn-ips"}, "action": "route", "outbound": "DIRECT"})
	}
	if t.TUN {
		inbounds = append(inbounds, Object{"type": "tun", "tag": "tun-in", "address": []string{"172.19.0.1/30"}, "auto_route": true, "strict_route": true, "mtu": 1420})
	}
	p := dnsPolicy(t)
	dns := Object{"servers": []Object{singDNS(p.Bootstrap[0], "bootstrap", ""), singDNS(p.Remote[0], "remote", final), singDNS(p.Local[0], "local", "")}, "final": "remote"}
	dnsRules := []Object{}
	if t.PrivateNetwork {
		dnsRules = append(dnsRules, Object{"domain_suffix": []string{"lan", "local"}, "action": "route", "server": "local"})
	}
	if t.Preset == "split" {
		dnsRules = append(dnsRules, Object{"rule_set": "cn-domains", "action": "route", "server": "local"})
	}
	if len(dnsRules) > 0 {
		dns["rules"] = dnsRules
	}
	return applyNativeTemplate(t.Native, Object{"log": Object{"level": "warn"}, "inbounds": inbounds, "outbounds": native, "route": route, "dns": dns})
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
	for _, g := range t.Groups {
		kind := g.Type
		if kind == "urltest" {
			kind = "url-test"
		}
		entry := Object{"name": g.ID, "type": kind, "proxies": policyMembers(g, nodes)}
		if g.Type != "select" {
			endpoint, interval := policyDefaults(g)
			entry["url"], entry["interval"], entry["tolerance"] = endpoint, interval, g.Tolerance
		}
		group = append(group, entry)
		if g.Scope != "vpngate" && len(normal) > 0 {
			members := group[0]["proxies"].([]string)
			group[0]["proxies"] = append([]string{g.ID}, members...)
		} else if len(gateway) > 0 {
			index := 0
			if len(normal) > 0 {
				index = 1
			}
			members := group[index]["proxies"].([]string)
			group[index]["proxies"] = append(members, g.ID)
		}
	}
	rules := []string{}
	for _, r := range t.Rules {
		target := r.Target
		if target == "PROXY" {
			target = templateFinal(nodes, t)
		}
		rules = append(rules, textRule(r, target))
	}
	providers := Object{}
	for _, p := range t.Providers {
		providers[p.ID] = Object{"type": "http", "behavior": strings.ReplaceAll(p.Type, "ip_cidr", "ipcidr"), "format": p.Format, "url": p.URL, "path": "./rules/" + p.ID + "." + p.Format, "interval": 86400}
		target := p.Target
		if target == "PROXY" {
			target = templateFinal(nodes, t)
		}
		rules = append(rules, "RULE-SET,"+p.ID+","+target)
	}
	if t.PrivateNetwork {
		for _, cidr := range privateCIDRs {
			rules = append(rules, textRule(Rule{Type: "ip_cidr", Value: cidr}, "DIRECT")+",no-resolve")
		}
		rules = append(rules, "DOMAIN-SUFFIX,lan,DIRECT", "DOMAIN-SUFFIX,local,DIRECT")
	}
	if t.Preset == "split" {
		for _, kind := range []string{"domains", "ips"} {
			id, behavior, category := "cn-"+kind, "domain", "geosite"
			if kind == "ips" {
				behavior, category = "ipcidr", "geoip"
			}
			providers[id] = Object{"type": "http", "behavior": behavior, "format": "mrs", "url": "https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@meta/geo/" + category + "/cn.mrs", "path": "./rules/" + id + ".mrs", "interval": 86400}
			rules = append(rules, "RULE-SET,"+id+",DIRECT")
		}
	}
	rules = append(rules, "MATCH,"+templateFinal(nodes, t))
	p := dnsPolicy(t)
	mode := "redir-host"
	if p.FakeIP {
		mode = "fake-ip"
	}
	dns := Object{"enable": true, "respect-rules": true, "enhanced-mode": mode, "nameserver": p.Remote, "proxy-server-nameserver": p.Bootstrap, "default-nameserver": p.Bootstrap, "ipv6": p.IPv6}
	if p.FakeIP {
		dns["fake-ip-filter"] = p.FakeIPFilter
	}
	policy := Object{}
	if t.Preset == "split" {
		policy["rule-set:cn-domains"] = p.Local
	}
	if t.PrivateNetwork {
		policy["+.lan"], policy["+.local"] = p.Local, p.Local
	}
	if len(policy) > 0 {
		dns["nameserver-policy"] = policy
	}
	result := Object{"mixed-port": 7890, "allow-lan": false, "mode": "rule", "log-level": "warning", "proxies": native, "proxy-groups": group, "rules": rules, "dns": dns}
	if len(providers) > 0 {
		result["rule-providers"] = providers
	}
	if t.TUN {
		result["tun"] = Object{"enable": true, "stack": "mixed", "auto-route": true, "auto-detect-interface": true, "dns-hijack": []string{"any:53"}}
	}
	return applyNativeTemplate(t.Native, result)
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
		if n.Cipher == "chacha20-ietf-poly1305" {
			m["method"] = "chacha20-poly1305"
		}
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
			target = templateFinal(nodes, t)
		}
		key := map[string]string{"domain": "domain", "domain_suffix": "domain_suffix", "ip_cidr": "ip_cidr"}[r.Type]
		rules = append(rules, Object{key: Object{"match": r.Value, "policy": target}})
	}
	if t.PrivateNetwork {
		for _, cidr := range privateCIDRs {
			rules = append(rules, Object{"ip_cidr": Object{"match": cidr, "policy": "DIRECT"}})
		}
	}
	if t.Preset == "split" {
		rules = append(rules, Object{"geoip": Object{"match": "CN", "policy": "DIRECT"}})
	}
	rules = append(rules, Object{"default": Object{"policy": templateFinal(nodes, t)}})
	return applyNativeTemplate(t.Native, Object{"proxies": native, "policy_groups": policy, "rules": rules})
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
