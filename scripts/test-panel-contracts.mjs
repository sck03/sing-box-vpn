import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { JSDOM } from "jsdom";
import { capabilitiesFixture } from "./panel-fixtures.mjs";

const dom = new JSDOM(
  await readFile(
    new URL("../internal/manage/panel/index.html", import.meta.url),
    "utf8",
  ),
  { url: "http://localhost/", pretendToBeVisual: true },
);
for (const key of ["window", "document", "Node", "Event", "FormData"])
  globalThis[key] = dom.window[key];
dom.window.HTMLDialogElement.prototype.showModal = function () {
  this.open = true;
};
dom.window.HTMLDialogElement.prototype.close = function () {
  this.open = false;
};
globalThis.fetch = async (path) => ({
  ok: true,
  json: async () => (path === "/api/capabilities" ? capabilitiesFixture : {}),
});
const tick = () => new Promise((resolve) => setTimeout(resolve, 0));
const { formDialog, table } = await import("../internal/manage/panel/ui.js");
const { validatePassword } = await import(
  "../internal/manage/panel/capabilities.js"
);
assert.doesNotThrow(() =>
  validatePassword("一二三四五六", capabilitiesFixture.password_bytes),
);
assert.throws(() =>
  validatePassword("一二", capabilitiesFixture.password_bytes),
);
assert.doesNotThrow(() =>
  validatePassword("12345678", capabilitiesFixture.password_bytes),
);
assert.throws(() =>
  validatePassword("1234567", capabilitiesFixture.password_bytes),
);
assert.doesNotThrow(() =>
  validatePassword("一二三", capabilitiesFixture.password_bytes),
);
assert.throws(() =>
  validatePassword("a".repeat(73), capabilitiesFixture.password_bytes),
);
assert.equal(
  document.querySelector('[name="password"]').getAttribute("minlength"),
  null,
);

const { gatewayFields } = await import(
  "../internal/manage/panel/network-fields.js"
);
formDialog(
  "网关",
  gatewayFields(capabilitiesFixture.gateway.limits),
  capabilitiesFixture.gateway.defaults,
  () => {},
);
const timeout = document.querySelector('[aria-label="连接超时（秒）"]');
timeout.value = "240";
assert.equal(timeout.checkValidity(), false);
const connections = document.querySelector('[aria-label="最大连接数"]');
connections.value = "2048";
assert.equal(connections.checkValidity(), false);

const { outboundFields, normalizeOutbound } = await import(
  "../internal/manage/panel/outbound-fields.js"
);
let submitted;
formDialog(
  "出站",
  outboundFields(capabilitiesFixture.protocols, ["upstream"], ["dns-local"]),
  { tag: "remote", type: "socks", server: "example.com", server_port: 443 },
  (v) => {
    submitted = normalizeOutbound(v);
  },
);
const type = document.querySelector('[aria-label="类型"]');
type.value = "tuic";
type.dispatchEvent(new Event("change", { bubbles: true }));
assert.equal(document.querySelector('[aria-label="TLS SNI"]').disabled, false);
assert.equal(document.querySelector('[aria-label="密码"]').required, true);
document.querySelector('[aria-label="UUID"]').value =
  "11111111-1111-4111-8111-111111111111";
document.querySelector('[aria-label="密码"]').value = "test-password";
document.querySelector('[aria-label="TLS SNI"]').value = "example.com";
document
  .querySelector("#dialog form")
  .dispatchEvent(new Event("submit", { cancelable: true }));
await tick();
assert.equal(submitted.tls.enabled, true);
assert.equal(submitted.tls.server_name, "example.com");
assert.equal(submitted.version, undefined);

for (const transport of [
  { type: "grpc", service_name: "demo", idle_timeout: "1m" },
  { type: "http", host: ["example.com"], path: "/tunnel" },
]) {
  const original = {
    tag: "advanced",
    type: "vless",
    server: "example.com",
    server_port: 443,
    uuid: "f5dbdb8a-84c0-4c3a-b511-f5f599b37651",
    transport,
    domain_resolver: { server: "dns-local", strategy: "prefer_ipv4" },
    tls: { enabled: true, server_name: "example.com", alpn: ["h2"] },
  };
  formDialog(
    "保留出站",
    outboundFields(capabilitiesFixture.protocols, ["upstream"], ["dns-local"]),
    original,
    (v) => {
      submitted = normalizeOutbound(v, original);
    },
    true,
  );
  document.querySelector('[aria-label="出站标识"]').value = "renamed";
  document
    .querySelector("#dialog form")
    .dispatchEvent(new Event("submit", { cancelable: true }));
  await tick();
  assert.deepEqual(
    submitted,
    { ...original, tag: "renamed" },
    "rename changed advanced connection fields",
  );
}

