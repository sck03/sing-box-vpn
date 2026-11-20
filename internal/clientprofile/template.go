package clientprofile

// NativeFields is shared by template validation and the editor. Extra native
// fields are passed through; managed sections cannot bypass the compiler.
type NativeFields struct {
	Defaults  []string `json:"defaults"`
	Managed   []string `json:"managed"`
	Forbidden []string `json:"forbidden"`
}

func TemplateFields(client string) NativeFields {
	switch client {
	case "sing-box":
		return NativeFields{[]string{"log"}, []string{"inbounds", "outbounds", "route", "dns"}, []string{"endpoints", "services"}}
	case "stash":
		return NativeFields{[]string{"mode", "log-level"}, []string{"proxies", "proxy-groups", "rules", "rule-providers", "dns"}, []string{"proxy-providers", "tun"}}
	case "mihomo", "openclash":
		return NativeFields{[]string{"mixed-port", "allow-lan", "mode", "log-level"}, []string{"proxies", "proxy-groups", "rules", "rule-providers", "dns", "tun"}, []string{"proxy-providers"}}
	case "egern":
		return NativeFields{[]string{}, []string{"proxies", "policy_groups", "rules"}, []string{"proxy_providers", "policy_group_providers", "rule_providers"}}
	default:
		return NativeFields{[]string{}, []string{}, []string{}}
	}
}
