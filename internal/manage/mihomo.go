package manage

import "strings"

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
	names := make([]string, len(nodes))
	for i, node := range nodes {
		names[i] = text(node["name"])
	}
	var groups []Object
	rules := []string{"IP-CIDR,127.0.0.0/8,DIRECT,no-resolve", "IP-CIDR,192.168.0.0/16,DIRECT,no-resolve"}
	if gateway {
		groups = []Object{{"name": "临时家宽", "type": "select", "proxies": names}}
		rules = append(rules, "MATCH,临时家宽")
	} else {
		choices := make([]string, 0, len(names)+2)
		choices = append(append(append(choices, "自动选择"), names...), "DIRECT")
		groups = []Object{{"name": "节点选择", "type": "select", "proxies": choices}, {"name": "自动选择", "type": "url-test", "proxies": names, "url": "https://www.gstatic.com/generate_204", "interval": 600}}
		rules = append(rules, "GEOIP,CN,DIRECT", "MATCH,节点选择")
	}
	return Object{"mixed-port": 7890, "allow-lan": false, "mode": "rule", "log-level": "info", "dns": Object{"enable": true, "ipv6": false, "enhanced-mode": "fake-ip", "fake-ip-range": "198.18.0.1/16", "respect-rules": true, "default-nameserver": []string{"223.5.5.5"}, "nameserver": []string{"https://1.1.1.1/dns-query"}, "proxy-server-nameserver": []string{"https://223.5.5.5/dns-query"}}, "proxies": nodes, "proxy-groups": groups, "rules": rules}
}
