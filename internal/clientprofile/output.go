package clientprofile

import "slices"

// Output identifies both the client syntax and its purpose. Grants authorize
// this identity, so a template cannot change another link's output semantics.
type Output struct {
	ID          string `json:"id"`
	Client      string `json:"client"`
	Mode        string `json:"mode"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

var outputs = func() map[string]Output {
	result := map[string]Output{}
	for client, profile := range catalog {
		for _, mode := range profile.Modes {
			if mode == "rules" || (mode == "nodes" && slices.Contains(profile.Modes, "provider")) {
				continue
			}
			id, label, description := client+"-"+mode, profile.Label, ""
			switch mode {
			case "nodes":
				label += " · 仅节点"
				description = "只包含节点；使用客户端自己的策略和 DNS。"
			case "provider":
				label += " · 节点集合"
				description = "只更新节点，需要在客户端配置中引用此 Provider。"
			case "config":
				label += " · 完整配置"
				description = "包含模板中的策略、规则和 DNS；导入前检查是否适合设备。"
			}
			if client == "uri" || client == "base64" {
				id, label = client, "通用节点 · URI 明文"
				if client == "base64" {
					label = "通用节点 · Base64"
				}
				description = "供支持节点链接订阅的客户端导入；Base64 是编码，不是加密。"
			}
			if client == "sing-box" && mode == "nodes" {
				description = "outbounds 节点片段；需要支持节点导入的客户端或与基础配置合并。"
			}
			result[id] = Output{id, client, mode, label, description}
		}
	}
	return result
}()

func OutputFor(id string) (Output, bool) { value, ok := outputs[id]; return value, ok }

func Outputs() []Output {
	result := make([]Output, 0, len(outputs))
	for _, value := range outputs {
		result = append(result, value)
	}
	slices.SortFunc(result, func(a, b Output) int {
		rank := func(o Output) int {
			if o.ID == "uri" {
				return 0
			}
			if o.ID == "base64" {
				return 1
			}
			if o.Mode != "config" {
				return 2
			}
			return 3
		}
		if d := rank(a) - rank(b); d != 0 {
			return d
		}
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return result
}
