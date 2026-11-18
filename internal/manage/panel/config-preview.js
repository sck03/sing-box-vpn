import { $, el, row, button, help, badge, bytes } from "./dom.js";
import { table } from "./tables.js";

export function showConfiguration(result, changed = []) {
  const outputs = result.outputs || [],
    unavailable = outputs.filter((o) => !o.available).length;
  const dialog = $("#dialog");
  dialog.replaceChildren(
    el("h2", {}, "配置校验与发布预览"),
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
                label: "说明",
                value: (o) =>
                  (o.diagnostics || []).map((d) => d.message).join("；"),
              },
            ],
            outputs,
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
    row(button("关闭", () => dialog.close())),
  );
  dialog.showModal();
}
