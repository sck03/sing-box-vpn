import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import { capabilitiesFixture } from "./panel-fixtures.mjs";

const dom = new JSDOM('<main></main><dialog id="dialog"></dialog><div id="notice"></div>', { url: "http://localhost/", pretendToBeVisual: true });
for (const name of ["window", "document", "Node", "Event", "FormData"])
  globalThis[name] = dom.window[name];
dom.window.HTMLDialogElement.prototype.showModal = function () { this.open = true; };
dom.window.HTMLDialogElement.prototype.close = function () { this.open = false; this.dispatchEvent(new Event("close")); };
globalThis.fetch = async (url) => {
  if (url === "/api/capabilities") return { ok: true, json: async () => capabilitiesFixture };
  throw Error("Unexpected request " + url);
};
const flush = () => new Promise((resolve) => setImmediate(resolve));
const { certificates } = await import("../internal/manage/panel/certificate-page.js");
const { exportAccountDialog } = await import("../internal/manage/panel/account-export.js");

const signal = new AbortController();
const config = { certificates: [{ id: "cert", provider: "acme", domain: "draft.example.test" }] };
let status = [], reads = 0, release;
const context = {
  signal: signal.signal, config: async () => config, changed() {}, render() {}, task() {},
  request: async () => { reads++; return release ? new Promise((resolve) => { release = resolve; }) : status; },
};
const root = document.querySelector("main");
root.replaceChildren(...await certificates(context));
const renew = () => [...root.querySelectorAll("button")].find((button) => button.textContent === "申请 / 续期");
assert.equal(renew().disabled, true);
status = [{ id: "cert", provider: "acme", domain: "live.example.test", saved: true, applied: true, can_renew: true, days_left: 90, renewal_state: "automatic" }];
const snapshot = { revision: 2, applied_revision: 2, tasks: [{ id: 1, kind: "apply", state: "succeeded", updated: 1 }] };
context.updateOverview(snapshot);
await flush();
assert.equal(renew().disabled, false);
assert.match(root.textContent, /剩余 90 天/);
assert.match(root.textContent, /草稿：draft.example.test/);
assert.equal(config.certificates[0].domain, "draft.example.test");
context.updateOverview(snapshot);
await flush();
assert.equal(reads, 2, "unchanged snapshots must not refetch certificates");
// A completion arriving during an older request must trigger one follow-up read.
release = true;
context.updateOverview({ ...snapshot, applied_revision: 3 });
context.updateOverview({ ...snapshot, applied_revision: 4 });
context.updateOverview({ ...snapshot, applied_revision: 5 });
assert.equal(reads, 3);
const finish = release;
release = null;
finish(status);
await flush();
assert.equal(reads, 4, "queued refreshes should coalesce without losing the latest state");
release = true;
context.updateOverview({ ...snapshot, applied_revision: 6 });
const beforeAbort = root.textContent;
signal.abort();
release([{ ...status[0], days_left: -9 }]);
await flush();
assert.equal(root.textContent, beforeAbort, "cancelled pages must ignore late results");

const exportSignal = new AbortController();
let submitted;
const ports = [
  { runtime_id: "normal", inbound_id: "a", port: 61001, udp_ports: ["62001"] },
  { runtime_id: "normal", inbound_id: "b", port: 61002, udp_ports: ["62002"] },
  { runtime_id: "vpngate", inbound_id: "a", port: 61003, udp_ports: ["62003"] },
];
const request = async (path, body) => {
  if (path.startsWith("/api/accounts/ports?")) return ports;
  if (path.startsWith("/api/accounts/outputs?")) return [{ profile: "uri", client: "uri", label: "URI", available: true, nodes: 3, input_nodes: 3, mode: "nodes" }];
  if (path === "/api/accounts/export") { submitted = body; return { content: "tuic://example", mime: "text/plain", extension: "txt" }; }
  throw Error("Unexpected request " + path);
};
const account = { id: "multi", name: "Multi", download_scopes: ["normal", "vpngate"] };
let editor = await exportAccountDialog(account, { signal: exportSignal.signal, workflowRequest: request });
assert.equal(Number(editor.value().port_0), 61001);
editor.setValue("port_0", 62001);
editor.setValue("port_1", 62002);
editor.setValue("port_2", 62003);
editor.form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
await flush();
assert.deepEqual(submitted.port_overrides, { "normal/a": 62001, "normal/b": 62002, "vpngate/a": 62003 });
assert.equal("tuic_port" in submitted, false);
editor = await exportAccountDialog(account, { signal: exportSignal.signal, workflowRequest: request });
editor.setValue("port_2", 62003);
editor.setValue("scope", "normal");
editor.form.dispatchEvent(new Event("change", { bubbles: true }));
await flush();
editor.form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
await flush();
assert.deepEqual(submitted.port_overrides, {}, "hidden runtimes must not leak overrides");
assert.equal(submitted.scope, "normal");
exportSignal.abort();
document.querySelector("dialog").close();

