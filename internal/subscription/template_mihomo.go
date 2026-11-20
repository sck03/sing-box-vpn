package subscription

func clashTemplate(nodes []Node, native []Object, t Template) Object {
	group, rules, providers := clashPolicies(nodes, t)
	p := dnsPolicy(t)
	mode := "redir-host"
	if p.fakeIPEnabled() {
		mode = "fake-ip"
	}
	dns := Object{"enable": true, "respect-rules": true, "enhanced-mode": mode, "nameserver": p.Remote, "proxy-server-nameserver": p.Bootstrap, "default-nameserver": p.Bootstrap, "ipv6": p.IPv6}
	if p.fakeIPEnabled() {
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
