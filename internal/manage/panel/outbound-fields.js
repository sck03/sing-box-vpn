const groups = ["selector", "urltest"];
const passwordProtocols = ["socks", "hysteria2", "tuic", "anytls"];
const tlsProtocols = ["vless", "vmess", "hysteria2", "tuic", "anytls"];
const uuidProtocols = ["vless", "vmess", "tuic"];
const isRemote = (v) => !["direct", ...groups].includes(v.type);
const tlsActive = (v) =>
  ["hysteria2", "tuic", "anytls"].includes(v.type) || v.tls?.enabled;

export function outboundFields(protocols, tags, resolvers) {
  const peers = (v) => tags.filter((tag) => tag !== v.tag);
  return [
    { key: "tag", label: "出站标识", required: true, section: "基本信息" },
    {
      key: "type",
      label: "类型",
      type: "select",
      options: ["direct", "socks", ...groups, ...protocols],
    },
    { key: "server", label: "服务器地址", required: true, visible: isRemote },
    {
      key: "server_port",
      label: "端口",
      type: "number",
      min: 1,
      max: 65535,
      required: true,
      visible: isRemote,
    },
    {
      key: "outbounds",
      label: "组成员",
      type: "multiselect",
      options: peers,
      required: true,
      wide: true,
      visible: (v) => groups.includes(v.type),
    },
    {
      key: "default",
      label: "默认成员",
      type: "select",
      options: (v) => [["", "第一个成员"], ...(v.outbounds || [])],
      visible: (v) => v.type === "selector",
    },
    {
      key: "url",
      label: "检测 URL",
      default: "https://www.gstatic.com/generate_204",
      visible: (v) => v.type === "urltest",
    },
    {
      key: "interval",
      label: "检测间隔",
      default: "3m",
      help: "例如 30s、3m。",
      visible: (v) => v.type === "urltest",
    },
    {
      key: "uuid",
      label: "UUID",
      required: true,
      visible: (v) => uuidProtocols.includes(v.type),
    },
    {
      key: "username",
      label: "SOCKS 用户名",
      visible: (v) => v.type === "socks",
    },
    {
      key: "password",
      label: "密码",
      type: "password",
      required: (v) => v.type !== "socks",
      visible: (v) => passwordProtocols.includes(v.type),
    },
    {
      key: "version",
      label: "SOCKS 版本",
      type: "select",
      options: ["5", "4", "4a"],
      default: "5",
      visible: (v) => v.type === "socks",
    },
    {
      key: "security",
      label: "VMess 加密",
      type: "select",
      options: ["auto", "aes-128-gcm", "chacha20-poly1305", "none"],
      default: "auto",
      visible: (v) => v.type === "vmess",
    },
    {
      key: "flow",
      label: "VLESS Flow",
      type: "select",
      options: ["", "xtls-rprx-vision"],
      visible: (v) => v.type === "vless",
    },
    {
      key: "tls.enabled",
      label: "启用 TLS",
      type: "bool",
      section: "连接安全",
      visible: (v) => ["vless", "vmess"].includes(v.type),
    },
    {
      key: "tls.server_name",
      label: "TLS SNI",
      visible: (v) => tlsProtocols.includes(v.type) && tlsActive(v),
    },
    {
      key: "tls.insecure",
      label: "允许不受信任的证书",
      type: "bool",
      help: "仅用于你明确了解证书来源的节点。",
      visible: (v) => tlsProtocols.includes(v.type) && tlsActive(v),
    },
    {
      key: "tls.alpn",
      label: "ALPN",
      type: "csv",
      help: "例如 h2、http/1.1；留空使用协议默认值。",
      visible: (v) => tlsProtocols.includes(v.type) && tlsActive(v),
    },
    {
      key: "tls.certificate_public_key_sha256",
      label: "证书公钥 SHA-256 固定值",
      type: "csv",
      wide: true,
      help: "Base64 编码的 SHA-256 公钥摘要；不是证书文件的十六进制摘要。",
      visible: (v) => tlsProtocols.includes(v.type) && tlsActive(v),
    },
    {
      key: "tls.reality.enabled",
      label: "使用 Reality",
      type: "bool",
      visible: (v) => v.type === "vless" && v.tls?.enabled,
    },
    {
      key: "tls.reality.public_key",
      label: "Reality 公钥",
      required: true,
      visible: (v) =>
        v.type === "vless" && v.tls?.enabled && v.tls?.reality?.enabled,
    },
    {
      key: "tls.reality.short_id",
      label: "Reality Short ID",
      visible: (v) =>
        v.type === "vless" && v.tls?.enabled && v.tls?.reality?.enabled,
    },
    {
      key: "tls.utls.enabled",
      label: "浏览器 TLS 指纹",
      type: "bool",
      visible: (v) => ["vless", "vmess"].includes(v.type) && v.tls?.enabled,
    },
    {
      key: "tls.utls.fingerprint",
      label: "指纹",
      type: "select",
      options: ["chrome", "firefox", "safari", "edge", "random"],
      default: "chrome",
      visible: (v) =>
        ["vless", "vmess"].includes(v.type) &&
        v.tls?.enabled &&
        v.tls?.utls?.enabled,
    },
    {
      key: "transport.type",
      label: "传输方式",
      section: "传输与路由",
      type: "select",
      options: [
        ["", "TCP"],
        ["ws", "WebSocket"],
        ["grpc", "gRPC"],
        ["httpupgrade", "HTTPUpgrade"],
      ],
      visible: (v) => ["vless", "vmess"].includes(v.type),
    },
    {
      key: "transport.service_name",
      label: "gRPC 服务名",
      visible: (v) =>
        ["vless", "vmess"].includes(v.type) && v.transport?.type === "grpc",
    },
    {
      key: "transport.path",
      label: "WebSocket 路径",
      default: "/",
      visible: (v) =>
        ["vless", "vmess"].includes(v.type) &&
        ["ws", "httpupgrade"].includes(v.transport?.type),
    },
    {
      key: "transport.headers.Host",
      label: "WebSocket Host",
      visible: (v) =>
        ["vless", "vmess"].includes(v.type) && v.transport?.type === "ws",
    },
    {
      key: "transport.host",
      label: "HTTPUpgrade Host",
      visible: (v) =>
        ["vless", "vmess"].includes(v.type) &&
        v.transport?.type === "httpupgrade",
    },
    {
      key: "congestion_control",
      label: "TUIC 拥塞控制",
      type: "select",
      options: ["bbr", "cubic", "new_reno"],
      default: "bbr",
      visible: (v) => v.type === "tuic",
    },
    {
      key: "server_ports",
      label: "Hysteria2 跳跃端口",
      type: "csv",
      help: "例如 31000:31100、31200:31200；留空使用固定端口。",
      visible: (v) => v.type === "hysteria2",
    },
    {
      key: "obfs.type",
      label: "Hysteria2 混淆",
      type: "select",
      options: [["", "关闭"], "salamander"],
      visible: (v) => v.type === "hysteria2",
    },
    {
      key: "obfs.password",
      label: "混淆密码",
      type: "password",
      required: true,
      visible: (v) => v.type === "hysteria2" && !!v.obfs?.type,
    },
    {
      key: "domain_resolver",
      label: "域名解析器",
      type: "select",
      options: [["", "使用默认解析器"], ...resolvers],
      visible: isRemote,
    },
    {
      key: "udp_relay_mode",
      label: "TUIC UDP 模式",
      type: "select",
      options: ["native", "quic"],
      default: "native",
      visible: (v) => v.type === "tuic",
    },
    ...[
      "idle_session_check_interval",
      "idle_session_timeout",
      "min_idle_session",
    ].map((key) => ({
      key,
      label: {
        idle_session_check_interval: "空闲会话检查间隔",
        idle_session_timeout: "空闲会话超时",
        min_idle_session: "最少空闲会话数",
      }[key],
      type: key === "min_idle_session" ? "number" : "text",
      nullable: key === "min_idle_session",
      min: 0,
      help:
        key === "min_idle_session"
          ? "留空使用核心默认值。"
          : "例如 30s；留空使用核心默认值。",
      visible: (v) => v.type === "anytls",
    })),
    {
      key: "detour",
      label: "链式上游",
      type: "select",
      options: (v) => [["", "直接连接"], ...peers(v)],
      visible: isRemote,
    },
  ];
}

