import { $, el, help, row, button, table } from "./ui.js";

export function showClientCapabilities(profile) {
  const dialog = $("#dialog");
  const protocols = Object.entries(profile.connections || {}).map(
    ([protocol, connection]) => ({ protocol, ...connection }),
  );
  dialog.replaceChildren(
    el("h2", {}, `${profile.label} · 支持范围`),
    help(
      "以下是当前转换器的字段范围；应用是否接受配置，仍以对应客户端验收为准。",
    ),
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
      ],
      protocols,
    ),
    row(button("关闭", () => dialog.close())),
  );
  dialog.showModal();
}
