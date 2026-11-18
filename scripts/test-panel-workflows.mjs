import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import { readFile } from "node:fs/promises";
const dom = new JSDOM(
  await readFile(
    new URL("../internal/manage/panel/index.html", import.meta.url),
    "utf8",
  ),
  { url: "http://localhost/" },
);
for (const key of ["window", "document", "Node", "Event", "FormData"])
  globalThis[key] = dom.window[key];
dom.window.HTMLDialogElement.prototype.showModal = function () {
  this.open = true;
};
dom.window.HTMLDialogElement.prototype.close = function () {
  this.open = false;
};
const { pageContext } = await import("../internal/manage/panel/lifecycle.js");
const { accounts } = await import("../internal/manage/panel/accounts.js");
const { setCSRF } = await import("../internal/manage/panel/api.js");
const { notice } = await import("../internal/manage/panel/dom.js");
const calls = [];
globalThis.fetch = async (path, options) => {
  if (options.signal?.aborted) throw new DOMException("aborted", "AbortError");
  calls.push(path);
  const value =
    path === "/api/subscribers"
      ? []
      : path.startsWith("/api/accounts?") || path.startsWith("/api/grants?")
        ? { items: [], total: 0 }
        : path.startsWith("/api/inbound-options")
          ? []
          : path === "/api/clients"
            ? { uri: { label: "URI" } }
            : {};
  return { ok: true, json: async () => value };
};
setCSRF("test-session");
const page = new AbortController();
const context = pageContext(
  {
    session: { role: "admin" },
    render() {
      page.abort();
    },
    task: async () => {},
  },
  page.signal,
);
document
  .querySelector("#content")
  .replaceChildren(...(await accounts(context)));
const click = (label, root = document) => {
  const button = [...root.querySelectorAll("button")].find(
    (b) => b.textContent === label,
  );
  assert(button, label);
  button.click();
};
const tick = () => new Promise((resolve) => setTimeout(resolve, 0));
click("新建权益");
document.querySelector('input[aria-label="权益 ID"]').value = "audit-account";
document.querySelector('input[aria-label="名称"]').value = "测试权益";
document
  .querySelector("dialog form")
  .dispatchEvent(new Event("submit", { cancelable: true }));
await tick();
assert.equal(page.signal.aborted, true);
assert.match(document.querySelector("dialog").textContent, /权益已创建/);
const share = [...document.querySelectorAll("dialog button")].find(
  (b) => b.textContent === "创建分享",
);
click("绑定入站");
await tick();
assert.match(document.querySelector("dialog h2").textContent, /绑定入站/);
share.click();
await tick();
assert.match(document.querySelector("dialog h2").textContent, /创建账户分享/);
assert(calls.includes("/api/clients"));
assert.equal(
  calls.filter((p) => p.startsWith("/api/inbound-options")).length,
  2,
);
clearTimeout(notice.timer);
dom.window.close();
console.log(
  "Panel workflow checks passed: post-create binding and sharing survive page cancellation.",
);
