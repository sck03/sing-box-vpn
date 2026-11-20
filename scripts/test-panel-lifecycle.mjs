import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
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
const { tasksCard, taskHistory, openTask } = await import(
  "../internal/manage/panel/tasks.js"
);
const { api, RequestError } = await import("../internal/manage/panel/api.js");
const { templateResources } = await import(
  "../internal/manage/panel/subscription-fields.js"
);
const tick = () => new Promise((resolve) => setTimeout(resolve, 0));
const dialog = document.querySelector("dialog");
let pending = [];
globalThis.fetch = (path) =>
  new Promise((resolve) => pending.push({ path, resolve }));
const controller = new AbortController();
const ctx = {
  signal: controller.signal,
  session: { role: "admin" },
  render() {},
};
document
  .querySelector("main")
  .append(tasksCard([{ id: 7, kind: "apply", state: "succeeded" }], ctx));
const details = [...document.querySelectorAll("button")].find(
  (button) => button.textContent === "详情",
);
details.click();
assert(dialog.open);
controller.abort();
dialog.replaceChildren();
document.querySelector("main").replaceChildren();
pending.shift().resolve({
  ok: true,
  json: async () => ({ id: 7, kind: "apply", state: "succeeded" }),
});
await tick();
assert(!dialog.open, "late task response reopened a dialog after navigation");
assert(!dialog.textContent.includes("任务 #7"));

const active = { ...ctx, signal: new AbortController().signal };
document.querySelector("main").append(
  tasksCard(
    [
      { id: 8, state: "succeeded" },
      { id: 9, state: "succeeded" },
    ],
    active,
  ),
);
const buttons = [...document.querySelectorAll("button")].filter(
  (button) => button.textContent === "详情",
);
buttons[0].click();
buttons[1].click();
pending[1].resolve({
  ok: true,
  json: async () => ({ id: 9, state: "succeeded" }),
});
await tick();
pending[0].resolve({
  ok: true,
  json: async () => ({ id: 8, state: "succeeded" }),
});
await tick();
assert(dialog.textContent.includes("任务 #9"));
assert(
  !dialog.textContent.includes("任务 #8"),
  "older request replaced latest dialog owner",
);
dialog.close();
globalThis.fetch = async () => ({
  ok: false,
  status: 409,
  text: async () =>
    JSON.stringify({
      error: { code: "revision_conflict", message: "reload", retryable: false },
    }),
});
await assert.rejects(
  api("/api/config"),
  (error) =>
    error instanceof RequestError &&
    error.code === "revision_conflict" &&
    error.status === 409 &&
    !error.retryable,
);

const resources = templateResources(
  [
    {
      id: "t",
      client: "mihomo",
      mode: "config",
      groups: [],
      providers: [],
      rules: [],
    },
  ],
  {
    mihomo: {
      label: "Mihomo",
      features: { policy_groups: true, rule_providers: true },
    },
  },
  () => {},
);
document.querySelector("main").replaceChildren(resources);
assert.equal(
  resources.querySelectorAll(".resource-navigation button").length,
  3,
);
assert.equal(
  resources.querySelectorAll(".data-table").length,
  1,
  "inactive resource editors eagerly rendered",
);
assert(
  resources.querySelector(".table-tools").hidden,
  "empty editor retained redundant tools",
);
resources.querySelectorAll(".resource-navigation button")[1].click();
assert.equal(resources.querySelectorAll(".data-table").length, 1);
assert.equal(resources.querySelectorAll('[aria-pressed="true"]').length, 1);
resources.selectTemplate("t");
assert.equal(
  resources.querySelector('select[aria-label="编辑模板资源"]').value,
  "t",
);
const { accounts } = await import("../internal/manage/panel/accounts.js");
for (const label of ["凭据", "历史"]) {
  const navigation = new AbortController();
  const context = {
    signal: navigation.signal,
    session: { role: "admin" },
    render() {},
    task() {},
    request: async (path) =>
      path.startsWith("/api/accounts?")
        ? {
            items: [
              {
                id: "demo",
                name: "Demo",
                state: "active",
                revision: 1,
                quota_bytes: null,
              },
            ],
            total: 1,
            offset: 0,
            limit: 20,
          }
        : [],
  };
  document.querySelector("main").replaceChildren(...(await accounts(context)));
  pending = [];
  globalThis.fetch = (path) =>
    new Promise((resolve) => pending.push({ path, resolve }));
  [...document.querySelectorAll("main button")]
    .find((button) => button.textContent === label)
    .click();
  await tick();
  assert(dialog.open);
  navigation.abort();
  dialog.replaceChildren();
  document.querySelector("main").replaceChildren();
  pending.forEach(({ resolve }) => resolve({ ok: true, json: async () => [] }));
  await tick();
  assert(!dialog.open, label + " reopened after navigation");
  assert(!dialog.textContent.includes("Demo"));
}

