import { sessionSignal } from "./api.js";
import { $, el, button } from "./dom.js";

// A completed write can outlive its editor. Keep its result reachable until
// explicitly acknowledged, and discard secrets when the session changes.
/** @param {string} title
 * @param {(clear: () => void) => void | Promise<void>} display
 * @param {AbortSignal} [signal]
 * @param {string} [label] */
export function retainDialogResult(
  title,
  display,
  signal = sessionSignal(),
  label = "查看分享地址",
) {
  signal.throwIfAborted();
  const area = $("#workflow-results");
  const entry = el("div", { class: "draft workflow-result" });
  const clear = () => {
    entry.remove();
    area.hidden = !area.children.length;
    signal.removeEventListener("abort", clear);
  };
  const show = () => {
    if (!signal.aborted) return display(clear);
  };
  entry.append(el("span", {}, title), button(label, show));
  area.append(entry);
  area.hidden = false;
  signal.addEventListener("abort", clear, { once: true });
  return { show, clear };
}
