export const capabilitiesFixture = {
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
    { name: "sing-box", label: "普通节点核心", description: "用户连接、认证与流量计量", page: "inbounds" },
    { name: "vpngate-core", label: "VPNGate 节点核心", description: "用户连接、认证与流量计量", page: "vpngate" },
    { name: "vpngate", label: "VPNGate 隧道网关", description: "建立 OpenVPN 隧道并检查出口", page: "vpngate" },
    { name: "subscription", label: "订阅下载服务", description: "提供用户页与配置下载", page: "access" },
    { name: "argo", label: "Argo 隧道", description: "发布 VMess WebSocket 入口", page: "outbounds" },
  ],
};