const { workspace } = await import("../internal/manage/panel/workspace.js");
const panes = { logs: [], backup: [], upgrade: [], network: [] };
const adminContext = { session: { role: "admin" } };
let pages = workspace("maintenance", panes, adminContext);
document.body.append(pages);
pages.querySelectorAll('[role="tab"]')[1].click();
assert.equal(pages.querySelectorAll('[role="tabpanel"]')[1].hidden, false);
pages.remove();
pages = workspace("maintenance", panes, { ...adminContext, section: "backup" });
assert.equal(
  pages.querySelectorAll('[role="tab"]')[1].getAttribute("aria-selected"),
  "true",
);

let actions = 0;
const { button } = await import("../internal/manage/panel/dom.js");
const grid = table(
  [
    { key: "name", label: "名称" },
    { key: "charged", label: "用量" },
  ],
  [{ id: "a", revision: 1, name: "A", charged: 1 }],
  () => [button("编辑", () => actions++)],
  { actionVersion: (a) => [a.id, a.revision] },
);
document.body.append(grid);
assert(grid.querySelector('[aria-label="列表排序"]'));
const edit = grid.querySelector("tbody button");
edit.focus();
grid.updateItems([{ id: "a", revision: 1, name: "A", charged: 2 }]);
edit.click();
await tick();
assert.equal(actions, 1, "usage counters disabled a current action");
grid.updateItems([{ id: "a", revision: 2, name: "B", charged: 2 }]);
edit.click();
await tick();
assert.equal(actions, 1, "a changed policy ran a stale action");

const { vpngate } = await import("../internal/manage/panel/vpngate.js");
const operator = await vpngate({
  session: { role: "operator" },
  config() {
    throw new Error("operator requested secrets");
  },
  task() {},
  section: "gateway",
});
await tick();
const panel = document.createElement("div");
panel.append(...operator);
assert(panel.textContent.includes("换 IP"));
assert(!panel.textContent.includes("选择 / 新增国家"));
const { subscriptionStatus } = await import(
  "../internal/manage/panel/subscription-status.js"
);
const activity = await subscriptionStatus({
  request: async () => ({
    resources: [
      {
        id: "template",
        name: "远程模板",
        kind: "template",
        state: "failed",
        checked: "2026-11-18T00:00:00Z",
        error: "HTTP 503",
      },
    ],
    sources: [],
    notification: {
      configured: true,
      activity: { state: "failed", failures: 2, error: "notification failed" },
      event: "refresh",
    },
  }),
});
assert(activity.textContent.includes("HTTP 503"));
assert(activity.textContent.includes("Webhook 通知"));
assert(activity.textContent.includes("notification failed"));
assert(activity.textContent.includes("最后成功"));
console.log(
  "Panel contract checks passed: UTF-8 passwords, shared gateway limits, protocol fields, workspace state, mobile sorting, usage focus, operator controls.",
);
const { navigation, markNavigation } = await import(
  "../internal/manage/panel/navigation.js"
);
const nav = document.createElement("nav");
nav.append(...navigation("admin", () => {}, "overview"));
document.body.append(nav);
assert.equal(
  nav.querySelectorAll('[data-navigation="common"] > button').length,
  4,
);
assert.equal(nav.querySelectorAll("details").length, 1);
const advanced = nav.querySelector("details");
assert.equal(advanced.open, false);
advanced.querySelector("summary").click();
await tick();
assert.equal(advanced.open, true);
assert.equal(window.localStorage.getItem("sbvpn-navigation:advanced"), "open");
advanced.open = false;
markNavigation(nav, "outbounds", true);
assert.equal(advanced.open, true);
assert.equal(
  nav.querySelector('[data-page="outbounds"]').getAttribute("aria-current"),
  "page",
);
const viewerNav = document.createElement("nav");
viewerNav.append(...navigation("viewer", () => {}, "overview"));
assert.equal(viewerNav.querySelectorAll("button").length, 2);
assert.equal(viewerNav.querySelector('[data-page="access"]'), null);

