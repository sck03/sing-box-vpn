import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import { readFile } from "node:fs/promises";
import { capabilitiesFixture } from "./panel-fixtures.mjs";
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
const calls = [];
globalThis.fetch = async (path, options) => {
  if (options.signal?.aborted) throw new DOMException("aborted", "AbortError");
  calls.push(path);
  const value =
    path === "/api/capabilities" ? capabilitiesFixture : path === "/api/subscribers"
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
await tick();
document.querySelector('input[aria-label="权益 ID"]').value = "audit-account";
document.querySelector('input[aria-label="名称"]').value = "测试权益";
document
  .querySelector("dialog form")
  .dispatchEvent(new Event("submit", { cancelable: true }));
await tick();
assert.equal(page.signal.aborted, true);
assert.match(document.querySelector("dialog").textContent, /权益已创建/);
assert.equal(
  [...document.querySelectorAll("dialog button")].some(
    (b) => b.textContent === "创建分享",
  ),
  false,
);
click("绑定入站");
await tick();
assert.match(document.querySelector("dialog h2").textContent, /绑定入站/);
assert.equal(
  calls.filter((p) => p.startsWith("/api/inbound-options")).length,
  4,
);
document.querySelector("dialog").close();
for (const scope of ["normal", "vpngate"]) for (const failed of [false, true]) {
  let saves = 0, queued, share;
  globalThis.fetch = async (path, options) => {
    let value = {};
    if (path === "/api/subscribers") value = [];
    else if (path.startsWith("/api/accounts?")) value = { items: [], total: 0 };
    else if (path.startsWith("/api/inbound-options")) value = path.endsWith(scope) ? [{ id: "node", name: "Node" }] : [];
    else if (path === "/api/accounts") saves++;
    else if (path === "/api/grants") { share = JSON.parse(options.body); value = { user_url: "http://localhost/user#fixture" }; }
    else if (path === "/api/tasks/detail?id=17") value = { id: 17, kind: "account", state: "succeeded" };
    else if (path.startsWith("/api/accounts/detail?")) value = { id: queued.account.id, name: "自动绑定", download_scopes: [scope], download_denial: "" };
    else if (path.startsWith("/api/accounts/outputs?")) value = [{ profile: "uri", client: "uri", label: "URI", available: true, nodes: 1, input_nodes: 1, mode: "nodes" }];
    else throw Error("unexpected " + path);
    return { ok: true, json: async () => value };
  };
  const rendered = new AbortController();
  const next = { session: { role: "admin" }, signal: rendered.signal, render() { rendered.abort(); },
    async task(command) { queued = command; if (failed) throw Error("queue unavailable"); return 17; } };
  document.querySelector("#content").replaceChildren(...await accounts(next));
  click("新建权益"); await tick();
  assert.equal(document.querySelector('[aria-label="创建后自动绑定节点"]').checked, true);
  document.querySelector('[aria-label="名称"]').value = "自动绑定";
  document.querySelector("dialog form").dispatchEvent(new Event("submit", { cancelable: true }));
  await tick(); await tick();
  assert.equal(saves, 1);
  assert.equal(queued.account.scope, scope);
  assert.equal(queued.account.operation, "bind");
  if (failed) {
    assert.match(document.querySelector("dialog").textContent, /权益已经保存/);
    click("绑定入站"); await tick();
    assert.equal(document.querySelector('[aria-label="运行实例"]').value, scope);
    assert.equal(saves, 1, "retry must never create a second account");
  } else {
    assert([...document.querySelectorAll("dialog button")].some((b) => b.textContent === "创建分享"));
    click("创建分享"); await tick(); await tick();
    assert(document.querySelector('[aria-label="设备平台"]'), "sharing inherited the cancelled page signal");
    const expiry = document.querySelector('[aria-label="链接到期时间"]');
    const downloads = document.querySelector('[aria-label="最多下载次数"]');
    assert(expiry && downloads, "account share limits have no UI");
    expiry.value = "2030-01-01T12:30";
    downloads.value = "3";
    document.querySelector("dialog form").dispatchEvent(new Event("submit", { cancelable: true }));
    await tick(); await tick();
    assert.equal(share.max_downloads, 3);
    assert.equal(new Date(share.expires_at).getFullYear(), 2030);
  }
  document.querySelector("dialog").close();
}
const { createAccountShare } = await import("../internal/manage/panel/sharing.js");
const { formDialog } = await import("../internal/manage/panel/form-dialog.js");
for (const mode of ["active", "replaced", "closed", "navigated", "logged-out"]) {
  setCSRF("sharing-" + mode);
  let completeShare, refreshes = 0;
  const sourcePage = new AbortController();
  const workflowRequest = async (path) => {
    if (path.startsWith("/api/accounts/outputs?"))
      return [{ profile: "uri", client: "uri", label: "URI", available: true, nodes: 1, input_nodes: 1, mode: "nodes" }];
    assert.equal(path, "/api/grants");
    return new Promise((resolve) => { completeShare = resolve; });
  };
  await createAccountShare([{ id: "alice", name: "Alice", download_scopes: ["normal"] }], {
    workflowRequest, signal: sourcePage.signal,
    render() { refreshes++; sourcePage.abort(); },
  });
  document.querySelector("dialog form").dispatchEvent(new Event("submit", { cancelable: true }));
  await tick();
  assert(completeShare, "share write did not start");
  const dialog = document.querySelector("dialog");
  if (mode === "replaced" || mode === "navigated") {
    if (mode === "navigated") sourcePage.abort();
    formDialog("新编辑器", [{ key: "name", label: "新草稿" }], { name: "保留未保存内容" }, async () => {});
  } else if (mode === "closed") dialog.close();
  else if (mode === "logged-out") {
    setCSRF("");
    dialog.close();
    dialog.replaceChildren();
  }
  completeShare({ user_url: "http://localhost/user#late-" + mode });
  await tick(); await tick();
  const results = document.querySelector("#workflow-results");
  if (mode === "logged-out") {
    assert.equal(results.children.length, 0, "late result survived logout");
    assert.equal(dialog.open, false);
  } else {
    assert.equal(results.children.length, 1, "completed link became unreachable");
    if (mode === "active") assert.match(dialog.textContent, /分享已创建/);
    if (mode === "replaced" || mode === "navigated") {
      assert.match(dialog.textContent, /新编辑器/);
      assert.equal(dialog.querySelector('[aria-label="新草稿"]').value, "保留未保存内容");
    }
    if (mode === "closed") assert.equal(dialog.open, false, "late response reopened a dismissed editor");
    dialog.close();
    click("查看分享地址", results);
    assert.equal(dialog.querySelector('[aria-label="分享地址"]').value, "http://localhost/user#late-" + mode);
    click("完成", dialog);
    assert.equal(results.children.length, 0);
    assert.equal(results.hidden, true);
    assert.equal(dialog.textContent, "");
  }
  assert.equal(refreshes, mode === "active" ? 1 : 0, "late write cancelled a newer page or dialog");
}
// A result already retained also belongs to its authenticated session.
const { retainDialogResult } = await import("../internal/manage/panel/workflow-results.js");
retainDialogResult("待保存", () => {});
setCSRF("");
assert.equal(document.querySelector("#workflow-results").children.length, 0);
dom.window.close();
console.log(
  "Panel workflow checks passed: binding, share limits, dialog ownership, late result recovery and logout cleanup.",
);
