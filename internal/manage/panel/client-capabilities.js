import { $, el, help, row, button, table, badge } from "./ui.js";

export function verificationLabel(profile) {
  if (profile.tested_version) return `发行测试版本 · ${profile.tested_version}`;
  if (profile.verification?.includes("parser-roundtrip")) return "解析往返校验";
  if (profile.verification?.includes("uri-roundtrip"))
    return "URI 往返 · 应用待验收";
  if (profile.verification?.includes("mihomo-core"))
    return "CI 核心测试 · 路由器待验收";
  return "实验适配 · 应用待验收";
}

export function showClientCapabilities(profile) {
  const dialog = $("#dialog");
  const protocols = Object.entries(profile.connections || {}).map(
    ([protocol, connection]) => ({ protocol, ...connection }),
  );
  dialog.replaceChildren(
    el("h2", {}, `${profile.label} · 支持范围`),
    row(
      badge(verificationLabel(profile), profile.experimental ? "warn" : "good"),
      badge(profile.platforms?.join(" / ") || "通用平台"),
    ),
    help(
      "以下是当前转换器的字段范围；应用是否接受配置，仍以对应客户端验收为准。",
    ),
    help(
      `端口跳跃：${profile.features?.port_hopping ? "支持" : "不支持"}；自定义跳跃间隔：${profile.features?.hop_interval ? "支持" : "不支持"}。不支持的参数会在转换时给出诊断。`,
    ),
    ...(profile.limitations || []).map(help),
    table(
      [
        { key: "protocol", label: "协议" },
        { label: "传输", value: (v) => v.transports.join(" / ") },
        {
          label: "加密方式",
          value: (v) => v.ciphers?.join(" / ") || "协议固定",
        },
        { label: "插件", value: (v) => v.plugins?.join(" / ") || "—" },
        { label: "gRPC", value: (v) => v.grpc_modes?.join(" / ") || "—" },
        { label: "WS early-data", value: (v) => (v.early_data ? "支持" : "—") },
        { label: "Reality", value: (v) => (v.reality ? "支持" : "—") },
        { label: "TLS 指纹", value: (v) => (v.utls ? "支持" : "—") },
        { label: "XHTTP 扩展", value: (v) => (v.xhttp_extra ? "支持" : "—") },
      ],
      protocols,
    ),
    ...(profile.features?.native_template
      ? [
          el("h3", {}, "原生模板字段"),
          help(
            `可覆盖默认值：${profile.native_fields?.defaults?.join("、") || "无"}。`,
          ),
          help(
            `编译器管理：${profile.native_fields?.managed?.join("、") || "无"}。`,
          ),
          help(
            `禁止注入：${profile.native_fields?.forbidden?.join("、") || "无"}。其他原生字段由目标客户端校验。`,
          ),
        ]
      : []),
    row(button("关闭", () => dialog.close())),
  );
  dialog.showModal();
}
