export const capabilitiesFixture = {
  runtime: {
	 outbound_fields: {
	   vless: { tls: true, utls: true }, vmess: { tls: true, utls: true },
	   anytls: { tls: true, required_tls: true, utls: true },
	   hysteria2: { tls: true, required_tls: true }, tuic: { tls: true, required_tls: true },
	 },
    inbounds: ["vless", "vmess", "hysteria2", "tuic", "anytls"],
    outbounds: ["direct", "socks", "selector", "urltest", "vless", "vmess", "hysteria2", "tuic", "anytls"],
    dns: ["tcp", "udp", "tls", "https", "local", "resolved"],
    endpoints: ["wireguard"], services: ["resolved"],
  },
  protocols: ["vless", "vmess", "hysteria2", "tuic", "anytls"],
  countries: { JP: "日本", US: "美国" },
  password_bytes: { min: 8, max: 72 },
  gateway: {
    defaults: {
      mode: "sticky",
      residential_only: true,
      exclude_hosting: true,
      sources: ["https://www.vpngate.net/api/iphone/"],
      connect_timeout: 60,
      health_interval: 10,
      max_connections: 128,
    },
    limits: {
      connect_timeout: { min: 5, max: 180 },
      health_interval: { min: 1, max: 300 },
      max_connections: { min: 1, max: 1024 },
    },
  },
  service_actions: ["start", "stop", "restart", "enable", "disable"],
  services: [
    {
      name: "sb-core",
      label: "普通节点核心",
      description: "用户连接、认证与流量计量",
      page: "inbounds",
    },
    {
      name: "sb-vpngate-core",
      label: "VPNGate 节点核心",
      description: "用户连接、认证与流量计量",
      page: "vpngate",
      section: "access",
    },
    {
      name: "sb-vpngate-gateway",
      label: "VPNGate 隧道网关",
      description: "建立 OpenVPN 隧道并检查出口",
      page: "vpngate",
      section: "gateway",
    },
    {
      name: "sb-subscription",
      label: "订阅下载服务",
      description: "提供用户页与配置下载",
      page: "access",
      section: "subscription",
    },
    {
      name: "sb-argo",
      label: "Argo 隧道",
      description: "发布 VMess WebSocket 入口",
      page: "outbounds",
      section: "argo",
    },
  ],
};
