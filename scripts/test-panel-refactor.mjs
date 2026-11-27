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
config.app.nodes = null;
let dirty = false;
const preset = { id: "added", protocol: "vless", port: 12345, tls: {} };
globalThis.fetch = async (url) => {
  assert.equal(url, "/api/preset");
  return { ok: true, json: async () => preset };
};
document.querySelector("main").replaceChildren(
  ...(await inbounds({
    ...ctx,
    request: undefined,
    changed() {
      dirty = true;
    },
  })),
);
[...document.querySelectorAll("main button")]
  .find((b) => b.textContent === "新增入站")
  .click();
dialog.querySelector('[aria-label="唯一 ID"]').value = "added";
dialog
  .querySelector("form")
  .dispatchEvent(new Event("submit", { cancelable: true }));
await tick();
assert.equal(dirty, true);
assert.deepEqual(
  config.app.nodes,
  [preset],
  "null node collection detached the edited list",
);
dialog.close();
// Resolver objects must survive edits to unrelated fields, including dynamic options.
const { routeFields } = await import(
  "../internal/manage/panel/network-fields.js"
);
const resolver = {
  server: "dns-local",
  strategy: "prefer_ipv4",
  disable_cache: true,
};
saved = undefined;
let editor = formDialog(
  "route",
  routeFields,
  { final: "direct", default_domain_resolver: resolver },
  (v) => {
    saved = v;
  },
);
assert.equal(
  editor.input("default_domain_resolver").value,
  JSON.stringify(resolver),
);
editor.input("final").value = "proxy";
editor.form.dispatchEvent(new Event("submit", { cancelable: true }));
await tick();
assert.deepEqual(saved.default_domain_resolver, resolver);
assert.equal(saved.final, "proxy");
editor = formDialog(
  "resolver",
  [{ key: "resolver", label: "解析器", type: "resolver" }],
  { resolver },
  (v) => {
    saved = v;
  },
);
editor.input("resolver").value =
  '{"server":"dns-other","strategy":"ipv6_only"}';
editor.form.dispatchEvent(new Event("submit", { cancelable: true }));
await tick();
assert.deepEqual(saved.resolver, {
  server: "dns-other",
  strategy: "ipv6_only",
});
editor = formDialog(
  "invalid",
  [{ key: "resolver", label: "解析器", type: "resolver" }],
  { resolver },
  () => {
    throw Error("invalid value reached save");
  },
);
editor.input("resolver").value = '{"strategy":"ipv4_only"}';
editor.form.dispatchEvent(new Event("submit", { cancelable: true }));
await tick();
assert.equal(editor.dialog.open, true);
assert.equal(editor.input("resolver").getAttribute("aria-invalid"), "true");
assert.match(editor.form.querySelector(".form-error").textContent, /server/);
dialog.close();
editor = formDialog(
  "dynamic",
  [
    {
      key: "resolver",
      label: "解析器",
      type: "select",
      options: () => ["", "dns-local"],
    },
  ],
  { resolver },
  (v) => {
    saved = v;
  },
  true,
);
editor.refresh();
editor.form.dispatchEvent(new Event("submit", { cancelable: true }));
await tick();
assert.deepEqual(saved.resolver, resolver);
const { routing } = await import("../internal/manage/panel/routing.js");
const dnsConfig = {
  app: {
    advanced: {
      route: {},
      dns: {
        servers: [
          { tag: "dns-local", type: "local" },
          {
            tag: "dns-remote",
            type: "https",
            server: "dns.example.test",
            domain_resolver: resolver,
            connect_timeout: "8s",
          },
        ],
      },
    },
  },
};
const dnsPage = await routing({
  config: async () => dnsConfig,
  request: async () => dnsConfig.app.advanced,
  changed() {},
  render() {},
});
document.querySelector("main").replaceChildren(...dnsPage);
const dnsRow = [...document.querySelectorAll("tr")].find((r) =>
  r.textContent.includes("dns-remote"),
);
[...dnsRow.querySelectorAll("button")]
  .find((b) => b.textContent === "编辑")
  .click();
dialog.querySelector('[aria-label="Tag"]').value = "dns-renamed";
dialog
  .querySelector('[aria-label="Tag"]')
  .dispatchEvent(new Event("change", { bubbles: true }));
assert.equal(
  dialog.querySelector('[aria-label="服务器域名解析器"]').value,
  JSON.stringify(resolver),
);
dialog
  .querySelector("form")
  .dispatchEvent(new Event("submit", { cancelable: true }));
await tick();
assert.deepEqual(
  dnsConfig.app.advanced.dns.servers[1].domain_resolver,
  resolver,
);
assert.equal(dnsConfig.app.advanced.dns.servers[1].connect_timeout, "8s");
assert.equal(dnsConfig.app.advanced.dns.servers[1].tag, "dns-renamed");
[...document.querySelectorAll("main button")]
  .find((b) => b.textContent === "添加域名分流")
  .click();
dialog.querySelector('[aria-label="域名后缀"]').value =
  ".EXAMPLE.com, example.com";
dialog.querySelector('[aria-label="IP 策略"]').value = "prefer_ipv4";
dialog
  .querySelector("form")
  .dispatchEvent(new Event("submit", { cancelable: true }));
await tick();
assert.equal("routing" in dnsConfig.app, false);
assert.deepEqual(
  dnsConfig.app.advanced.route.rules.map((r) => r.action),
  ["sniff", "resolve", "route"],
);
assert.deepEqual(dnsConfig.app.advanced.route.rules[2].domain_suffix, [
  "example.com",
]);
console.log(
  "Refactor regressions passed: secret controls, certificate selection, preview drafts, stale dialogs and protocol-aware copy.",
);
dom.window.close();
