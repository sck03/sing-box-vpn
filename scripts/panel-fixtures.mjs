export const capabilitiesFixture = {
  protocols: ["vless", "vmess", "hysteria2", "tuic", "anytls"],
  countries: { JP: "日本", US: "美国" },
  password_bytes: { min: 12, max: 72 },
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
};
