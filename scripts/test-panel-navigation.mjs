import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { JSDOM } from "jsdom";
import { capabilitiesFixture } from "./panel-fixtures.mjs";

const dom = new JSDOM(
  await readFile(
    new URL("../internal/manage/panel/index.html", import.meta.url),
    "utf8",
  ),
  { url: "https://panel.test/", pretendToBeVisual: true },
);
for (const key of [
  "window",
  "document",
  "Node",
  "Event",
  "FormData",
  "location",
])
  globalThis[key] = dom.window[key];
const tick = () => new Promise((resolve) => setTimeout(resolve, 0));
const { routeFor, searchPages } =
  await import("../internal/manage/panel/page-catalog.js");
assert.equal(searchPages("Argo", "admin")[0].route, "outbounds/argo");
assert.equal(searchPages("备份", "admin")[0].route, "maintenance/backup");
assert.equal(searchPages("Argo", "viewer").length, 0);
assert.equal(searchPages("备份", "operator").length, 0);
assert.equal(routeFor("maintenance/backup", "operator").section, "");
assert.equal(routeFor("maintenance/logs", "operator").section, "logs");
assert.equal(routeFor("admins", "viewer").page, "overview");
assert.equal(routeFor("outbounds/unknown", "admin").hash, "outbounds");

const { workspace } = await import("../internal/manage/panel/workspace.js");
const { sectionLoader } = await import("../internal/manage/panel/sections.js");
let optionalLoads = 0;
const tabs = workspace("optional", [
  ["普通", []],
  [
    "可选",
    () => {
      optionalLoads++;
      return document.createElement("p");
    },
    "optional",
  ],
]);
document.querySelector("#content").replaceChildren(tabs);
assert.equal(optionalLoads, 0);
tabs.querySelectorAll("button")[1].click();
await tick();
assert.equal(optionalLoads, 1);
tabs.querySelectorAll("button")[0].click();
tabs.querySelectorAll("button")[1].click();
await tick();
assert.equal(optionalLoads, 1, "a loaded tab should reuse its content");
const page = new AbortController(),
  late = document.createElement("section");
let release;
const ensure = sectionLoader(
  late,
  () =>
    new Promise((resolve) => {
      release = resolve;
    }),
  page.signal,
);
const first = ensure();
assert.equal(ensure(), first, "optional reads must coalesce");
await tick();
page.abort();
release(document.createTextNode("stale content"));
await first;
assert(!late.textContent.includes("stale content"));
let attempts = 0;
const retryPanel = document.createElement("section");
const retry = sectionLoader(retryPanel, () => {
  if (++attempts === 1) throw new Error("offline");
  return document.createTextNode("recovered");
});
await retry();
assert.match(retryPanel.textContent, /offline/);
await retry();
assert.equal(retryPanel.textContent, "recovered");

const config = {
  app: { name: "test", address: "127.0.0.1", nodes: [], advanced: {} },
  vpn: { profiles: {} },
  gateway: {},
  tunnel: { mode: "off" },
  certificates: [],
  listener: { port: 2097 },
  subscriptions: {},
};
const requests = [];
const snapshot = {
  revision: 1,
  applied_revision: 1,
  services: [],
  runtimes: [],
  tasks: [],
};
globalThis.fetch = async (path) => {
  requests.push(path);
  const data =
    path === "/api/capabilities"
      ? capabilitiesFixture
      : path === "/api/auth/session"
        ? { id: "admin", role: "admin", csrf: "test" }
        : path === "/api/config"
          ? { config: structuredClone(config), revision: 1 }
          : path === "/api/overview"
            ? snapshot
            : String(path).startsWith("/api/accounts?") ||
                String(path).startsWith("/api/grants?")
              ? { items: [], total: 0 }
              : path === "/api/server"
                ? { outbounds: [], dns: { servers: [] } }
                : path === "/api/clients"
                  ? {}
                  : [];
  return { ok: true, json: async () => data };
};
const { accounts } = await import("../internal/manage/panel/accounts.js");
const context = { session: { role: "admin" }, render() {}, task() {} };
const originalPerformance = globalThis.performance;
let now = 0;
globalThis.performance = { now: () => now };
const accountPage = await accounts(context);
document.querySelector("#content").replaceChildren(...accountPage);
assert.equal(
  requests.length,
  1,
  "the entitlement page should only read its list initially",
);
context.updateOverview(snapshot);
context.updateOverview(snapshot);
await tick();
assert.equal(
  requests.length,
  1,
  "duplicate snapshots should not refetch usage",
);
now = 5000;
context.updateOverview(snapshot);
context.updateOverview(snapshot);
await tick();
assert.equal(requests.length, 2);
globalThis.performance = originalPerformance;
const optionalGroup = accountPage.at(-1);
optionalGroup.open = true;
await tick();
await tick();
assert(requests.includes("/api/subscribers"));

const { subscriptions } =
  await import("../internal/manage/panel/subscriptions.js");
const before = JSON.stringify(config);
const compiler = await subscriptions({
  config: async () => config,
  render() {},
  changed() {},
  task() {},
});
document.querySelector("#content").replaceChildren(...compiler);
await tick();
assert.equal(
  JSON.stringify(config),
  before,
  "viewing sparse subscriptions must not modify the draft",
);
assert(
  !requests.includes("/api/subscriptions/status"),
  "hidden status must not fetch",
);
compiler[1].querySelectorAll('[role="tab"]')[5].click();
await tick();
assert(requests.includes("/api/subscriptions/status"));

const { maintenance } = await import("../internal/manage/panel/maintenance.js");
const maintenancePage = await maintenance({ ...context, overview: snapshot });
document.querySelector("#content").replaceChildren(...maintenancePage);
assert(
  !document.querySelector('[type="file"]'),
  "backup content should wait for selection",
);
assert(
  !document
    .querySelector("#maintenance-pane-2")
    .textContent.includes("程序与受管运行时"),
);
document.querySelector("#maintenance-tab-1").click();
await tick();
assert(
  document.querySelector('[type="file"]'),
  "selecting backup should create its controls",
);
const backupFile = document.querySelector('[type="file"]');
document.querySelector("#maintenance-tab-0").click();
document.querySelector("#maintenance-tab-1").click();
await tick();
assert.equal(
  document.querySelector('[type="file"]'),
  backupFile,
  "tab changes should preserve backup selections",
);

const streams = [];
globalThis.EventSource = class {
  constructor() {
    streams.push(this);
  }
  addEventListener() {}
  close() {}
};
await import("../internal/manage/panel/app.js");
await tick();
await tick();
assert.equal(streams.length, 1);
location.hash = "outbounds/argo";
window.dispatchEvent(new Event("hashchange"));
await tick();
await tick();
const argo = document.querySelector("#outbounds-tab-2");
assert.equal(argo.getAttribute("aria-selected"), "true");
assert.match(
  document.querySelector("#outbounds-pane-2").textContent,
  /Argo 隧道/,
);
location.hash = "";
window.dispatchEvent(new Event("hashchange"));
await tick();
await tick();
assert.equal(document.querySelector("#page-title").textContent, "运行概览");
assert.match(document.querySelector("#content").textContent, /常用操作/);
const { notice } = await import("../internal/manage/panel/dom.js");
clearTimeout(notice.timer);
dom.window.close();
console.log(
  "Panel navigation checks passed: task-oriented groups, section search, roles, lazy reads, cancellation, sparse drafts, refresh budgets and browser history.",
);
