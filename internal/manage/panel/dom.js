import { errorMessage } from "./api.js";
export const $ = (selector, root = document) => root.querySelector(selector);
const booleanAttributes = new Set([
  "disabled",
  "checked",
  "selected",
  "hidden",
  "open",
  "multiple",
  "required",
  "readonly",
  "autofocus",
  "inert",
  "novalidate",
]);
export function el(tag, attrs = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(attrs)) {
    if (key.startsWith("on")) node.addEventListener(key.slice(2), value);
    else if (key === "class") node.className = value;
    else if (typeof value === "boolean" && booleanAttributes.has(key))
      node.toggleAttribute(key, value);
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
let noticeTimer;
export function notice(message, error = false, link = undefined) {
  const node = $("#notice");
  node.replaceChildren(document.createTextNode(message));
  if (link) node.append(button(link.label, link.action));
  node.classList.toggle("error", error);
  node.hidden = false;
  window.clearTimeout(noticeTimer);
  noticeTimer = window.setTimeout(
    () => (node.hidden = true),
    error ? 16000 : 6500,
  );
}
const pendingControls = new WeakMap();
// Business availability remains authoritative while an async action is busy.
export function setDisabled(control, disabled) {
  const pending = pendingControls.get(control);
  if (pending) pending.disabled = disabled;
  control.disabled = !!pending || disabled;
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
    try {
      target?.setAttribute("aria-busy", "true");
      controls.forEach((control) => {
        const state = pendingControls.get(control) || {
          disabled: control.disabled,
          count: 0,
        };
        state.count++;
        pendingControls.set(control, state);
        control.disabled = true;
      });
      await fn(event);
    } catch (error) {
      if (error.name !== "AbortError") notice(errorMessage(error), true);
    } finally {
      pending = false;
      target?.removeAttribute("aria-busy");
      controls.forEach((control) => {
        const state = pendingControls.get(control);
        if (--state.count === 0) {
          pendingControls.delete(control);
          control.disabled = state.disabled;
        }
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

const menuDocuments = new WeakSet();
function dismissOutsideMenus(doc) {
  if (menuDocuments.has(doc)) return;
  menuDocuments.add(doc);
  let pointerActive = false;
  const dismiss = (event) => {
    for (const menu of doc.querySelectorAll("details.action-menu[open]"))
      if (!menu.contains(event.target)) menu.open = false;
  };
  // Closing an inline menu on pointerdown moves the target before pointerup,
  // so a neighbouring button can lose its click. Dismiss after that click.
  doc.addEventListener(
    "pointerdown",
    () => {
      pointerActive = true;
    },
    true,
  );
  doc.addEventListener(
    "pointerup",
    () => {
      pointerActive = false;
    },
    true,
  );
  doc.addEventListener(
    "pointercancel",
    () => {
      pointerActive = false;
    },
    true,
  );
  doc.addEventListener("click", dismiss);
  doc.addEventListener(
    "focusin",
    (event) => {
      if (!pointerActive) dismiss(event);
    },
    true,
  );
}

export function actionGroup(items, limit = 2) {
  const controls = items.filter(Boolean);
  const group = el("div", { class: "actions" }, controls.slice(0, limit));
  if (controls.length <= limit) return group;
  const toggle = el("summary", { "aria-label": "更多操作" }, "更多");
  const more = el(
    "details",
    { class: "action-menu" },
    toggle,
    el("div", { class: "action-menu-items" }, controls.slice(limit)),
  );
  more.addEventListener("keydown", (event) => {
    if (event.key === "Escape") {
      more.open = false;
      toggle.focus();
    }
  });
  dismissOutsideMenus(more.ownerDocument);
  more.addEventListener("click", (event) => {
    if (event.target.closest("button")) more.open = false;
  });
  group.append(more);
  return group;
}
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
