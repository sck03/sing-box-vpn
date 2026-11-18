package manage

// Service definitions connect host lifecycle to its configuration workspace.
// The frontend consumes these descriptors instead of maintaining a second list.
type serviceDefinition struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Page        string `json:"page"`
	Runtime     string `json:"runtime,omitempty"`
}

var serviceDefinitions = []serviceDefinition{
	{Name: "sing-box", Label: "普通节点核心", Description: "用户连接、认证与流量计量", Page: "inbounds", Runtime: "normal"},
	{Name: "vpngate-core", Label: "VPNGate 节点核心", Description: "用户连接、认证与流量计量；经 VPNGate 网关转发", Page: "vpngate", Runtime: "vpngate"},
	{Name: "vpngate", Label: "VPNGate 隧道网关", Description: "建立 OpenVPN 隧道、选择出口并检查出口可达性", Page: "vpngate"},
	{Name: "subscription", Label: "订阅下载服务", Description: "提供用户页及配置下载；启用后使用独立下载监听", Page: "access"},
	{Name: "argo", Label: "Argo 隧道", Description: "将 VMess WebSocket 入口发布到 Cloudflare 隧道", Page: "outbounds"},
}

var platformServices = func() []string {
	names := make([]string, len(serviceDefinitions))
	for i, service := range serviceDefinitions {
		names[i] = service.Name
	}
	return names
}()

func serviceDetails(name string) serviceDefinition {
	for _, service := range serviceDefinitions {
		if service.Name == name {
			return service
		}
	}
	return serviceDefinition{Name: name}
}
