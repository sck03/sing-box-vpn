import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import { capabilitiesFixture } from "./panel-fixtures.mjs";
const dom = new JSDOM(
  '<main id="content"></main><dialog id="dialog"></dialog><div id="notice"></div>',
  { url: "http://localhost/", pretendToBeVisual: true },
);
for (const name of ["window", "document", "Node", "Event", "FormData"])
  globalThis[name] = dom.window[name];
dom.window.HTMLDialogElement.prototype.showModal = function () {
  this.open = true;
};
dom.window.HTMLDialogElement.prototype.close = function () {
  this.open = false;
  this.dispatchEvent(new Event("close"));
};
const tick = () => new Promise((resolve) => setTimeout(resolve, 0));
const dialog = document.querySelector("dialog");
const { formDialog } = await import("../internal/manage/panel/ui.js");
const { access } = await import("../internal/manage/panel/access.js");
const { inbounds } = await import("../internal/manage/panel/network.js");
const { accessValue } = await import(
  "../internal/manage/panel/public-access-fields.js"
);
const asset = {
  id: "panel",
  domain: "panel.example.test",
  provider: "import",
  certificate: "/certs/panel/cert.pem",
  key: "/certs/panel/key.pem",
};
const options = {
  listen: "0.0.0.0:2096",
  public_url: "https://panel.example.test:2096",
  certificate: asset.certificate,
  key: asset.key,
  trusted_proxy: [],
};
assert.equal(accessValue(options, [asset]).certificate_asset, "panel");
let saved;
const secret = () =>
  formDialog(
    "secret",
    [{ key: "secret", label: "Reality 私钥", type: "password" }],
    { secret: "example-key" },
    (value) => (saved = value),
  );
secret();
let input = dialog.querySelector("input");
assert.equal(input.type, "password");
let toggle = dialog.querySelector('[aria-label="显示Reality 私钥"]');
toggle.click();
assert.equal(input.type, "text");
assert.equal(toggle.getAttribute("aria-pressed"), "true");
assert.equal(input.value, "example-key");
assert.equal(saved, undefined);
toggle.click();
assert.equal(input.type, "password");
dialog.close();
secret();
assert.equal(dialog.querySelector("input").type, "password");
dialog.close();
const config = {
  certificates: [asset],
  listener: { enabled: false, listen: "127.0.0.1", port: 2097 },
  app: {
    address: "localhost",
    nodes: [
      {
        id: "node",
        protocol: "vless",
        port: 443,
        tls: { reality_private: "secret-key", server_name: "example.test" },
      },
    ],
  },
};
let pending = [];
globalThis.fetch = async (url, request) => {
  if (url === "/api/capabilities")
    return { ok: true, json: async () => capabilitiesFixture };
  if (url.endsWith("preview"))
    return new Promise((resolve) =>
      pending.push({ resolve, body: JSON.parse(request.body) }),
    );
  throw Error("unexpected " + url);
};
const ctx = {
  session: { role: "admin" },
  signal: new AbortController().signal,
  config: async () => config,
  render() {},
  changed() {},
  request: async (path) =>
    path === "/api/certificates"
      ? [{ id: asset.id, days_left: 60 }]
      : { options, active: options, revision: "one" },
};
const page = await access(ctx);
document.querySelector("main").replaceChildren(...page);
await tick();
const open = () =>
  [...page[0].querySelectorAll("button")]
    .find((b) => b.textContent === "配置面板访问")
    .click();
open();
assert.equal(dialog.querySelector('[aria-label="HTTPS 证书"]').value, "panel");
input = dialog.querySelector('[aria-label="面板公网 URL"]');
input.value = "https://new.example.test";
dialog
  .querySelector("form")
  .dispatchEvent(new Event("submit", { cancelable: true }));
assert.equal(pending.length, 1);
let request = pending.shift();
request.resolve({
  ok: true,
  json: async () => ({ options: request.body.options }),
});
await tick();
[...dialog.querySelectorAll("button")]
  .find((b) => b.textContent === "返回编辑")
  .click();
await tick();
assert.equal(
  dialog.querySelector('[aria-label="面板公网 URL"]').value,
  "https://new.example.test",
);
dialog
  .querySelector("form")
  .dispatchEvent(new Event("submit", { cancelable: true }));
request = pending.shift();
dialog.close();
formDialog(
  "另一个编辑窗口",
  [{ key: "name", label: "名称" }],
  { name: "keep" },
  () => {},
);
request.resolve({
  ok: true,
  json: async () => ({ options: request.body.options }),
});
await tick();
assert.equal(dialog.querySelector("h2").textContent, "另一个编辑窗口");
assert.equal(dialog.querySelector("input").value, "keep");
dialog.close();
const inboundPage = await inbounds(ctx);
document.querySelector("main").replaceChildren(...inboundPage);
[...document.querySelectorAll("main button")]
  .find((b) => b.textContent === "复制")
  .click();
assert.equal(
  dialog.querySelector('[aria-label="Reality 私钥"]').value,
  "secret-key",
);
assert(dialog.querySelector('[aria-label="显示Reality 私钥"]'));
dialog.close();
console.log(
  "Refactor regressions passed: secret controls, certificate selection, preview drafts, stale dialogs and protocol-aware copy.",
);
dom.window.close();
