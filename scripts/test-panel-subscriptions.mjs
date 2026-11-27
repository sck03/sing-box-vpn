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

const { formDialog, resourceEditor } = await import(
  "../internal/manage/panel/ui.js"
);
const { templateFields, normalizeTemplate } = await import(
  "../internal/manage/panel/clients.js"
);
const { accountOutputDialog } = await import(
  "../internal/manage/panel/account-outputs.js"
);
const { subscriptionWizard } = await import(
  "../internal/manage/panel/subscription-wizard.js"
);

const catalog = {
  mihomo: {
    label: "Mihomo",
    modes: ["config", "provider"],
    features: { dns: true, advanced_dns: true, fake_ip_toggle: true },
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
  field("Fake-IP").value,
  "",
  "form did not show the effective default",
);
field("代理 DNS").value = "https://dns.google/dns-query";
submit();
await tick();
assert.equal(
  saved.dns_policy.fake_ip,
  undefined,
  "editing DNS must preserve inheritance",
);
assert.deepEqual(saved.dns_policy.remote, ["https://dns.google/dns-query"]);
formDialog(
  "关闭 Fake-IP",
  templateFields(catalog),
  saved,
  (value) => {
    saved = normalizeTemplate(value, catalog);
  },
  true,
);
field("Fake-IP").value = "false";
submit();
await tick();
assert.equal(saved.dns_policy.fake_ip, false, "explicit disable was discarded");
formDialog(
  "继承 Fake-IP",
  templateFields(catalog),
  saved,
  (value) => {
    saved = normalizeTemplate(value, catalog);
  },
  true,
);
field("Fake-IP").value = "";
submit();
await tick();
assert.equal(Object.hasOwn(saved.dns_policy, "fake_ip"), false);
assert.throws(
  () =>
    normalizeTemplate(
      { client: "mihomo", mode: "config", native: { dns: {} } },
      {
        mihomo: { ...catalog.mihomo, native_fields: { managed: ["dns"] } },
      },
    ),
  /native.dns/,
);

const calls = [];
const account = {
  id: "account",
  name: "测试账户",
  download_scopes: ["normal", "vpngate"],
};
const uri = {
  profile: "uri",
  client: "uri",
  label: "URI",
  available: true,
  nodes: 2,
  input_nodes: 3,
  skipped: 1,
  template: "account-uri",
};
await accountOutputDialog({
  accounts: [account],
  ctx: {
    workflowRequest: async (path) => {
      calls.push(path);
      return path.endsWith("scope=vpngate")
        ? [
            {
              client: "mihomo",
              profile: "mihomo-provider",
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
              profile: "loon-config",
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
  [...document.querySelectorAll('.choice-list input:not(:disabled)')].map((o) => o.value),
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
  [...document.querySelectorAll('.choice-list input:not(:disabled)')].map((o) => o.value),
  ["mihomo-provider"],
);
document.querySelector('.choice-list input[value="mihomo-provider"]').click();
submit();
await tick();
assert.deepEqual(saved.profiles, ["mihomo-provider"]);

let resolveOld;
const old = new Promise((resolve) => {
  resolveOld = resolve;
});
let loads = 0;
await accountOutputDialog({
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
  field("订阅格式与用途").options[0].value,
  "uri",
  "stale scope request replaced current choices",
);
document.querySelector("dialog").close();

const config = {
  subscriptions: {
    version: 6,
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
  request: async (path, value) => {
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

const { requestConfiguration, showConfiguration } = await import(
  "../internal/manage/panel/config-preview.js"
);
const dialog = document.querySelector("dialog");
let resolvePreview, previewSignal;
const previewController = new AbortController();
const pendingPreview = requestConfiguration(
  (signal) => {
    previewSignal = signal;
    return new Promise((resolve) => {
      resolvePreview = resolve;
    });
  },
  [],
  { signal: previewController.signal },
);
formDialog("新的编辑窗口", [{ key: "name", label: "新名称" }], {}, () => {});
await tick();
assert(previewSignal.aborted, "replacing a preview must abort its request");
resolvePreview({ outputs: [] });
await pendingPreview;
assert.match(
  dialog.textContent,
  /新的编辑窗口/,
  "late preview replaced a newer editor",
);

let resolveDetail, detailSignal;
const page = new AbortController();
showConfiguration({ outputs: [{ id: "out", available: true }] }, [], {
  signal: page.signal,
  loadOutput: (id, signal) => {
    detailSignal = signal;
    return new Promise((resolve) => {
      resolveDetail = resolve;
    });
  },
});
click("查看配置", dialog);
page.abort();
assert(detailSignal.aborted);
assert.equal(dialog.open, false, "page exit must close its preview");
formDialog("下一页编辑", [], {}, () => {});
resolveDetail({ preview: { content: "stale secret" } });
await tick();
assert.match(dialog.textContent, /下一页编辑/);
assert.doesNotMatch(dialog.textContent, /stale secret/);

const { subscriptions } = await import("../internal/manage/panel/subscriptions.js");
let queued;
const subscriptionPage = new AbortController();
const sections = await subscriptions({
  signal: subscriptionPage.signal,
  section: "deliveries",
  session: { role: "admin" },
  config: async () => ({ subscriptions: {} }),
  changed() {}, render() {},
  request: async (path) => {
    assert.equal(path, "/api/subscriptions/actions");
    return { revision: 1, pending: false, outputs: [{ id: "output", name: "Output" }], deliveries: [{ id: "device" }] };
  },
  task: async (command) => { queued = command; },
});
document.querySelector("#content").replaceChildren(...sections);
await tick();
click("分发输出");
assert.equal(field("目的地").value, "device");
assert(field("目的地").required);
document.querySelector("dialog form").requestSubmit();
await tick();
assert.deepEqual(queued, { operation: "subscription", subscription: { operation: "deliver", id: "device" } });
queued = undefined;
click("检测输出");
field("探测 URL").value = "";
assert.equal(document.querySelector("dialog form").checkValidity(), false);
document.querySelector("dialog form").requestSubmit();
await tick();
assert.equal(queued, undefined, "empty probe URL queued a doomed task");
subscriptionPage.abort();

// Cache hits still reserve the dialog before yielding to navigation or an editor.
const cachedWizardPage = new AbortController();
let catalogReads = 0, presetReads = 0;
const wizardSections = await subscriptions({
  signal: cachedWizardPage.signal, session: { role: "admin" }, section: "deliveries",
  config: async () => ({ subscriptions: {} }), changed() {}, render() {}, task() {},
  request: async (path) => {
    if (path === "/api/clients") { catalogReads++; return catalog; }
    if (path === "/api/subscription-presets") { presetReads++; return presets; }
    if (path === "/api/subscriptions/actions") return { outputs: [], deliveries: [] };
    throw Error("Unexpected request " + path);
  },
});
document.querySelector("#content").replaceChildren(...wizardSections);
await tick();
click("快速创建订阅");
await tick();
assert.match(dialog.textContent, /快速创建订阅 · 来源与客户端/);
dialog.close();
click("快速创建订阅");
formDialog("新的编辑器", [], {}, () => {});
await tick();
assert.match(dialog.textContent, /新的编辑器/);
dialog.close();
click("快速创建订阅");
cachedWizardPage.abort();
await tick();
assert.equal(dialog.open, false, "cached wizard reopened after page cancellation");
assert.equal(catalogReads, 1);
assert.equal(presetReads, 1);

const { setCSRF } = await import("../internal/manage/panel/api.js");
setCSRF("output-sharing");
let finishOutputShare, shareRefreshes = 0;
const sharePage = new AbortController();
const shareSections = await subscriptions({
  signal: sharePage.signal, session: { role: "admin" }, section: "outputs",
  config: async () => ({ subscriptions: {} }), changed() {},
  render() { shareRefreshes++; sharePage.abort(); },
  request: async (path) => {
    assert.equal(path, "/api/grant-options");
    return { revision: 1, pending: false, outputs: [{ id: "output", name: "Output" }] };
  },
  workflowRequest: async (path, value) => {
    assert.equal(path, "/api/grants");
    assert.deepEqual(value.outputs, ["output"]);
    return new Promise((resolve) => { finishOutputShare = resolve; });
  },
});
document.querySelector("#content").replaceChildren(...shareSections);
await tick();
click("创建输出分享");
field("授权 ID").value = "output-share";
dialog.querySelector('.choice-control[aria-label="选择输出"] .choice-trigger').click();
dialog.querySelector('input[type="checkbox"][value="output"]').click();
submit();
await tick();
assert(finishOutputShare, "output share write did not start");
formDialog("新输出草稿", [{ key: "name", label: "新名称" }], { name: "保留" }, () => {});
const outputURLs = ["http://localhost/s/fixture/output", "http://localhost/s/fixture/other"];
finishOutputShare({ urls: outputURLs });
await tick(); await tick();
assert.match(dialog.textContent, /新输出草稿/);
assert.equal(field("新名称").value, "保留");
assert.equal(shareRefreshes, 0);
dialog.close();
click("查看分享地址", document.querySelector("#workflow-results"));
assert.equal(field("分享地址").value, outputURLs.join("\n"));
click("完成");
sharePage.abort();
setCSRF("");
dom.window.close();
console.log(
  "Subscription panel passed: effective defaults, compatible clients, stale responses, draft wizard and local list updates.",
);
