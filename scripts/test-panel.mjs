import assert from "node:assert/strict";
import { capabilitiesFixture } from "./panel-fixtures.mjs";
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
globalThis.confirm = () => true;
const { inbounds } = await import("../internal/manage/panel/network.js");
const { certificates } = await import("../internal/manage/panel/certificate-page.js");
const { routing } = await import("../internal/manage/panel/routing.js");
const { outbounds } = await import("../internal/manage/panel/outbounds.js");
const { vpngate } = await import("../internal/manage/panel/vpngate.js");
const { accounts } = await import("../internal/manage/panel/accounts.js");
const { subscribers } = await import("../internal/manage/panel/subscribers.js");
const { grants } = await import("../internal/manage/panel/grants.js");
const { subscriptions } =
  await import("../internal/manage/panel/subscriptions.js");
const { overview } = await import("../internal/manage/panel/overview.js");
const { admins } = await import("../internal/manage/panel/admins.js");
const { maintenance } = await import("../internal/manage/panel/maintenance.js");
const { resourceEditor, table } =
  await import("../internal/manage/panel/ui.js");
const config = {
  app: {
    name: "test",
    address: "127.0.0.1",
    nodes: [],
    advanced: {},
    routing: {},
  },
  vpn: { profiles: {} },
  gateway: {},
  tunnel: { mode: "off" },
  certificates: [],
  listener: { port: 2097 },
  subscriptions: {
    sources: [],
    collections: [],
    templates: [],
    outputs: [],
    rule_sets: [],
    deliveries: [],
  },
};
const effective = {
  outbounds: [
    { type: "direct", tag: "direct" },
    { type: "socks", tag: "socks-out", server: "127.0.0.1", server_port: 1080 },
  ],
  route: { rules: [], final: "direct" },
  dns: {
    servers: [{ type: "local", tag: "dns-default" }],
    final: "dns-default",
  },
};
globalThis.fetch = async (path) => ({
  ok: true,
  json: async () =>
    path === "/api/capabilities"
      ? capabilitiesFixture
      : path === "/api/server"
        ? structuredClone(effective)
        : String(path).startsWith("/api/tasks?") ? { items: [], next_before: 0 } : path === "/api/overview"
          ? {
              revision: 1,
              applied_revision: 1,
              services: [],
              runtimes: [],
              tasks: [],
              task_counts: { active: 0, failed: 0, attention: 0 },
            }
          : String(path).startsWith("/api/grants") ||
              String(path).startsWith("/api/accounts?")
            ? { items: [], total: 0, offset: 0, limit: 20 }
            : [],
});
const ctx = {
  session: { role: "admin" },
  root: "/tmp/test",
  dirty: false,
  config: async () => config,
  changed() {
    this.dirty = true;
  },
  render() {},
  observeRevision() {},
  task() {},
  selectCountry() {},
};
for (const page of [
  inbounds,
  routing,
  outbounds,
  vpngate,
  certificates,
  accounts,
  subscribers,
  grants,
  subscriptions,
  overview,
  maintenance,
  admins,
]) {
  const before = JSON.stringify(config);
  const content = await page(ctx);
  assert(content.every((n) => n instanceof Node));
  assert.equal(
    JSON.stringify(config),
    before,
    `${page.name} mutated the draft while viewing`,
  );
  document.querySelector("#content").replaceChildren(...content);
}
const items = [{ type: "direct", tag: "direct" }];
let changed = 0;
document.querySelector("#content").replaceChildren(
  resourceEditor({
    title: "outbounds",
    items,
    fields: [{ key: "tag", label: "Tag" }],
    defaults: { type: "direct", tag: "new" },
    change: () => changed++,
  }),
);
[...document.querySelectorAll("button")]
  .find((b) => b.textContent === "复制")
  .click();
document.querySelector("#dialog input").value = "copy";
document
  .querySelector("#dialog form")
  .dispatchEvent(new Event("submit", { cancelable: true }));
await new Promise((resolve) => setTimeout(resolve, 0));
assert.equal(changed, 1);
assert.equal(items[1].tag, "copy");
assert.equal(items[1].id, undefined);
ctx.session.role = "operator";
const operator = await maintenance(ctx);
assert.equal(operator.length, 2);
assert(
  !operator
    .map((n) => n.textContent)
    .join()
    .includes("下载全量备份"),
);
const list = table(
  [{ key: "name", label: "Name" }],
  Array.from({ length: 51 }, (_, i) => ({ name: "row " + i })),
);
document.querySelector("#content").replaceChildren(list);
document.querySelector("#shell").hidden = false;
const search = list.querySelector("input");
search.value = "row";
search.dispatchEvent(new Event("input"));
[...list.querySelectorAll("button")]
  .find((b) => b.textContent === "下一页")
  .click();
search.focus();
list.updateItems(Array.from({ length: 52 }, (_, i) => ({ name: "row " + i })));
assert.equal(list.querySelector("input"), search);
assert.equal(search.value, "row");
assert.equal(document.activeElement, search);
assert.match(list.querySelector(".pager").textContent, /2 \/ 3/);

const { api, setCSRF } = await import("../internal/manage/panel/api.js");
let release;
globalThis.fetch = () =>
  new Promise((resolve) => {
    release = resolve;
  });
