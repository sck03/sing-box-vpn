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
  this.dispatchEvent(new Event("close"));
};
const tick = () => new Promise((resolve) => setTimeout(resolve, 0));
const field = (name) => document.querySelector(`[aria-label="${name}"]`);
const submit = () =>
  document
    .querySelector("dialog form")
    .dispatchEvent(new Event("submit", { cancelable: true }));
const click = (name, root = document) => {
  const button = [...root.querySelectorAll("button")].find(
    (b) => b.textContent === name,
  );
  assert(button, name);
  button.click();
};

const { formDialog, resourceEditor } =
  await import("../internal/manage/panel/ui.js");
const { templateFields, normalizeTemplate } =
  await import("../internal/manage/panel/clients.js");
const { accountClientDialog } =
  await import("../internal/manage/panel/account-clients.js");
const { subscriptionWizard } =
  await import("../internal/manage/panel/subscription-wizard.js");
const { notice } = await import("../internal/manage/panel/dom.js");

const catalog = {
  mihomo: {
    label: "Mihomo",
    modes: ["config", "provider"],
    features: { dns: true, advanced_dns: true },
  },
  uri: { label: "URI", modes: ["nodes"], features: {} },
};
let saved;
formDialog(
  "DNS 默认值",
  templateFields(catalog),
  { id: "template", client: "mihomo", version: 1, mode: "config" },
  (value) => {
    saved = normalizeTemplate(value, catalog);
  },
  true,
);
assert.equal(
  field("Fake-IP").checked,
  true,
  "form did not show the effective default",
);
field("Fake-IP").checked = false;
submit();
await tick();
assert.equal(saved.dns_policy.fake_ip, false, "explicit disable was discarded");

const calls = [];
const account = {
  id: "account",
  name: "测试账户",
  download_scopes: ["normal", "vpngate"],
};
const uri = {
  client: "uri",
  label: "URI",
  available: true,
  nodes: 2,
  input_nodes: 3,
  skipped: 1,
  template: "account-uri",
};
await accountClientDialog({
  accounts: [account],
  ctx: {
    workflowRequest: async (path) => {
      calls.push(path);
      return path.endsWith("scope=vpngate")
        ? [
            {
              client: "mihomo",
              label: "Mihomo",
              available: true,
              nodes: 1,
              input_nodes: 1,
              skipped: 0,
            },
          ]
        : [
            uri,
            {
              client: "loon",
              label: "Loon",
              available: false,
              nodes: 0,
              input_nodes: 3,
              skipped: 3,
            },
          ];
    },
  },
  title: "兼容性",
  multiple: true,
  save: (value) => {
    saved = value;
  },
});
assert.deepEqual(
  [...field("允许客户端格式（可多选）").options].map((o) => o.value),
  ["uri"],
);
assert.match(
  document.querySelector("dialog").textContent,
  /跳过 1 个不兼容节点/,
);
field("运行实例").value = "vpngate";
field("运行实例").dispatchEvent(new Event("change", { bubbles: true }));
await tick();
assert.equal(calls.length, 2);
assert.deepEqual(
  [...field("允许客户端格式（可多选）").options].map((o) => o.value),
  ["mihomo"],
);
field("允许客户端格式（可多选）").options[0].selected = true;
submit();
await tick();
assert.deepEqual(saved.clients, ["mihomo"]);

let resolveOld;
const old = new Promise((resolve) => {
  resolveOld = resolve;
});
let loads = 0;
await accountClientDialog({
  accounts: [account],
  ctx: { workflowRequest: async () => (++loads === 2 ? old : [uri]) },
  title: "延迟响应",
  save() {},
});
field("运行实例").value = "normal";
field("运行实例").dispatchEvent(new Event("change", { bubbles: true }));
field("运行实例").value = "vpngate";
field("运行实例").dispatchEvent(new Event("change", { bubbles: true }));
await tick();
resolveOld([{ client: "old", label: "过期结果", available: true }]);
await tick();
assert.equal(
  field("格式").options[0].value,
  "uri",
  "stale scope request replaced current choices",
);
document.querySelector("dialog").close();

const config = {
  subscriptions: {
    version: 4,
    sources: [],
    collections: [],
    templates: [],
    outputs: [],
  },
};
const presets = [
  {
    id: "proxy",
    name: "全局代理",
    templates: {
      uri: {
        id: "base",
        client: "uri",
        version: 1,
        mode: "nodes",
        preset: "proxy",
      },
    },
  },
];
let changed = false;
const wizardContext = {
  config: async () => config,
  changed: () => {
    changed = true;
  },
  render() {},
  workflowRequest: async (path, value) => {
    assert.equal(path, "/api/config/preview");
    assert.equal(value.subscriptions.sources[0].format, "auto");
    return {
      valid: true,
      outputs: [
        {
          id: value.subscriptions.outputs[0].id,
          available: true,
          nodes: 1,
          input_nodes: 1,
          diagnostics: [],
        },
      ],
    };
  },
};
subscriptionWizard(wizardContext, config, { uri: catalog.uri }, presets);
field("订阅名称").value = "机场";
field("来源").value = "inline";
field("来源").dispatchEvent(new Event("change", { bubbles: true }));
field("节点内容").value =
  "vless://00000000-0000-4000-8000-000000000001@example.com:443";
submit();
await tick();
assert.equal(
  config.subscriptions.outputs.length,
  0,
  "preview modified the draft before acceptance",
);
click("加入草稿");
await tick();
assert(changed);
assert.equal(config.subscriptions.outputs.length, 1);
assert.equal(config.subscriptions.templates[0].client, "uri");

const items = [
  { id: "a", name: "Alpha" },
  { id: "b", name: "Beta" },
];
let localChange;
const editor = resourceEditor({
  title: "本地更新",
  local: true,
  items,
  fields: [
    { key: "id", label: "ID" },
    { key: "name", label: "名称" },
  ],
  defaults: {},
  change: (value) => {
    localChange = value;
  },
});
document.querySelector("#content").replaceChildren(editor);
const search = editor.querySelector(".search");
search.value = "Alpha";
search.dispatchEvent(new Event("input"));
click("编辑", editor);
field("名称").value = "Alpha updated";
submit();
await tick();
assert(localChange.local);
assert.equal(editor.querySelector(".search"), search);
assert.equal(search.value, "Alpha");
assert.match(editor.textContent, /Alpha updated/);

clearTimeout(notice.timer);
dom.window.close();
console.log(
  "Subscription panel passed: effective defaults, compatible clients, stale responses, draft wizard and local list updates.",
);
