import { $, el, button } from "./dom.js";

// Reserve ownership before loading. Replacing or closing the loading dialog
// cancels reads; a late response can never open an editor over a newer dialog.
export async function loadDialog(signal, title, load, open) {
  const session = dialogSession(signal);
  session.replace(
    el("h2", {}, title),
    el("p", { role: "status" }, "正在加载…"),
    button("取消", session.close),
  );
  try {
    const value = await load(session.signal);
    if (!session.active()) return;
    session.close();
    return open(value);
  } catch (error) {
    if (session.active() && error.name !== "AbortError") session.fail(error);
  }
}

// One owner for a dialog and its requests, even when another editor replaces it.
export function dialogSession(signal) {
  signal?.throwIfAborted();
  const dialog = $("#dialog");
  const marker = el("span", { hidden: true });
  const controller = new AbortController();
  const active = () => !controller.signal.aborted && dialog.contains(marker);
  const abort = () => controller.abort();
  const closed = () => {
    if (!dialog.open) abort();
  };
  const observer = new dialog.ownerDocument.defaultView.MutationObserver(() => {
    if (!dialog.contains(marker)) abort();
  });
  controller.signal.addEventListener(
    "abort",
    () => {
      observer.disconnect();
      signal?.removeEventListener("abort", abort);
      dialog.removeEventListener("close", closed);
      if (dialog.contains(marker)) dialog.close();
    },
    { once: true },
  );
  signal?.addEventListener("abort", abort, { once: true });
  dialog.addEventListener("close", closed);
  dialog.replaceChildren(marker);
  observer.observe(dialog, { childList: true });
  dialog.showModal();
  const replace = (...children) => {
    if (active()) dialog.replaceChildren(marker, ...children);
  };
  return {
    signal: controller.signal,
    active,
    replace,
    fail: (error) =>
      replace(
        el("p", { class: "form-error", role: "alert" }, error.message),
        button("关闭", abort),
      ),
    close: abort,
  };
}
