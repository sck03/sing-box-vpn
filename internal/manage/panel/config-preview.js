import { $, el, row, button, help, badge, bytes } from "./dom.js";
import { table } from "./tables.js";
import { validationLabel } from "./validation.js";

export function showConfiguration(result, changed = [], options = {}) {
  const outputs = result.outputs || [],
    unavailable = outputs.filter((o) => !o.available).length;
  const dialog = $("#dialog");
  const marker = el("h2", {}, "配置校验与发布预览");
  let loading = 0;
  const showOutput = async (output) => {
    const generation = ++loading;
    const detail = await options.loadOutput(output.id);
    if (generation !== loading || !dialog.open || !dialog.contains(marker))
      return;
    dialog.replaceChildren(
      el("h2", {}, detail.name || detail.id),
      help(
        `已隐藏凭据${detail.preview.decoded ? "，Base64 已解码" : ""}${detail.preview.truncated ? "；仅显示前 64 KiB" : ""}。`,
      ),
      el("pre", { class: "export-content" }, detail.preview.content),
      row(
        button("返回发布预览", () =>
          showConfiguration(result, changed, options),
        ),
        button("关闭", () => dialog.close()),
      ),
    );
  };
  dialog.replaceChildren(
    marker,
    badge(
      unavailable ? `${unavailable} 个输出暂不可用` : "配置校验通过",
      unavailable ? "warn" : "good",
    ),
    help(
      unavailable
        ? "配置格式有效，部分来源尚无可用数据。应用后会继续刷新；请检查下方输出说明。"
        : "应用后会更新配置并按需重启受影响的实例。",
    ),
    ...(changed.length
      ? [
          el("h3", {}, "变更字段"),
          el(
            "ul",
            { class: "changed-fields" },
            changed.map((path) => el("li", {}, path)),
          ),
        ]
      : []),
    ...(outputs.length
      ? [
          table(
            [
              { key: "id", label: "输出" },
              { key: "name", label: "名称" },
              {
                label: "状态",
                value: (o) => (o.available ? "可发布" : "暂不可用"),
              },
              { label: "大小", value: (o) => bytes(o.bytes) },
              {
                label: "有效 / 筛选前",
                value: (o) => `${o.nodes ?? 0} / ${o.input_nodes ?? 0}`,
              },
              {
                label: "节点变化",
                value: (o) =>
                  `+${o.delta?.added ?? 0} / −${o.delta?.removed ?? 0}`,
              },
              { label: "验证", value: (o) => validationLabel(o.validation) },
              {
                label: "说明",
                value: (o) =>
                  (o.diagnostics || []).map((d) => d.message).join("；"),
              },
            ],
            outputs,
            options.loadOutput
              ? (output) =>
                  output.available
                    ? [button("查看配置", () => showOutput(output))]
                    : []
              : undefined,
          ),
        ]
      : []),
    el(
      "details",
      { class: "advanced" },
      el("summary", {}, "技术详情（已隐藏秘密）"),
      el(
        "pre",
        {},
        JSON.stringify(
          {
            ordinary_server: result.ordinary_server,
            diagnostics: result.diagnostics,
          },
          null,
          2,
        ),
      ),
    ),
    row(
      ...(options.applyDraft
        ? [
            button(
              "加入草稿",
              async () => {
                await options.applyDraft();
                dialog.close();
              },
              "primary",
            ),
          ]
        : []),
      button("关闭", () => dialog.close()),
    ),
  );
  dialog.showModal();
}
