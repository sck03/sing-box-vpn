import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import { readFile } from "node:fs/promises";

const dom = new JSDOM(
  await readFile(
    new URL("../internal/manage/panel/index.html", import.meta.url),
    "utf8",
  ),
  { url: "http://localhost" },
);
for (const key of ["window", "document", "Node", "Event", "FormData"])
  globalThis[key] = dom.window[key];
dom.window.HTMLDialogElement.prototype.showModal = function () {
  this.open = true;
};
dom.window.HTMLDialogElement.prototype.close = function () {
  this.open = false;
};
const { table, badge, formDialog, remoteTable } = await import(
  "../internal/manage/panel/ui.js"
);
const host = document.querySelector("#content");
document.querySelector("#shell").hidden = false;
const list = table(
  [{ label: "状态", render: (v) => badge(v.state) }],
  [{ id: "one", state: "active" }],
  () => [document.createElement("button")],
);
host.replaceChildren(list);
list.querySelector("input").value = "active";
list.querySelector("input").dispatchEvent(new Event("input"));
assert.equal(
  list.querySelectorAll("tbody tr").length,
  1,
  "visible status must be searchable",
);
const original = list.querySelector("tbody");
list.updateItems([{ id: "one", state: "active" }]);
assert.equal(
  list.querySelector("tbody"),
  original,
  "unchanged update rebuilt DOM",
);
const focused = list.querySelector("tbody button");
focused.focus();
list.updateItems([{ id: "one", state: "inactive" }]);
assert.equal(
  list.querySelector("tbody .tag").textContent,
  "inactive",
  "focused control froze live values",
);
assert.equal(
  document.activeElement,
  focused,
  "live update destroyed focused action",
);
list.updateItems([{ id: "one", state: "active" }]);
list.querySelector("input").focus();
await Promise.resolve();
assert.equal(list.querySelectorAll("tbody tr").length, 1);
assert.equal(
  list.querySelector("tbody .tag").textContent,
  "active",
  "deferred update replaced the latest state",
);

let saved;
formDialog(
  "额度",
  [
    {
      key: "quota",
      label: "额度 GiB",
      type: "number",
      scale: 1073741824,
      nullable: true,
    },
  ],
  { quota: 1073741824 },
  (v) => {
    saved = v;
  },
);
const field = document.querySelector("dialog input");
assert.equal(field.value, "1");
field.value = "1.5";
document
  .querySelector("dialog form")
  .dispatchEvent(new Event("submit", { cancelable: true }));
await new Promise((resolve) => setTimeout(resolve, 0));
assert.equal(saved.quota, 1610612736);
field.value = "0.1";
document
  .querySelector("dialog form")
  .dispatchEvent(new Event("submit", { cancelable: true }));
await new Promise((resolve) => setTimeout(resolve, 0));
assert.equal(saved.quota, Math.round(0.1 * 1073741824));

const calls = [];
const remote = await remoteTable(
  [{ key: "id", label: "授权" }],
  async (params) => {
    calls.push(params);
    return { items: [{ id: "page-" + params.offset }], total: 45 };
  },
);
host.replaceChildren(remote);
[...remote.querySelectorAll("button")]
  .find((b) => b.textContent === "下一页")
  .click();
await new Promise((resolve) => setTimeout(resolve, 0));
assert.equal(calls.at(-1).offset, 20);
assert.match(remote.textContent, /page-20/);
const nextPage = [...remote.querySelectorAll("button")].find(
  (b) => b.textContent === "下一页",
);
nextPage.click();
await new Promise((resolve) => setTimeout(resolve, 0));
assert.equal(calls.at(-1).offset, 40);
assert.equal(
  nextPage.disabled,
  true,
  "busy release enabled the last-page button",
);
const lastPageCalls = calls.length;
nextPage.click();
await new Promise((resolve) => setTimeout(resolve, 0));
assert.equal(calls.length, lastPageCalls, "last page sent an extra request");

const preciseDate = "2026-11-17T12:34:59.123Z";
formDialog(
  "修改名称",
  [
    { key: "name", label: "名称" },
    { key: "expires_at", label: "到期", type: "date" },
  ],
  { name: "Original", expires_at: preciseDate },
  (value) => {
    saved = value;
  },
);
document.querySelector('dialog input[aria-label="名称"]').value = "Updated";
document
  .querySelector("dialog form")
  .dispatchEvent(new Event("submit", { cancelable: true }));
await new Promise((resolve) => setTimeout(resolve, 0));
assert.equal(saved.expires_at, preciseDate, "unchanged date lost precision");
assert.equal(saved.name, "Updated");

const { canViewTask } = await import("../internal/manage/panel/tasks.js");
assert.equal(
  !!canViewTask(
    { kind: "apply", details: { actor: "operator" } },
    { id: "operator", role: "operator" },
  ),
  true,
);
assert.equal(
  !!canViewTask(
    { kind: "apply", details: { actor: "other" } },
    { id: "operator", role: "operator" },
  ),
  false,
);
assert.equal(
  !!canViewTask(
    { details: { operators: true } },
    { id: "viewer", role: "viewer" },
  ),
  false,
);