const { formDialog } = await import("../internal/manage/panel/ui.js");
formDialog(
  "字段错误",
  [{ key: "port", label: "端口", type: "number" }],
  { port: 443 },
  async () => {
    throw new RequestError("端口无效", 400, {
      code: "invalid_field",
      message: "端口无效",
      field: "app.nodes[test].port",
      retryable: false,
    });
  },
);
dialog
  .querySelector("form")
  .dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
await tick();
assert.equal(
  dialog.querySelector('input[aria-invalid="true"]'),
  document.activeElement,
);
assert(dialog.textContent.includes("端口无效"));
dialog.close();
let pageReads = [];
const pageContext = {
  ...active,
  request: (path) =>
    new Promise((resolve, reject) => pageReads.push({ path, resolve, reject })),
};
const historyPromise = taskHistory(pageContext);
pageReads
  .shift()
  .resolve({ items: [{ id: 100, state: "succeeded" }], next_before: 100 });
const history = await historyPromise;
document.querySelector("main").replaceChildren(history);
const historyButton = (label) =>
  [...history.querySelectorAll("button")].find(
    (item) => item.textContent === label,
  );
pageContext.updateOverview({
  tasks: [{ id: 101, state: "running" }],
  task_counts: { active: 1, attention: 0 },
});
assert.equal(pageReads.length, 1);
pageContext.updateOverview({
  tasks: [{ id: 101, state: "succeeded" }],
  task_counts: { active: 0, attention: 0 },
});
assert.equal(pageReads.length, 1, "snapshot requests were not coalesced");
pageReads.shift().resolve({
  items: [
    { id: 101, state: "running" },
    { id: 100, state: "succeeded" },
  ],
  next_before: 100,
});
await tick();
pageReads.shift().resolve({
  items: [
    { id: 101, state: "succeeded" },
    { id: 100, state: "succeeded" },
  ],
  next_before: 100,
});
await tick();
assert(
  history.textContent.includes("101"),
  "new task never entered first page",
);
historyButton("较早记录").click();
pageReads.shift().reject(new Error("temporary page failure"));
await tick();
assert(
  history.textContent.includes("第 1 页"),
  "failed history request advanced cursor",
);
historyButton("较早记录").click();
assert(pageReads[0].path.includes("before=100"));
pageReads
  .shift()
  .resolve({ items: [{ id: 80, state: "succeeded" }], next_before: 80 });
await tick();
historyButton("较早记录").click();
historyButton("较新记录").click();
assert.equal(
  pageReads.length,
  1,
  "competing history navigation issued multiple reads",
);
pageReads
  .shift()
  .resolve({ items: [{ id: 60, state: "succeeded" }], next_before: 0 });
await tick();
assert(
  history.textContent.includes("第 3 页"),
  "competing navigation corrupted cursor",
);
historyButton("较新记录").click();
assert(pageReads[0].path.includes("before=100"));
pageReads
  .shift()
  .resolve({ items: [{ id: 80, state: "succeeded" }], next_before: 80 });
await tick();
assert(history.textContent.includes("第 2 页"));
pageContext.updateOverview({
  tasks: [{ id: 102, state: "running" }],
  task_counts: { active: 1, attention: 0 },
});
assert.equal(pageReads.length, 0, "live update replaced a historical page");
assert(history.textContent.includes("当前历史页保持不变"));
let detailReads = 0;
globalThis.fetch = async () => ({
  ok: true,
  json: async () => ({
    id: 103,
    kind: "service",
    state: ++detailReads === 1 ? "running" : "succeeded",
    phase: detailReads === 1 ? "executing" : "succeeded",
  }),
});
await openTask(103);
dialog.querySelector("details").open = true;
const closeControl = [...dialog.querySelectorAll("button")].find(
  (b) => b.textContent === "关闭",
);
closeControl.focus();
await new Promise((resolve) => setTimeout(resolve, 1650));
assert(
  dialog.textContent.includes("已完成"),
  "running task detail never refreshed",
);
assert(dialog.querySelector("details").open, "refresh collapsed diagnostics");
assert.equal(
  document.activeElement,
  closeControl,
  "refresh stole close-button focus",
);
dialog.close();
await new Promise((resolve) => setTimeout(resolve, 1550));
assert.equal(detailReads, 2, "terminal or closed task kept polling");
dom.window.close();
console.log(
  "Panel lifecycle checks passed: navigation abort, competing dialogs, typed errors and contextual resource editors.",
);
