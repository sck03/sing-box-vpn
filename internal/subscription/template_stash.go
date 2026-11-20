package subscription

// Stash shares proxy groups and rule syntax, but owns its DNS and device
// settings. Do not route this through the Mihomo complete-config renderer.
func stashTemplate(nodes []Node, native []Object, t Template) Object {
	groups, rules, providers := clashPolicies(nodes, t)
	p := dnsPolicy(t)
	dns := Object{"follow-rule": true, "nameserver": p.Remote, "default-nameserver": p.Bootstrap, "proxy-server-nameserver": p.Bootstrap, "ipv6": p.IPv6, "fake-ip-filter": p.FakeIPFilter}
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
	result := Object{"mode": "rule", "log-level": "warning", "proxies": native, "proxy-groups": groups, "rules": rules, "dns": dns}
	if len(providers) > 0 {
		result["rule-providers"] = providers
	}
	return applyNativeTemplate(t.Native, result)
}
