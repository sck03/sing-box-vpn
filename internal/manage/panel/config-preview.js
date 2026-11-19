import { el, row, button, help, badge, bytes } from "./dom.js";
import { table } from "./tables.js";
import { validationLabel } from "./validation.js";
import { dialogSession } from "./dialog-session.js";

export async function requestConfiguration(load, changed = [], options = {}) {
  const session = dialogSession(options.signal);
  session.replace(
    el("h2", {}, "正在生成配置预览"),
    el("progress", { class: "preview-progress", "aria-label": "编译进度" }),
    help("正在编译和校验所选输出…"),
    row(button("取消", session.close)),
  );
  try {
    const result = await load(session.signal);
    if (session.active())
      showConfiguration(result, changed, { ...options, session });
  } catch (error) {
    if (session.active() && error.name !== "AbortError")
      session.replace(
        el("h2", {}, "配置预览失败"),
        help(error.message),
        row(button("关闭", session.close)),
      );
    else session.close();
    throw error;
  }
}

export function showConfiguration(result, changed = [], options = {}) {
  if (options.signal?.aborted) return;
  const session = options.session || dialogSession(options.signal);
  options = { ...options, session };
  const outputs = result.outputs || [],
    unavailable = outputs.filter((o) => !o.available).length;
  const corePassed = outputs.filter(
    (o) => o.validation?.state === "passed" && o.validation.scope === "core-config",
  ).length;
  const needsReview = outputs.filter(
    (o) => (o.diagnostics || []).length > 0 || o.validation?.state === "unavailable",
  ).length;
  const marker = el("h2", {}, "配置校验与发布预览");
  let loading = 0;
  let detailRequest;
  const showOutput = async (output) => {
    const generation = ++loading;
    detailRequest?.abort();
    detailRequest = new AbortController();
    const detail = await options.loadOutput(
      output.id,
      AbortSignal.any([session.signal, detailRequest.signal]),
    );
    if (generation !== loading || !session.active()) return;
    session.replace(
      el("h2", {}, detail.name || detail.id),
      help(
        `已隐藏凭据${detail.preview.decoded ? "，Base64 已解码" : ""}${detail.preview.truncated ? "；仅显示前 64 KiB" : ""}。`,
      ),
      el("pre", { class: "export-content" }, detail.preview.content),
      row(
        button("返回发布预览", () =>
          showConfiguration(result, changed, options),
        ),
        button("关闭", session.close),
      ),
    );
  };
  session.replace(
    marker,
    badge(
      unavailable ? `${unavailable} 个输出暂不可用` : "配置预览已生成",
      unavailable ? "warn" : "good",
    ),
    help(
      unavailable
        ? "配置格式有效，部分来源尚无可用数据。应用后会继续刷新；请检查下方输出说明。"
        : "应用后会更新配置并按需重启受影响的实例。",
    ),
    ...(outputs.length
      ? [el(
          "div",
          { class: "preview-summary", "aria-label": "输出概览" },
          [
            ["可发布输出", outputs.length - unavailable],
            ["核心校验通过", corePassed],
            ["需要关注", needsReview],
            ["暂不可用", unavailable],
          ].map(([label, value]) =>
            el("div", {}, el("span", {}, label), el("strong", {}, value)),
          ),
        )]
      : []),
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
                session.close();
              },
              "primary",
            ),
          ]
        : []),
      button("关闭", session.close),
    ),
  );
}
