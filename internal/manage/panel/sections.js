import { el, help, button } from "./dom.js";

// Load each optional section once. Page cancellation also invalidates late DOM writes.
export function sectionLoader(root, load, signal) {
  let pending,
    loaded = false;
  return function ensure() {
    if (loaded || signal?.aborted) return Promise.resolve();
    if (pending) return pending;
    root.setAttribute("aria-busy", "true");
    root.replaceChildren(help("正在载入…"));
    pending = Promise.resolve()
      .then(load)
      .then((content) => {
        if (signal?.aborted) return;
        root.replaceChildren(...(Array.isArray(content) ? content : [content]));
        loaded = true;
      })
      .catch((error) => {
        if (!signal?.aborted && error.name !== "AbortError")
          root.replaceChildren(help(error.message), button("重试", ensure));
      })
      .finally(() => {
        pending = null;
        root.removeAttribute("aria-busy");
      });
    return pending;
  };
}
export function disclosure(title, load, signal) {
  const body = el("div"),
    root = el("details", { class: "advanced" }, el("summary", {}, title), body);
  const ensure = sectionLoader(body, load, signal);
  root.addEventListener("toggle", () => {
    if (root.open) void ensure();
  });
  return root;
}
