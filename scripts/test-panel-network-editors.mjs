import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import { capabilitiesFixture } from "./panel-fixtures.mjs";

const dom = new JSDOM(
  '<main></main><dialog id="dialog"></dialog><div id="notice"></div>',
  {
    url: "http://localhost/",
    pretendToBeVisual: true,
  },
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
const main = document.querySelector("main");
const click = async (root, text) => {
  const button = [...root.querySelectorAll("button")].find(
    (node) => node.textContent === text,
  );
  assert(button, `Missing ${text}`);
  button.click();
  await tick();
};
const submit = async (form) => {
  assert(form.checkValidity(), "Fixture must pass native validation");
  form.requestSubmit();
  await tick();
};
const { formDialog } = await import("../internal/manage/panel/form-dialog.js");
const { ruleFields, cleanRule } = await import(
  "../internal/manage/panel/network-fields.js"
);
const { outbounds } = await import("../internal/manage/panel/outbounds.js");
const { pageContext } = await import("../internal/manage/panel/lifecycle.js");

for (const rule of [
  { domain_suffix: "example.com", inbound: "in1", outbound: "direct" },
  { domain_suffix: ["example.com"], action: "bypass", outbound: "direct" },
  { domain_suffix: ["example.com"], action: "bypass" },
  { domain_suffix: ["example.com"], action: "hijack-dns" },
  {
    domain_suffix: ["example.com"],
    action: "route-options",
    override_port: 8443,
  },
  { domain_suffix: ["example.com"], action: "resolve", server: "dns-local" },
]) {
  let saved;
  const original = structuredClone(rule);
  const editor = formDialog(
    "规则",
    ruleFields(["in1"], ["direct"], capabilitiesFixture.runtime.route_actions),
    rule,
    (value) => {
      saved = cleanRule(value, rule);
    },
  );
  await submit(editor.form);
  assert(saved);
  for (const [key, value] of Object.entries(original))
    assert.deepEqual(saved[key], value, `Changed ${key}`);
  assert.equal(saved.action || "route", original.action || "route");
  assert.deepEqual(
    rule,
    original,
    "Opening and saving must not mutate its input",
  );
}

let saved;
const original = {
  domain_suffix: "example.com",
  inbound: "in1",
  outbound: "direct",
};
let editor = formDialog(
  "修改单值",
  ruleFields(
    ["in1", "in2"],
    ["direct"],
    capabilitiesFixture.runtime.route_actions,
  ),
  original,
  (value) => {
    saved = cleanRule(value, original);
  },
);
editor.setValue("domain_suffix", ["one.example", "two.example"]);
editor.setValue("inbound", ["in1", "in2"]);
await submit(editor.form);
assert.deepEqual(saved.domain_suffix, ["one.example", "two.example"]);
assert.deepEqual(saved.inbound, ["in1", "in2"]);
editor = formDialog(
  "切换动作",
  ruleFields(["in1"], ["direct"], capabilitiesFixture.runtime.route_actions),
  original,
  (value) => {
    saved = cleanRule(value, original);
  },
);
editor.input("action").value = "reject";
editor.refresh();
await submit(editor.form);
assert.equal(saved.outbound, undefined);

const config = {
  app: {
    nodes: [],
    advanced: {
      endpoints: [
        {
          type: "wireguard",
          tag: "warp-out",
          address: ["172.16.0.2/32"],
          private_key: "initial-key",
          peers: [
            {
              address: "original.example",
              port: 51820,
              public_key: "public-key",
              allowed_ips: "0.0.0.0/0",
            },
          ],
        },
      ],
    },
  },
};
globalThis.fetch = async (path) =>
  new Response(
    JSON.stringify(
      path === "/api/capabilities"
        ? capabilitiesFixture
        : {
            outbounds: [{ type: "direct", tag: "direct" }],
            endpoints: config.app.advanced.endpoints,
          },
    ),
    { status: 200 },
  );
let controller,
  renders = 0,
  changes = 0;
const ctx = {
  session: { role: "admin" },
  config: async () => config,
  changed() {
    changes++;
  },
  render,
};
async function render() {
  controller?.abort();
  controller = new AbortController();
  renders++;
  const view = pageContext(ctx, controller.signal);
  view.section = "warp";
  main.replaceChildren(...(await outbounds(view)));
  await tick();
}
await render();
await click(main, "管理对端");
for (const address of ["first.example", "second.example"]) {
  await click(dialog, "编辑");
  dialog.querySelector('[aria-label="服务器地址"]').value = address;
  await submit(dialog.querySelector("form"));
  assert.equal(config.app.advanced.endpoints[0].peers[0].address, address);
  assert(dialog.open, "Successful child save returns to its resource list");
}
assert.equal(
  renders,
  1,
  "Peer edits must update locally, keeping the current draft owner",
);
await click(dialog, "完成");
await click(main, "编辑");
dialog.querySelector('[aria-label="私钥"]').value = "updated-key";
await submit(dialog.querySelector("form"));
assert.equal(
  config.app.advanced.endpoints[0].peers[0].address,
  "second.example",
);
assert.equal(
  config.app.advanced.endpoints[0].peers[0].allowed_ips,
  "0.0.0.0/0",
);
assert.equal(config.app.advanced.endpoints[0].private_key, "updated-key");

await click(main, "管理对端");
await click(dialog, "编辑");
const abandoned = dialog.querySelector("form");
dialog.querySelector('[aria-label="服务器地址"]').value = "abandoned.example";
const before = changes;
controller.abort();
abandoned.dispatchEvent(new Event("submit", { cancelable: true }));
await tick();
assert(!dialog.open);
assert.equal(changes, before, "A retired dialog must not commit or reopen");
assert.equal(
  config.app.advanced.endpoints[0].peers[0].address,
  "second.example",
);
dom.window.close();
console.log(
  "Network editors passed: listable values, route semantics, repeated peer edits, local updates and page cancellation.",
);
