import { $, el, button, help } from "./dom.js";
import { table } from "./tables.js";
import { api } from "./api.js";
import { dialogSession } from "./dialog-session.js";

export async function loadRecords(title, columns, path, signal) {
  return loadDetails(title, path, signal, (records) => table(columns, records));
}

export async function loadResult(title, path, signal) {
  return loadDetails(title, path, signal, (value) => el("pre", {}, JSON.stringify(value, null, 2)));
}

async function loadDetails(title, path, signal, render) {
  const session = dialogSession(signal);
  session.replace(el("h2", {}, title), help("正在加载…"));
  try {
    const records = await api(path, undefined, { signal: session.signal });
    session.replace(el("h2", {}, title), render(records), button("关闭", session.close));
  } catch (error) {
    session.fail(error);
  }
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
