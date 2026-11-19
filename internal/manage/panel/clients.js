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
    { key: "id", label: "ID", required: true, advanced: true },
    { key: "name", label: "模板名称" },
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
      label: "默认 DNS",
      type: "csv",
      visible: (v) => complete(v) && capability(v)?.features.dns,
      help: "原生核心模板支持 IP 和 udp/tcp/tls/https URL；其他客户端使用 IP。",
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
      key: "final",
      label: "默认策略",
      type: "select",
      visible: complete,
      options: (v) => [
        ["", "自动选择普通 / VPNGate"],
        ["PROXY", "普通代理"],
        ["VPNGate", "VPNGate"],
        ...(v.groups || []).map((g) => [g.id, g.name || g.id]),
      ],
    },
    {
      key: "private_network",
      label: "局域网直连",
      type: "bool",
      visible: complete,
      help: "VPNGate 专用输出不允许启用；自定义规则仍优先执行。",
    },
    {
      key: "dns_policy.local",
      label: "国内 DNS",
      type: "csv",
      advanced: true,
      visible: (v) => complete(v) && capability(v)?.features.advanced_dns,
    },
    {
      key: "dns_policy.remote",
      label: "代理 DNS",
      type: "csv",
      advanced: true,
      visible: (v) => complete(v) && capability(v)?.features.advanced_dns,
      help: "sing-box 每种用途使用一个解析器。",
    },
    {
      key: "dns_policy.bootstrap",
      label: "引导 DNS（IP）",
      type: "csv",
      advanced: true,
      visible: (v) => complete(v) && capability(v)?.features.advanced_dns,
    },
    {
      key: "dns_policy.fake_ip",
      label: "Fake-IP",
      type: "bool",
      default: true,
      advanced: true,
      visible: (v) =>
        complete(v) && ["mihomo", "openclash", "stash"].includes(v.client),
    },
    {
      key: "dns_policy.fake_ip_filter",
      label: "Fake-IP 排除域名",
      type: "csv",
      advanced: true,
      visible: (v) =>
        complete(v) &&
        ["mihomo", "openclash", "stash"].includes(v.client) &&
        v.dns_policy?.fake_ip,
    },
    {
      key: "dns_policy.ipv6",
      label: "DNS 返回 IPv6",
      type: "bool",
      advanced: true,
      visible: (v) =>
        complete(v) && ["mihomo", "openclash", "stash"].includes(v.client),
    },
    {
      key: "native",
      label: "客户端原生基础字段 JSON",
      type: "json",
      wide: true,
      advanced: true,
      visible: (v) => complete(v) && capability(v)?.features.native_template,
      help: "补充客户端字段；节点、策略组、路由和 DNS 由模板编译器生成。",
    },
    {
      key: "rules",
      label: "高级路由规则 JSON",
      type: "json",
      default: [],
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
    delete value.dns_policy;
    delete value.groups;
    delete value.providers;
    delete value.native;
    delete value.final;
    delete value.private_network;
  }
  if (!features.dns) delete value.dns;
  if (!features.probe) delete value.probe_url;
  if (!features.tun) delete value.tun;
  if (!features.advanced_dns) delete value.dns_policy;
  if (!features.policy_groups) delete value.groups;
  if (!features.rule_providers) delete value.providers;
  if (!features.native_template) delete value.native;
  if (value.client === "sing-box" && value.dns_policy) {
    value.dns_policy.fake_ip = false;
    value.dns_policy.ipv6 = false;
    delete value.dns_policy.fake_ip_filter;
  }
  if (value.dns_policy && !value.dns_policy.fake_ip)
    delete value.dns_policy.fake_ip_filter;
  if (value.dns_policy && Object.keys(value.dns_policy).length === 0)
    delete value.dns_policy;
  if (value.mode !== "config" && value.mode !== "rules") {
    delete value.rules;
    delete value.rule_sets;
  }
  return value;
}
