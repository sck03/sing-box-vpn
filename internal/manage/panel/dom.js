export const $ = (selector, root = document) => root.querySelector(selector);
export function el(tag, attrs = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(attrs)) {
    if (key.startsWith("on")) node.addEventListener(key.slice(2), value);
    else if (key === "class") node.className = value;
    else if (value !== undefined && value !== null)
      node.setAttribute(key, String(value));
  }
  for (const child of children.flat()) {
    if (child !== null && child !== undefined)
      node.append(
        child instanceof Node ? child : document.createTextNode(String(child)),
      );
  }
  return node;
}
export function notice(message, error = false) {
  const node = $("#notice");
  node.textContent = message;
  node.classList.toggle("error", error);
  node.hidden = false;
  clearTimeout(notice.timer);
  notice.timer = setTimeout(() => (node.hidden = true), error ? 16000 : 6500);
}
export function action(fn) {
  let pending = false;
  return async (event) => {
    if (event?.type === "submit") event.preventDefault();
    if (pending) return;
    pending = true;
    const target = event?.currentTarget;
    const controls =
      target?.tagName === "FORM"
        ? [...target.querySelectorAll("button, input[type=submit]")]
        : target
          ? [target]
          : [];
    const disabled = controls.map((control) => control.disabled);
    try {
      target?.setAttribute("aria-busy", "true");
      controls.forEach((control) => {
        control.disabled = true;
      });
      await fn(event);
    } catch (error) {
      if (error.name !== "AbortError") notice(error.message, true);
    } finally {
      pending = false;
      target?.removeAttribute("aria-busy");
      controls.forEach((control, i) => {
        control.disabled = disabled[i];
      });
    }
  };
}
export const button = (label, fn, kind = "") =>
  el("button", { type: "button", class: kind, onclick: action(fn) }, label);
export const card = (title, ...body) =>
  el("section", { class: "card" }, el("h2", {}, title), ...body);
export const help = (text) => el("p", { class: "help" }, text);
export const row = (...children) => el("div", { class: "row" }, ...children);
export const badge = (text, kind = "") =>
  el("span", { class: "tag " + kind }, text);
export function bytes(value) {
  if (value === null || value === undefined) return "不限";
  const n = Number(value);
  if (n < 1024) return `${n} B`;
  const units = ["KiB", "MiB", "GiB", "TiB", "PiB"];
  let v = n / 1024,
    i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(2)} ${units[i]}`;
}
export function date(value) {
  if (!value) return "—";
  return new Date(
    typeof value === "number" ? value * 1000 : value,
  ).toLocaleString();
}
export function getPath(object, path) {
  return path.split(".").reduce((v, k) => v?.[k], object);
}
export function setPath(object, path, value) {
  const parts = path.split(".");
  let cursor = object;
  for (const key of parts.slice(0, -1)) {
    cursor[key] ??= {};
    cursor = cursor[key];
  }
  cursor[parts.at(-1)] = value;
}
export function showResult(title, value) {
  const dialog = $("#dialog");
  dialog.replaceChildren(
    el("h2", {}, title),
    el(
      "pre",
      {},
      typeof value === "string" ? value : JSON.stringify(value, null, 2),
    ),
    button("关闭", () => dialog.close()),
  );
  dialog.showModal();
}