const { access } = await import("../internal/manage/panel/access.js");
const accessCalls = [];
const initial = {
  listen: "127.0.0.1:2096",
  public_url: "",
  certificate: "",
  key: "",
  trusted_proxy: [],
};
globalThis.fetch = async (path, options) => {
  const body = options.body && JSON.parse(options.body);
  accessCalls.push({ path, body });
  return {
    ok: true,
    json: async () =>
      path.endsWith("preview")
        ? { options: body.options }
        : path === "/api/panel/settings" && !body
          ? { options: initial, revision: "saved" }
          : { ok: true },
  };
};
const accessPage = await access({
  session: { role: "admin" },
  request: async () => ({
    options: initial,
    active: { ...initial, public_url: "http://localhost:2096" },
    revision: "version",
  }),
  config: async () => ({
    certificates: [],
    listener: { enabled: false, listen: "127.0.0.1", port: 2097 },
  }),
  render() {},
});
document.querySelector("#content").replaceChildren(...accessPage);
await tick();
[...accessPage[0].querySelectorAll("button")]
  .find((b) => b.textContent === "配置面板访问")
  .click();
const mode = document.querySelector('[aria-label="访问方式"]');
mode.value = "tls";
mode.dispatchEvent(new Event("change", { bubbles: true }));
assert.equal(
  document.querySelector('[aria-label="监听地址"]').value,
  "0.0.0.0:2096",
);
mode.value = "proxy";
mode.dispatchEvent(new Event("change", { bubbles: true }));
assert.equal(
  document.querySelector('[aria-label="监听地址"]').value,
  "127.0.0.1:2096",
);
document.querySelector('[aria-label="面板公网 URL"]').value =
  "https://panel.example.com";
document
  .querySelector("dialog form")
  .dispatchEvent(new Event("submit", { cancelable: true }));
await tick();
assert.equal(accessCalls.length, 1, "preview must precede settings save");
assert.deepEqual(accessCalls[0].body.options.trusted_proxy, ["127.0.0.1/32"]);
[...document.querySelectorAll("dialog button")]
  .find((b) => b.textContent === "保存配置")
  .click();
await tick();
assert.equal(accessCalls[1].body.revision, "version");
assert.equal(
  accessCalls[1].body.options.public_url,
  "https://panel.example.com",
);
assert.match(document.querySelector("dialog").textContent, /重启/);
// Both public listeners use the same TLS/proxy contract.
const { accessFields, accessOptions, accessValue } = await import(
  "../internal/manage/panel/public-access-fields.js"
);
const certificateAssets = [
  {
    id: "shared",
    domain: "sub.example.test",
    certificate: "/cert.pem",
    key: "/key.pem",
  },
];
formDialog(
  "独立下载监听",
  accessFields(certificateAssets, true),
  {
    ...accessValue({
      listen: "0.0.0.0:2097",
      public_url: "https://sub.example.test",
    }),
    mode: "tls",
    enabled: true,
  },
  () => {},
);
assert.equal(
  document.querySelector('[aria-label="访问方式"]').options.length,
  2,
);
assert.match(document.querySelector("dialog").textContent, /无需 Nginx\/Caddy/);
assert.deepEqual(
  accessOptions(
    {
      mode: "tls",
      listen: "0.0.0.0:2097",
      public_url: "https://sub.example.test/",
      certificate_asset: "shared",
    },
    certificateAssets,
  ),
  {
    listen: "0.0.0.0:2097",
    public_url: "https://sub.example.test",
    certificate: "/cert.pem",
    key: "/key.pem",
    trusted_proxy: [],
  },
);
const { tasksCard, taskDetails, taskStage } = await import(
  "../internal/manage/panel/tasks.js"
);
assert.equal(
  taskStage({ state: "failed", phase: "executing" }),
  "失败于：执行中",
);
taskDetails({
  id: 1,
  kind: "apply",
  state: "succeeded",
  phase: "succeeded",
  result: { retained: false, outcome: "passed" },
});
assert.match(
  document.querySelector("dialog").textContent,
  /详情已按保留策略清理/,
);
const maintenanceCalls = [];
globalThis.confirm = () => true;
globalThis.fetch = async (path, options) => {
  maintenanceCalls.push({
    path,
    body: options.body && JSON.parse(options.body),
  });
  return { ok: true, json: async () => ({ deleted: 1 }) };
};
const historyContext = {
  session: { role: "admin" },
  render() {},
  request: async () => ({
    policy: { count: 20, days: 1, result_bytes: 262144 },
    usage: { completed: 1, pending: 1, result_bytes: 512 },
  }),
};
const tasks = tasksCard(
  [
    { id: 1, kind: "service", state: "failed", phase: "executing" },
    { id: 2, kind: "service", state: "running", phase: "executing" },
  ],
  historyContext,
);
document.querySelector("#content").replaceChildren(tasks);
const finishedRow = tasks.querySelector("tbody tr");
assert.equal(
  tasks.querySelectorAll("tbody tr")[1].textContent.includes("删除"),
  false,
);
[...finishedRow.querySelectorAll("button")]
  .find((b) => b.textContent === "标记已处理")
  .click();
