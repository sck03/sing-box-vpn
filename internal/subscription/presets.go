package subscription

import (
	project "github.com/sck03/sing-box-vpn"
	"slices"

	"github.com/sck03/sing-box-vpn/internal/clientprofile"
)

type Preset struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Templates   map[string]Template `json:"templates"`
}

func Presets() []Preset {
	result := []Preset{
		{ID: "nodes", Name: "仅节点（推荐）", Description: "只输出连接信息，使用客户端自己的策略和 DNS；节点片段需由客户端导入或引用。"},
		{ID: "proxy", Name: "全局代理", Description: "完整配置使用代理出口；通用格式提供节点列表。"},
		{ID: "split", Name: "国内直连", Description: "国内规则和局域网直连，其他请求使用代理。"},
		{ID: "mobile", Name: "移动端 TUN", Description: "启用 TUN，使用代理 DNS，按需测试节点。"},
		{ID: "automatic", Name: "自动选择", Description: "每 15 分钟检查节点，使用 80 毫秒切换容差。"},
		{ID: "regional", Name: "地区选择", Description: "按名称建立香港、日本、新加坡和美国策略组，空地区自动省略。"},
		{ID: "provider", Name: "路由器 Provider", Description: "只更新节点，保留 OpenClash 或客户端自己的路由配置。"},
		{ID: "vpngate", Name: "VPNGate 专用", Description: "使用受管出口，不添加国内或局域网直连规则。"},
	}
	for i := range result {
		p := &result[i]
		p.Templates = map[string]Template{}
		for client, profile := range clientprofile.All() {
			t := Template{ID: "preset-" + p.ID, Name: p.Name, Client: client, Version: 1, Mode: profile.AccountMode, Preset: "proxy"}
			switch p.ID {
			case "nodes":
				if slices.Contains(profile.Modes, "provider") {
					t.Mode = "provider"
				} else if slices.Contains(profile.Modes, "nodes") {
					t.Mode = "nodes"
				} else {
					continue
				}
			case "split":
				if !slices.Contains(profile.Modes, "config") {
					continue
				}
				t.Mode, t.Preset, t.PrivateNetwork = "config", "split", true
			case "mobile":
				if !profile.Features.TUN {
					continue
				}
				t.Mode, t.TUN = "config", true
			case "automatic":
				if !profile.Features.PolicyGroups {
					continue
				}
				t.Mode = "config"
				t.Final = "automatic"
				t.Groups = []PolicyGroup{{ID: "automatic", Name: "自动选择", Type: "urltest", IntervalSeconds: 900, Tolerance: 80}}
			case "regional":
				if !profile.Features.PolicyGroups {
					continue
				}
				t.Mode = "config"
				for _, region := range []struct{ id, name, pattern string }{{"hong-kong", "香港", "(?i)香港|Hong.?Kong|\\bHK\\b|🇭🇰"}, {"japan", "日本", "(?i)日本|Japan|\\bJP\\b|🇯🇵"}, {"singapore", "新加坡", "(?i)新加坡|Singapore|\\bSG\\b|🇸🇬"}, {"united-states", "美国", "(?i)美国|United.?States|\\bUS\\b|🇺🇸"}} {
					t.Groups = append(t.Groups, PolicyGroup{ID: region.id, Name: region.name, Type: "urltest", Include: region.pattern, Optional: true, IntervalSeconds: 900, Tolerance: 80})
				}
			case "provider":
				if !slices.Contains(profile.Modes, "provider") {
					continue
				}
				t.Mode = "provider"
			}
			t.Origin = &TemplateOrigin{Format: "builtin", Revision: project.Versions.Release}
			if client == "sing-box" {
				t.Origin.SourceURL = "https://sing-box.sagernet.org/configuration/"
			}
			if client == "mihomo" {
				t.Origin.SourceURL = "https://wiki.metacubex.one/config/"
			}
			p.Templates[client] = t
		}
	}
	return result
}

func templateFinal(nodes []Node, t Template) string {
	if t.Final != "" && t.Final != "PROXY" {
		return t.Final
	}
	return finalGroup(nodes)
}

var privateCIDRs = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8", "169.254.0.0/16", "::1/128", "fc00::/7", "fe80::/10"}