const { createAccountShare } = await import(
  "../internal/manage/panel/sharing.js"
);
let createdShare;
await createAccountShare(
  [
    {
      id: "a",
      name: "Alice",
      download_scopes: ["normal"],
      download_denial: "",
    },
  ],
  {
    workflowRequest: async (path, body) => {
      if (path.startsWith("/api/accounts/outputs?"))
        return [
          {
            client: "uri",
            profile: "uri",
            label: "URI",
            available: true,
            nodes: 1,
            input_nodes: 1,
            skipped: 0,
          },
        ];
      createdShare = body;
      return { user_url: "https://example.com/user#test" };
    },
    render() {},
  },
);
assert.equal(
  document.querySelector('dialog input[aria-label="授权 ID"]'),
  null,
);
assert.equal(
  document.querySelector('dialog input[aria-label="下载到期"]'),
  null,
);
document
  .querySelector("dialog form")
  .dispatchEvent(new Event("submit", { cancelable: true }));
await new Promise((resolve) => setTimeout(resolve, 0));
assert.equal(createdShare.account_id, "a");
assert.match(createdShare.id, /^share-[a-f0-9]{32}$/);
assert.equal(createdShare.expires_at, null);
assert.equal(createdShare.max_downloads, null);
assert.equal(
  document.querySelector("dialog").open,
  true,
  "save closed the share result",
);
assert.match(document.querySelector("dialog").textContent, /分享已创建/);
assert.equal(
  document.querySelector('input[aria-label="分享地址"]').value,
  "https://example.com/user#test",
);

const { grants } = await import("../internal/manage/panel/grants.js");
const pages = await grants({
  session: { role: "admin" },
  request: async (path) =>
    path.startsWith("/api/grants?")
      ? {
          items: [
            { id: "expired", state: "expired", expires_at: "2020-01-01" },
            { id: "used", state: "exhausted", downloads: 1, max_downloads: 1 },
          ],
          total: 2,
        }
      : [],
  render() {},
});
host.replaceChildren(...pages);
assert.match(host.textContent, /已到期/);
assert.match(host.textContent, /次数用尽/);
assert.equal(
  [...host.querySelectorAll(".tag")].some((n) => n.textContent === "有效"),
  false,
);

const { maintenance } = await import("../internal/manage/panel/maintenance.js");
const maintenanceContext = {
  session: { role: "operator" },
  request: async () => ({
    items: [{ id: 42, kind: "service", state: "running", phase: "executing" }],
    next_before: 0,
  }),
};
host.replaceChildren(...(await maintenance(maintenanceContext)));
assert.match(host.textContent, /执行中/);
maintenanceContext.updateOverview({
  tasks: [{ id: 42, kind: "service", state: "succeeded", phase: "succeeded" }],
  task_counts: { active: 0, failed: 0, attention: 0 },
});
assert.match(host.textContent, /已完成/);
assert.doesNotMatch(host.textContent, /执行中/);

const sortable = table(
  [{ key: "count", label: "数量" }],
  [{ count: 20 }, { count: 3 }],
  null,
  { key: "sorting-regression" },
);
host.replaceChildren(sortable);
sortable.querySelector("th button").click();
await Promise.resolve();
assert.equal(sortable.querySelector("td").textContent, "3");
const restored = table(
  [{ key: "count", label: "数量" }],
  [{ count: 20 }, { count: 3 }],
  null,
  { key: "sorting-regression" },
);
assert.equal(restored.querySelector("td").textContent, "3");

const controller = new AbortController();
let currentName = "A",
  pageCalls = 0;
const paged = await remoteTable(
  [{ key: "name", label: "名称" }],
  async (params) => {
    pageCalls++;
    assert.equal(params.sort, "name");
    return { items: [{ id: "a", name: currentName }], total: 1 };
  },
  () => [document.createElement("button")],
  {
    signal: controller.signal,
    sortOptions: [["name", "名称"]],
  },
);
host.replaceChildren(paged);
const liveButton = paged.querySelector("tbody button");
liveButton.focus();
currentName = "B";
await paged.refresh();
assert.equal(document.activeElement, liveButton);
paged.querySelector("input").focus();
await Promise.resolve();
assert.equal(paged.querySelector("td").textContent, "B");
paged.querySelector("input").dispatchEvent(new Event("input"));
const beforeAbort = pageCalls;
controller.abort();
await new Promise((resolve) => setTimeout(resolve, 220));
await paged.refresh();
assert.equal(pageCalls, beforeAbort, "aborted page kept polling");
const compactRow = table(
  [
    { key: "name", label: "名称" },
    { key: "expires", label: "到期", secondary: true },
  ],
  [{ id: 1, name: "A", expires: "tomorrow" }],
);
host.replaceChildren(compactRow);
compactRow.querySelector(".mobile-row-expand").click();
assert(compactRow.querySelector("tr.expanded"));
compactRow.querySelector("input").focus();
compactRow.updateItems([{ id: 1, name: "B", expires: "tomorrow" }]);
assert.equal(
  compactRow.querySelector(".mobile-row-expand").getAttribute("aria-expanded"),
  "true",
  "live update collapsed mobile details",
);
console.log(
  "Panel quality checks passed: task updates, sorting, pagination, focus retention, abort cleanup, status and unit conversion.",
);
