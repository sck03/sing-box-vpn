import { $, el, button, help } from "./dom.js";
import { table } from "./tables.js";

export function showRecords(title, columns, records, description = "") {
  const dialog = $("#dialog");
  dialog.replaceChildren(
    el("h2", {}, title),
    ...(description ? [help(description)] : []),
    table(columns, records),
    button("关闭", () => dialog.close()),
  );
  dialog.showModal();
}

export function showFacts(title, values, description = "") {
  const dialog = $("#dialog");
  const list = el("dl", { class: "detail-grid" });
  for (const [label, value] of values)
    list.append(el("dt", {}, label), el("dd", {}, value ?? "—"));
  dialog.replaceChildren(
    el("h2", {}, title),
    list,
    ...(description ? [help(description)] : []),
    button("关闭", () => dialog.close()),
  );
  dialog.showModal();
}
