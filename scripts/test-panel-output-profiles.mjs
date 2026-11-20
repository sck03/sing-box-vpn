import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
const dom = new JSDOM(
  '<main></main><dialog id="dialog"></dialog><div id="notice"></div>',
  { url: "http://localhost/" },
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
const { accountOutputDialog } = await import(
  "../internal/manage/panel/account-outputs.js"
);
const { formDialog } = await import("../internal/manage/panel/ui.js");
const { accountTemplateSettings } = await import(
  "../internal/manage/panel/template-settings.js"
);
const { templateResources } = await import(
  "../internal/manage/panel/subscription-fields.js"
);
const { validationLabel } = await import(
  "../internal/manage/panel/validation.js"
);
const dialog = document.querySelector("dialog");
for (const cancel of ["page", "replacement", "close"]) {
  const page = new AbortController();
  let finish, signal;
  const loading = accountOutputDialog({
    accounts: [{ id: "a", name: "A" }],
    ctx: {
      signal: page.signal,
      workflowRequest: async (_, __, options) => {
        signal = options.signal;
        return new Promise((resolve) => (finish = resolve));
      },
    },
    title: "旧导出窗口",
    save() {},
  });
  if (cancel === "page") page.abort();
  if (cancel === "close") dialog.close();
  formDialog("新的编辑窗口", [], {}, () => {});
  await tick();
  assert(signal.aborted, cancel + " did not cancel initial read");
  finish([
    {
      profile: "uri",
      client: "uri",
      label: "URI",
      available: true,
      nodes: 1,
      input_nodes: 1,
    },
  ]);
  await loading;
  assert.equal(dialog.querySelector("h2").textContent, "新的编辑窗口");
}
let changed = 0;
const config = {
  templates: [
    { id: "custom", name: "自定义", client: "mihomo", mode: "provider" },
    { id: "full", client: "mihomo", mode: "config" },
  ],
  account_defaults: {
    "mihomo-provider": { template: "custom", skip_unsupported: true },
  },
};
const section = accountTemplateSettings(
  config,
  {
    request: async () => [
      {
        id: "mihomo-provider",
        client: "mihomo",
        mode: "provider",
        label: "Mihomo 节点",
      },
    ],
  },
  () => changed++,
);
document.querySelector("main").replaceChildren(section);
section.open = true;
section.dispatchEvent(new Event("toggle"));
await tick();
[...section.querySelectorAll("button")]
  .find((b) => b.textContent === "设置")
  .click();
const select = dialog.querySelector('select[aria-label="下载模板"]');
assert.deepEqual(
  [...select.options].map((o) => o.value),
  ["", "custom"],
);
select.value = "";
dialog
  .querySelector("form")
  .dispatchEvent(new Event("submit", { cancelable: true }));
await tick();
assert.equal(changed, 1);
assert.equal(Object.hasOwn(config.account_defaults, "mihomo-provider"), false);
const rules = templateResources(
  [{ id: "simple", client: "loon", mode: "config" }],
  { loon: { label: "Loon", features: {} } },
  () => {},
);
assert.match(rules.textContent, /本地规则/);
assert.equal(rules.querySelectorAll(".resource-navigation button").length, 1);
assert.equal(
  validationLabel({ state: "unavailable", scope: "core-config" }),
  "sing-box 配置检查不可用",
);
assert.match(
  validationLabel({ state: "pending", scope: "selection" }),
  /下载时校验/,
);
assert.equal(
  validationLabel({ state: "passed", engine: "mihomo", scope: "parser" }),
  "节点格式检查通过 · 客户端导入待验证",
);
const { taskDetails } = await import("../internal/manage/panel/tasks.js");
taskDetails({
  id: 1,
  kind: "subscription",
  state: "succeeded",
  outcome: "not_run",
  result: {
    passed: 0,
    skipped: 1,
    total: 1,
    nodes: [
      {
        name: "XHTTP",
        state: "unsupported",
        engine: "sing-box",
        milliseconds: 0,
        success: false,
      },
    ],
  },
});
assert.match(dialog.textContent, /无法检测/);
assert.match(dialog.textContent, /超出检测能力/);
assert.doesNotMatch(dialog.textContent, /需要处理|0 ms/);
dom.window.close();
console.log(
  "Output profiles passed: initial cancellation, dialog ownership, reset, mode isolation, simple rules and validation states.",
);
