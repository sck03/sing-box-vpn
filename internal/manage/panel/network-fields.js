// Declarative field schemas keep configuration rules separate from page layout.
export const ruleFields = [
  { key: "domain_suffix", label: "域名后缀", type: "csv" },
  { key: "ip_cidr", label: "IP CIDR", type: "csv" },
  { key: "inbound", label: "入站 ID", type: "csv" },
  {
    key: "action",
    label: "动作",
    type: "select",
    options: ["route", "reject", "sniff", "resolve"],
  },
  { key: "outbound", label: "出站 tag", visible: (v) => v.action === "route" },
];
export function cleanRule(value) {
  for (const key of ["domain_suffix", "ip_cidr", "inbound", "outbound"])
    if (!value[key]?.length) delete value[key];
  if (value.action !== "route") delete value.outbound;
  return value;
}
export const routeDefaults = { auto_detect_interface: true };
export const routeFields = [
  { key: "final", label: "默认出站 tag", help: "没有规则匹配时使用的出站。" },
  { key: "auto_detect_interface", label: "自动检测出口网卡", type: "bool" },
  {
    key: "default_interface",
    label: "默认网卡",
    visible: (v) => !v.auto_detect_interface,
  },
];
export const dnsFields = [
  { key: "final", label: "默认 DNS tag" },
  {
    key: "strategy",
    label: "解析策略",
    type: "select",
    options: [
      ["", "默认"],
      "prefer_ipv4",
      "prefer_ipv6",
      "ipv4_only",
      "ipv6_only",
    ],
  },
  { key: "disable_cache", label: "禁用 DNS 缓存", type: "bool" },
  { key: "independent_cache", label: "不同解析器使用独立缓存", type: "bool" },
  { key: "disable_expire", label: "保留已过期缓存", type: "bool" },
];
export const vpnProfileFields = [
  ...["vless", "vmess", "hysteria2", "tuic", "anytls"].map((protocol) => ({
    key: "ports." + protocol,
    label: protocol + " 端口",
    type: "number",
    min: 1,
    max: 65535,
  })),
  { key: "reality_sni", label: "Reality SNI" },
  { key: "tls_sni", label: "TLS SNI" },
  { key: "vmess_tls", label: "VMess 启用 TLS", type: "bool" },
  { key: "ws_path", label: "WebSocket 路径" },
];
export const gatewayDefaults = {
  mode: "sticky",
  residential_only: true,
  exclude_hosting: true,
  sources: ["https://www.vpngate.net/api/iphone/"],
  connect_timeout: 60,
  health_interval: 10,
  max_connections: 128,
};
export const gatewayFields = [
  {
    key: "allow_asns",
    label: "允许的 ASN（逗号分隔，留空不限）",
    type: "numbers",
    wide: true,
  },
  {
    key: "mode",
    label: "选择策略",
    type: "select",
    options: [
      ["sticky", "优先保持当前出口"],
      ["fixed", "固定服务器"],
    ],
  },
  {
    key: "fixed_host",
    label: "固定服务器",
    visible: (v) => v.mode === "fixed",
    required: true,
  },
  {
    key: "sources",
    label: "节点来源 URL（逗号分隔）",
    type: "csv",
    wide: true,
    required: true,
  },
  { key: "residential_only", label: "筛选住宅候选", type: "bool" },
  { key: "exclude_hosting", label: "排除托管机房", type: "bool" },
  { key: "require_isp", label: "必须识别为 ISP", type: "bool" },
  {
    key: "connect_timeout",
    label: "连接超时（秒）",
    type: "number",
    min: 1,
    max: 300,
  },
  {
    key: "health_interval",
    label: "健康检查间隔（秒）",
    type: "number",
    min: 1,
    max: 300,
  },
  {
    key: "max_connections",
    label: "最大连接数",
    type: "number",
    min: 1,
    max: 65536,
  },
];