const { observeStatus } = await import("../internal/manage/panel/status-observer.js");
const observerLife = new AbortController();
const tasks = Array.from({ length: 12 }, (_, i) => ({ id: i + 2, state: "queued" }));
const initial = { resource_version: 1, revision: 1, applied_revision: 1, tasks, task_counts: { active: 13 } };
const observerContext = { signal: observerLife.signal, overview: initial, section: "status" };
let observed = 0, fallback, stopped = false;
const nativeInterval = window.setInterval, nativeClear = window.clearInterval;
window.setInterval = (fn, delay) => { assert.equal(delay, 60000); fallback = fn; return 123; };
window.clearInterval = (id) => { assert.equal(id, 123); stopped = true; };
const observer = observeStatus(observerContext, async () => { observed++; }, { section: "status" });
await observer.refresh();
observerContext.updateOverview({ ...initial, task_counts: { active: 12 } });
await flush();
assert.equal(observed, 2, "completion outside 12 summaries must refresh");
observerContext.updateOverview({ ...initial, resource_version: 2, task_counts: { active: 12 } });
await flush();
assert.equal(observed, 3, "resource changes must refresh even with identical tasks");
observerContext.sectionChanged("sources");
fallback();
await flush();
assert.equal(observed, 3, "hidden sections must not poll");
observerContext.sectionChanged("status");
await flush();
fallback();
await flush();
assert.equal(observed, 5, "visible fallback covers expiry without tasks");
observerLife.abort();
assert.equal(stopped, true);
fallback();
await flush();
assert.equal(observed, 5);
window.setInterval = nativeInterval;
window.clearInterval = nativeClear;

const { vpngate } = await import("../internal/manage/panel/vpngate.js");
const gatewayLife = new AbortController();
const gatewayContext = {
  signal: gatewayLife.signal, session: { role: "admin" }, section: "gateway",
  config: async () => ({ vpn: { active_country: "KR", profiles: {} }, gateway: {}, certificates: [] }),
  task() {}, changed() {}, render() {},
  request: async () => ({ state: "ready", country: "JP", source_refresh: {} }),
};
root.replaceChildren(...await vpngate(gatewayContext));
await flush();
const gatewayPanel = root.querySelector('[role="tabpanel"]:not([hidden])');
assert.match(gatewayPanel.textContent, /运行国家：JP/);
assert.doesNotMatch(gatewayPanel.textContent, /运行国家：KR/);
gatewayLife.abort();

const { access } = await import("../internal/manage/panel/access.js");
const accessLife = new AbortController();
const accessContext = {
  signal: accessLife.signal, session: { role: "admin" }, section: "subscription",
  config: async () => ({ certificates: [], listener: { enabled: true, public_base_url: "https://draft.example.test", listen: "127.0.0.1", port: 2097 } }),
  request: async (path) => {
    if (path === "/api/certificates") return [];
    if (path === "/api/panel/settings") return { revision: 1, options: { listen: "127.0.0.1:2096" }, restart_required: false };
    if (path === "/api/grant-options") return { listener_enabled: false, pending: true, outputs: [], public_url: "https://panel.example.test" };
    throw Error("Unexpected request " + path);
  },
};
root.replaceChildren(...await access(accessContext));
await flush();
assert.match(root.textContent, /已应用：分享使用面板地址/);
assert.match(root.textContent, /草稿：启用独立下载监听/);
accessLife.abort();
dom.window.close();
console.log("State synchronization passed: resource invalidation, hidden summaries, fallback cleanup, applied/draft separation, certificates and per-inbound export ports.");
