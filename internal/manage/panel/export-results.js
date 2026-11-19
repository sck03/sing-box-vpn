import { api, download } from "./api.js";
import { validationLabel } from "./validation.js";
import { $, el, button, row, help } from "./dom.js";

export function showExport(result, client) {
  const dialog = $("#dialog"),
    content = result.content;
  const output = el("pre", { class: "export-content" }, content);
  const controls = row(
    button("复制配置", async () => {
      try {
        await navigator.clipboard.writeText(content);
      } catch {
        const input = el("textarea", { "aria-label": "复制配置内容" });
        input.value = content;
        output.replaceChildren(input);
        input.focus();
        input.select();
      }
    }),
    button("下载文件", () =>
      download(
        new Blob([content], { type: result.mime || "text/plain" }),
        `account-${client}.${result.extension}`,
      ),
    ),
  );
  const qr = el("div", { class: "qr-preview" });
  if (client === "uri") {
    const links = content.trim().split("\n").filter(Boolean);
    const choice = el(
      "select",
      { "aria-label": "选择二维码节点" },
      links.map((_, i) => el("option", { value: i }, `节点 ${i + 1}`)),
    );
    const error = el("p", { role: "status" });
    controls.append(
      choice,
      button("显示二维码", async () => {
        error.textContent = "";
        try {
          const image = await api("/api/qr", {
            content: links[Number(choice.value)],
          });
          if (!dialog.open || !dialog.contains(output)) return;
          qr.replaceChildren(
            el("img", {
              src: image.image,
              alt: "所选节点二维码",
              width: 240,
              height: 240,
            }),
          );
        } catch (failure) {
          if (dialog.contains(output)) error.textContent = failure.message;
        }
      }),
    );
    qr.append(error);
  }
  dialog.replaceChildren(
    el("h2", {}, "账户专属配置"),
    help("配置包含代理凭据，请仅交给获授权的使用者。"),
    result.client
      ? help(
          `${result.client.nodes}/${result.client.input_nodes} 个节点 · ${result.client.template_name || result.client.template}${result.client.skipped ? ` · 已跳过 ${result.client.skipped} 个不兼容节点` : ""}`,
        )
      : null,
    help(validationLabel(result.validation)),
    controls,
    output,
    qr,
    button("关闭", () => dialog.close()),
  );
  dialog.showModal();
}
