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
console.log(
  "Panel contract checks passed: UTF-8 passwords, shared gateway limits, protocol fields, workspace state, mobile sorting, usage focus, operator controls.",
);
dom.window.close();