export function normalizeOutbound(v, previous) {
  if (
    previous &&
    previous.transport?.type !== v.transport?.type &&
    v.transport
  ) {
    const { type, path, headers, service_name, host } = v.transport;
    v.transport =
      type === "ws"
        ? { type, path, headers }
        : type === "httpupgrade"
          ? { type, path, host }
          : { type, service_name };
  }
  if (!isRemote(v)) {
    delete v.server;
    delete v.server_port;
    delete v.detour;
    delete v.domain_resolver;
  }
  if (!groups.includes(v.type)) delete v.outbounds;
  if (v.type !== "selector" || !v.default) delete v.default;
  if (v.type !== "urltest") {
    delete v.url;
    delete v.interval;
  }
  if (!uuidProtocols.includes(v.type)) delete v.uuid;
  if (!passwordProtocols.includes(v.type)) delete v.password;
  if (v.type !== "socks") {
    delete v.username;
    delete v.version;
  }
  if (v.type !== "vmess") delete v.security;
  if (v.type !== "vless" || !v.flow) delete v.flow;
  if (v.type !== "tuic") {
    delete v.congestion_control;
    delete v.udp_relay_mode;
  }
  if (v.type !== "hysteria2") delete v.server_ports;
  if (v.type !== "hysteria2" || !v.obfs?.type) delete v.obfs;
  for (const key of [
    "idle_session_check_interval",
    "idle_session_timeout",
    "min_idle_session",
  ])
    if (v.type !== "anytls" || v[key] === "" || v[key] == null) delete v[key];
  if (["hysteria2", "tuic", "anytls"].includes(v.type)) {
    v.tls ??= {};
    v.tls.enabled = true;
  }
  if (!tlsProtocols.includes(v.type) || !v.tls?.enabled) delete v.tls;
  if (v.tls) {
    if (v.type !== "vless" || !v.tls.reality?.enabled) delete v.tls.reality;
    if (!["vless", "vmess"].includes(v.type) || !v.tls.utls?.enabled)
      delete v.tls.utls;
  }
  if (!["vless", "vmess"].includes(v.type) || !v.transport?.type)
    delete v.transport;
  if (v.flow && (!v.tls?.enabled || v.transport?.type))
    throw new Error("Vision 需要 TLS + TCP，请调整传输或关闭 Flow。");
  for (const key of ["detour", "domain_resolver"]) if (!v[key]) delete v[key];
  return v;
}
