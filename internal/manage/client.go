package manage

// ClientConfig builds only the rules and selectors used by this scope. VPNGate
// deliberately has no public direct fallback or automatic exit selection.
func ClientConfig(nodes []Object, gateway bool) Object {
	names := make([]string, len(nodes))
	for i, node := range nodes {
		names[i] = text(node["tag"])
	}
	outbounds := make([]Object, 0, len(nodes)+3)
	outbounds = append(outbounds, nodes...)
	selector := Object{"type": "selector", "tag": "proxy", "outbounds": names}
	dnsRules := make([]Object, 0, 2)
	rules := make([]Object, 0, 5)
	rules = append(rules, Object{"port": 53, "action": "hijack-dns"})
	if !gateway {
		outbounds = append(outbounds, Object{"type": "urltest", "tag": "auto", "outbounds": names, "url": "https://www.gstatic.com/generate_204", "interval": "10m", "tolerance": 30})
		selector["outbounds"], selector["default"] = append([]string{"auto"}, names...), "auto"
		dnsRules = append(dnsRules, Object{"rule_set": []string{"cn-domains"}, "action": "route", "server": "dns-cn"})
		rules = append(rules, Object{"clash_mode": "Global", "action": "route", "outbound": "proxy"}, Object{"clash_mode": "Direct", "action": "route", "outbound": "direct"})
	}
	dnsRules = append(dnsRules, Object{"query_type": []string{"A", "AAAA"}, "action": "route", "server": "fakeip"})
	outbounds = append(outbounds, selector, Object{"type": "direct", "tag": "direct"})
	rules = append(rules, Object{"ip_is_private": true, "action": "route", "outbound": "direct"})
	route := Object{"default_domain_resolver": "dns-cn", "auto_detect_interface": true, "final": "proxy"}
	result := Object{
		"log": Object{"level": "info"},
		"dns": Object{
			"servers": []Object{
				{"type": "fakeip", "tag": "fakeip", "inet4_range": "198.18.0.0/15"},
				{"type": "udp", "tag": "dns-cn", "server": "223.5.5.5"},
				{"type": "https", "tag": "dns-proxy", "server": "1.1.1.1", "detour": "proxy"},
			},
			"rules": dnsRules, "final": "dns-proxy", "strategy": "ipv4_only", "reverse_mapping": true,
		},
		"inbounds":     []Object{{"type": "tun", "tag": "tun-in", "address": []string{"172.19.0.1/30"}, "auto_route": true, "strict_route": true, "mtu": 1420}},
		"route":        route,
		"experimental": Object{"cache_file": Object{"enabled": true, "store_dns": true}, "clash_api": Object{"external_controller": "127.0.0.1:9090", "default_mode": "Rule"}},
		"outbounds":    outbounds,
	}
	if !gateway {
		result["http_clients"] = []Object{{"tag": "rule-set-direct"}}
		route["default_http_client"] = "rule-set-direct"
		route["rule_set"] = []Object{
			{"type": "remote", "tag": "cn-domains", "format": "binary", "url": "https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/cn.srs"},
			{"type": "remote", "tag": "cn-ips", "format": "binary", "url": "https://cdn.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geoip/cn.srs"},
		}
		rules = append(rules, Object{"rule_set": []string{"cn-domains", "cn-ips"}, "action": "route", "outbound": "direct"})
	}
	route["rules"] = rules
	return result
}