await tick();
assert.deepEqual(maintenanceCalls.pop(), {
  path: "/api/tasks/acknowledge",
  body: { id: 1 },
});
[...finishedRow.querySelectorAll("button")]
  .find((b) => b.textContent === "删除")
  .click();
await tick();
assert.deepEqual(maintenanceCalls.pop(), {
  path: "/api/tasks/delete",
  body: { id: 1 },
});
const { historyControls, journalCard } = await import(
  "../internal/manage/panel/log-maintenance.js"
);
const retention = await historyControls(historyContext);
[...retention.querySelectorAll("button")]
  .find((b) => b.textContent === "清理已处理")
  .click();
await tick();
assert.deepEqual(maintenanceCalls.pop(), {
  path: "/api/tasks/history/clear",
  body: { acknowledged_only: true },
});
assert.equal(
  journalCard([], { session: { role: "operator" } }).textContent.includes(
    "清理运行日志",
  ),
  false,
);
assert(
  capabilitiesFixture.services.some(
    (s) => s.name === "sb-vpngate-core" && s.section === "access",
  ),
);
assert(
  capabilitiesFixture.services.some(
    (s) => s.name === "sb-vpngate-gateway" && s.section === "gateway",
  ),
);
// New connection fields must save exact values and disappear after a protocol
// switch, while an unrelated template edit keeps inherited DNS defaults.
formDialog(
  "Hy2",
  outboundFields(capabilitiesFixture.protocols, [], []),
  {
    tag: "hy2",
    type: "hysteria2",
    server: "example.com",
    server_port: 443,
    password: "secret",
    tls: { enabled: true },
  },
  (v) => {
    submitted = normalizeOutbound(v);
  },
  true,
);
document.querySelector('[aria-label="Hysteria2 跳跃端口"]').value =
  "31000:31100, 31200:31200";
const obfs = document.querySelector('[aria-label="Hysteria2 混淆"]');
obfs.value = "salamander";
obfs.dispatchEvent(new Event("change", { bubbles: true }));
document.querySelector('[aria-label="混淆密码"]').value = "obfs-secret";
document
  .querySelector("#dialog form")
  .dispatchEvent(new Event("submit", { cancelable: true }));
await tick();
assert.deepEqual(submitted.server_ports, ["31000:31100", "31200:31200"]);
assert.deepEqual(submitted.obfs, {
  type: "salamander",
  password: "obfs-secret",
});
const switched = normalizeOutbound({ ...submitted, type: "direct" }, submitted);
assert.equal(switched.obfs, undefined);
assert.equal(switched.server_ports, undefined);
assert.equal(switched.tls, undefined);

const { templateFields, normalizeTemplate } = await import(
  "../internal/manage/panel/clients.js"
);
const catalog = {
  mihomo: {
    label: "Mihomo",
    modes: ["config"],
    features: {
      dns: true,
      advanced_dns: true,
      policy_groups: true,
      rule_providers: true,
      native_template: true,
    },
  },
};
const template = {
  id: "original",
  client: "mihomo",
  version: 1,
  mode: "config",
  rules: [],
};
formDialog(
  "模板",
  templateFields(catalog),
  template,
  (v) => {
    submitted = normalizeTemplate(v, catalog);
  },
  true,
);
document.querySelector('[aria-label="ID"]').value = "renamed";
document
  .querySelector("#dialog form")
  .dispatchEvent(new Event("submit", { cancelable: true }));
await tick();
assert.equal(
  submitted.dns_policy,
  undefined,
  "unrelated edit replaced inherited DNS policy",
);

const { templateResources } = await import(
  "../internal/manage/panel/subscription-fields.js"
);
template.groups = [];
template.providers = [];
template.rules = [
  { type: "domain", value: "first.example", target: "DIRECT" },
  { type: "domain", value: "second.example", target: "PROXY" },
];
let resourceChanges = 0;
const resources = templateResources(
  [template],
  catalog,
  () => resourceChanges++,
);
document.querySelector("#content").replaceChildren(resources);
[...resources.querySelectorAll("button")]
  .find((b) => b.textContent === "↓")
  .click();
await tick();
assert.equal(template.rules[0].value, "second.example");
assert.equal(resourceChanges, 1);
dom.window.close();
