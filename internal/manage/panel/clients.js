import { api } from "./api.js";

export const modeLabels = {
  config: "完整配置",
  nodes: "节点列表",
  provider: "节点 Provider",
  rules: "规则列表",
};

export async function clientCatalog(request = api) {
  return request("/api/clients");
}

export function clientOptions(catalog) {
  return Object.entries(catalog).map(([id, profile]) => [
    id,
    profile.label + (profile.experimental ? " · 实验适配" : ""),
  ]);
}

export function templateFields(catalog) {
  const capability = (value) => catalog[value.client];
  const complete = (value) => value.mode === "config";
  const rules = (value) => complete(value) || value.mode === "rules";
  return [
    { key: "id", label: "ID", required: true },
    {
      key: "url",
      label: "远程模板 URL（可留空）",
      type: "url",
      wide: true,
      help: "使用声明式 JSON 模板；刷新来源时同步。下面的字段作为本地初始值。",
    },
    {
      key: "client",
      label: "客户端",
      type: "select",
      options: clientOptions(catalog),
    },
    {
      key: "mode",
      label: "模式",
      type: "select",
      options: (value) =>
        (capability(value)?.modes || []).map((id) => [id, modeLabels[id]]),
    },
    {
      key: "preset",
      label: "路由预设",
      type: "select",
      options: [
        ["proxy", "全部代理"],
        ["split", "国内直连"],
      ],
      visible: complete,
    },
    {
      key: "dns",
      label: "DNS 服务器 IP",
      type: "csv",
      visible: (v) => complete(v) && capability(v)?.features.dns,
    },
    {
      key: "probe_url",
      label: "自动测速 URL",
      visible: (v) => complete(v) && capability(v)?.features.probe,
    },
    {
      key: "tun",
      label: "启用 TUN",
      type: "bool",
      visible: (v) => complete(v) && capability(v)?.features.tun,
    },
    { key: "rule_sets", label: "规则集 ID", type: "csv", visible: rules },
    {
      key: "rules",
      label: "高级路由规则 JSON",
      type: "json",
      wide: true,
      visible: rules,
    },
  ];
}

export function normalizeTemplate(value, catalog) {
  const features = catalog[value.client]?.features || {};
  if (value.mode !== "config") {
    delete value.preset;
    delete value.dns;
    delete value.probe_url;
    delete value.tun;
  }
  if (!features.dns) delete value.dns;
  if (!features.probe) delete value.probe_url;
  if (!features.tun) delete value.tun;
  if (value.mode !== "config" && value.mode !== "rules") {
    delete value.rules;
    delete value.rule_sets;
  }
  return value;
}
