// Package servicecatalog owns project service identities, runtime scopes and UI routes.
package servicecatalog

const (
	Core         = "sb-core"
	VPNCore      = "sb-vpngate-core"
	Gateway      = "sb-vpngate-gateway"
	Subscription = "sb-subscription"
	Argo         = "sb-argo"
	Panel        = "sb-panel"
	Journal      = "sb-vpn"
)

type Definition struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Page        string `json:"page"`
	Section     string `json:"section,omitempty"`
	Runtime     string `json:"runtime,omitempty"`
}

func Definitions() []Definition {
	return []Definition{
		{Name: Core, Label: "普通节点核心", Description: "用户接入、认证与流量计量", Page: "inbounds", Runtime: "normal"},
		{Name: VPNCore, Label: "VPNGate 接入核心", Description: "用户接入、认证与计量；通过出口网关转发", Page: "vpngate", Section: "access", Runtime: "vpngate"},
		{Name: Gateway, Label: "VPNGate 出口网关", Description: "OpenVPN 隧道、出口选择与验证", Page: "vpngate", Section: "gateway"},
		{Name: Subscription, Label: "订阅服务", Description: "独立用户页与配置下载；支持内置 HTTPS", Page: "access", Section: "subscription"},
		{Name: Argo, Label: "Cloudflare 隧道", Description: "将 VMess WebSocket 入口发布到 Cloudflare", Page: "outbounds", Section: "argo"},
	}
}

func Names() []string {
	result := make([]string, 0, 5)
	for _, definition := range Definitions() {
		result = append(result, definition.Name)
	}
	return result
}

func Lookup(name string) Definition {
	for _, definition := range Definitions() {
		if definition.Name == name {
			return definition
		}
	}
	return Definition{Name: name}
}

func RuntimeService(scope string) string {
	for _, definition := range Definitions() {
		if definition.Runtime == scope {
			return definition.Name
		}
	}
	return ""
}
