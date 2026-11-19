import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
const dom = new JSDOM(
  '<main id="content"></main><dialog id="dialog"></dialog><div id="toast"></div>',
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
const { tasksCard } = await import("../internal/manage/panel/tasks.js");
const { api, RequestError } = await import("../internal/manage/panel/api.js");
const { templateResources } =
  await import("../internal/manage/panel/subscription-fields.js");
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
  { mihomo: { label: "Mihomo", features: { policy_groups: true } } },
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
dom.window.close();
console.log(
  "Panel lifecycle checks passed: navigation abort, competing dialogs, typed errors and contextual resource editors.",
);
