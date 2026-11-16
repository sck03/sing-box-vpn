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
globalThis.confirm = () => true;
const { inbounds, routing, outbounds, vpngate, certificates } =
  await import("../internal/manage/panel/network.js");
const { accounts, subscribers, grants } =
  await import("../internal/manage/panel/accounts.js");
const { subscriptions } =
  await import("../internal/manage/panel/subscriptions.js");
const { overview, maintenance, admins } =
  await import("../internal/manage/panel/maintenance.js");
const { resourceEditor } = await import("../internal/manage/panel/ui.js");
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
    grants: [],
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
    path === "/api/server"
      ? structuredClone(effective)
      : path === "/api/overview"
        ? {
            revision: 1,
            applied_revision: 1,
            services: [],
            runtimes: [],
            tasks: [],
          }
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
  replaceConfig() {},
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
console.log(
  "Panel DOM checks passed: 12 pages, view-only drafts, resource copy, operator scope.",
);
dom.window.close();