setCSRF("old-session");
const pending = api("/api/config");
setCSRF("");
release({ ok: true, json: async () => ({ secret: "old" }) });
await assert.rejects(pending, { name: "AbortError" });

// Exercise the application through its controls while requests are in flight.
globalThis.location = dom.window.location;
location.hash = "inbounds";
const configReads = [],
  configWrites = [],
  countries = [],
  previews = [];
const reply = (value) => ({ ok: true, json: async () => value });
const tick = () => new Promise((resolve) => setTimeout(resolve, 0));
globalThis.fetch = (path, options) => {
  if (path === "/api/auth/session")
    return Promise.resolve(
      reply({ id: "admin", role: "admin", csrf: "current" }),
    );
  const queue =
    path === "/api/country"
      ? countries
      : path === "/api/config/preview"
        ? previews
        : path === "/api/config"
          ? options.method === "GET"
            ? configReads
            : configWrites
          : null;
  assert(queue, `unexpected application request: ${path}`);
  return new Promise((resolve) =>
    queue.push({
      body: options.body ? JSON.parse(options.body) : undefined,
      resolve: (value) => resolve(reply(value)),
    }),
  );
};
await import("../internal/manage/panel/app.js");
await tick();
document.querySelector("#refresh").click();
await tick();
assert.equal(
  configReads.length,
  1,
  "concurrent renders must share the config load",
);
configReads[0].resolve({
  config: structuredClone(config),
  revision: 1,
  root: "/tmp/test",
});
await tick();
async function editPublish(label, value) {
  [...document.querySelectorAll("#content button")]
    .find((b) => b.textContent === "编辑发布信息")
    .click();
  document.querySelector(`#dialog input[aria-label="${label}"]`).value = value;
  document
    .querySelector("#dialog form")
    .dispatchEvent(new Event("submit", { cancelable: true }));
  await tick();
}
await editPublish("节点名称", "submitted");
document.querySelector("#save").click();
assert.equal(configWrites[0].body.config.app.name, "submitted");
await editPublish("节点名称", "edited-during-save");
configWrites[0].resolve({ task_id: 1 });
await tick();
assert.equal(
  document.querySelector("#draft").hidden,
  false,
  "edits made during a save must remain unsaved",
);
document.querySelector("#save").click();
assert.equal(configWrites[1].body.revision, 2);
assert.equal(configWrites[1].body.config.app.name, "edited-during-save");
configWrites[1].resolve({ task_id: 2 });
await tick();
assert.equal(document.querySelector("#draft").hidden, true);

document.querySelector("#preview").click();
await editPublish("IP / 域名", "changed.example.com");
previews[0].resolve({ valid: true });
await tick();
assert.equal(
  document.querySelector("#dialog .changed-fields"),
  null,
  "preview diff must describe the validated snapshot",
);
document.querySelector("#save").click();
document.querySelector("#discard").click();
await tick();
configReads[1].resolve({
  config: structuredClone(config),
  revision: 10,
  root: "/tmp/test",
});
await tick();
await editPublish("节点名称", "after-reload");
configWrites[2].resolve({ task_id: 3 });
await tick();
assert.equal(
  document.querySelector("#draft").hidden,
  false,
  "a stale save must not clear a reloaded draft",
);
document.querySelector("#save").click();
assert.equal(
  configWrites[3].body.revision,
  10,
  "a stale save must not overwrite the revision",
);
assert.equal(configWrites[3].body.config.app.name, "after-reload");
configWrites[3].resolve({ task_id: 4 });
await tick();
location.hash = "vpngate";
window.dispatchEvent(new Event("hashchange"));
await tick();
[...document.querySelectorAll("#content button")]
  .find((b) => b.textContent === "选择 / 新增国家")
  .click();
document.querySelector('dialog select[aria-label="国家代码"]').value = "JP";
document
  .querySelector("dialog form")
  .dispatchEvent(new Event("submit", { cancelable: true }));
await tick();
assert.equal(
  countries[0].body.config.app.name,
  "after-reload",
  "country preparation discarded existing draft",
);
const countryDraft = structuredClone(countries[0].body.config);
countryDraft.vpn.active_country = "JP";
countries[0].resolve(countryDraft);
await tick();
document.querySelector("#save").click();
assert.equal(configWrites[4].body.config.app.name, "after-reload");
assert.equal(configWrites[4].body.config.vpn.active_country, "JP");
configWrites[4].resolve({ task_id: 5 });
await tick();
[...document.querySelectorAll("#content button")]
  .find((b) => b.textContent === "选择 / 新增国家")
  .click();
document.querySelector('dialog select[aria-label="国家代码"]').value = "US";
document
  .querySelector("dialog form")
  .dispatchEvent(new Event("submit", { cancelable: true }));
await tick();
document.querySelector("#discard").click();
await tick();
configReads[2].resolve({
  config: structuredClone(config),
  revision: 20,
  root: "/tmp/test",
});
await tick();
countries[1].resolve(countryDraft);
await tick();
document.querySelector("#save").click();
assert.equal(configWrites[5].body.revision, 20);
assert.equal(
  configWrites[5].body.config.vpn.active_country,
  undefined,
  "late country response overwrote reloaded draft",
);
configWrites[5].resolve({ task_id: 6 });
await tick();
console.log(
  "Panel DOM checks passed: 12 pages, drafts, role scope, live table state, stale session responses, concurrent config loading/saving/preview/reload.",
);
dom.window.close();
