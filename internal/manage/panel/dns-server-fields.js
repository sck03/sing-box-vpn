const remote = (value) => !["local", "resolved"].includes(value.type);
const encrypted = (value) => ["tls", "https"].includes(value.type);

// An omitted port follows the core's transport default; explicit ports are
// user choices and must survive editing or switching transport.
export function dnsServerFields(types, servers) {
  return [
    { key: "tag", label: "Tag", required: true },
    { key: "type", label: "传输", type: "select", options: types },
    { key: "server", label: "服务器", required: true, visible: remote },
    { key: "server_port", label: "端口", type: "number", min: 1, max: 65535,
      visible: remote, help: "留空使用协议默认端口：UDP / TCP 53、TLS 853、HTTPS 443。自定义端口在切换协议时保留。" },
    { key: "path", label: "DoH 路径", default: "/dns-query", visible: (v) => v.type === "https" },
    { key: "domain_resolver", label: "服务器域名解析器", type: "resolver", visible: remote,
      options: (v) => [["", "默认解析器"], ...servers.filter((s) => s.tag !== v.tag).map((s) => s.tag)] },
    { key: "detour", label: "出站 tag（可留空）", visible: remote },
    { key: "tls.server_name", label: "TLS 服务器名（SNI）", advanced: true, visible: encrypted,
      help: "使用 IP 连接而证书签发给域名时填写；留空沿用服务器地址。" },
    { key: "tls.certificate_path", label: "自定义 CA 证书路径", advanced: true, visible: encrypted,
      help: "服务器上的 PEM 路径；留空使用系统信任证书。其他 TLS 参数可在 JSON 中编辑。" },
  ];
}

export function normalizeDNSServer(value) {
  for (const key of ["server", "server_port", "detour", "domain_resolver", "path"])
    if (!value[key]) delete value[key];
  if (!remote(value))
    for (const key of ["server", "server_port", "detour", "domain_resolver"]) delete value[key];
  if (value.type !== "https") delete value.path;
  if (!encrypted(value)) delete value.tls;
  else if (value.tls) {
    for (const key of ["server_name", "certificate_path"])
      if (!value.tls[key]) delete value.tls[key];
    if (!Object.keys(value.tls).length) delete value.tls;
  }
  return value;
}
