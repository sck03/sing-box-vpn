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
const { validatePassword } =
  await import("../internal/manage/panel/capabilities.js");
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

const { gatewayFields } =
  await import("../internal/manage/panel/network-fields.js");
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

const { outboundFields, normalizeOutbound } =
  await import("../internal/manage/panel/outbound-fields.js");
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

const { workspace } = await import("../internal/manage/panel/workspace.js");
let pages = workspace("test-workspace", [
  ["来源", []],
  ["输出", []],
]);
document.body.append(pages);
pages.querySelectorAll('[role="tab"]')[1].click();
assert.equal(pages.querySelectorAll('[role="tabpanel"]')[1].hidden, false);
pages.remove();
pages = workspace("test-workspace", [
  ["来源", []],
  ["输出", []],
]);
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
});
const panel = document.createElement("div");
panel.append(...operator);
assert(panel.textContent.includes("换 IP"));
assert(!panel.textContent.includes("选择 / 新增国家"));
const { subscriptionStatus } =
  await import("../internal/manage/panel/subscription-status.js");
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
const { navigation, markNavigation } =
  await import("../internal/manage/panel/navigation.js");
const nav = document.createElement("nav");
nav.append(...navigation("admin", () => {}, "overview"));
document.body.append(nav);
const groups = nav.querySelectorAll("details");
assert.equal(groups[0].querySelectorAll("button").length, 4);
assert.equal(groups[1].open, false);
groups[1].querySelector("summary").click();
await tick();
assert.equal(groups[1].open, true);
assert.equal(window.localStorage.getItem("sbvpn-navigation:advanced"), "open");
groups[1].open = false;
markNavigation(nav, "outbounds", true);
assert.equal(groups[1].open, true);
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
      path.endsWith("preview") ? { options: body.options } : { ok: true },
  };
};
const accessPage = await access({
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
accessPage[0].querySelector("button").click();
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
assert.match(
  document.querySelector("dialog").textContent,
  /systemctl restart sb-panel/,
);
dom.window.close();
