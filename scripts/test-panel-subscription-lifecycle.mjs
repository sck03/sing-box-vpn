import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
const dom = new JSDOM("<main></main>", {
  url: "http://localhost/",
  pretendToBeVisual: true,
});
for (const name of ["window", "document", "Node", "Event", "FormData"])
  globalThis[name] = dom.window[name];
const { subscriptions } =
  await import("../internal/manage/panel/subscriptions.js");
const { remoteTable } = await import("../internal/manage/panel/tables.js");
const tick = () => new Promise((resolve) => setTimeout(resolve, 0));
const life = new AbortController();
const pending = [];
let reads = 0;
let label = "initial output";
let hold = false;
const snapshot = () => ({
  revision: 1,
  sources: [],
  resources: [],
  outputs: [
    {
      id: "out",
      name: label,
      available: true,
      validation: { state: "passed" },
    },
  ],
  deliveries: [
    {
      id: "device",
      kind: "http-put",
      state: "uncertain",
      failures: 2,
      error: "remote acceptance unknown",
    },
  ],
  notification: { configured: false, activity: { state: "disabled" } },
});
const config = {
  subscriptions: {
    sources: [],
    collections: [],
    templates: [],
    outputs: [],
    rule_sets: [],
    deliveries: [],
  },
};
const context = {
  section: "status",
  session: { role: "admin" },
  signal: life.signal,
  config: async () => config,
  changed() {},
  task() {},
  render() {},
  request: async (path) => {
    if (path === "/api/subscriptions/status") {
      reads++;
      if (hold) return new Promise((resolve) => pending.push(resolve));
      return snapshot();
    }
    return [];
  },
};
document.querySelector("main").append(...(await subscriptions(context)));
await tick();
const status = () => document.querySelector('[aria-label="订阅运行状态"]');
assert(status().textContent.includes("initial output"));
assert(status().textContent.includes("远端接收状态未知"));
assert(status().textContent.includes("连续失败"));
label = "completed output";
context.updateOverview({
  applied_revision: 1,
  tasks: [{ id: 1, state: "succeeded", updated: 1 }],
});
await tick();
assert(
  status().textContent.includes(label),
  "task completion did not refresh status",
);
await context.showSection("sources");
const before = reads;
label = "returned output";
context.updateOverview({
  applied_revision: 1,
  tasks: [{ id: 2, state: "succeeded", updated: 2 }],
});
await tick();
assert.equal(reads, before, "hidden status requested data");
await context.showSection("status");
await tick();
assert(
  status().textContent.includes(label),
  "revisiting status kept stale data",
);
hold = true;
context.updateOverview({
  applied_revision: 1,
  tasks: [{ id: 3, state: "running" }],
});
context.updateOverview({
  applied_revision: 1,
  tasks: [{ id: 3, state: "succeeded" }],
});
assert.equal(pending.length, 1, "overlapping SSE created concurrent requests");
pending.shift()(snapshot());
await tick();
assert.equal(pending.length, 1, "coalesced update was lost");
label = "coalesced output";
pending.shift()(snapshot());
await tick();
assert(status().textContent.includes(label));
context.updateOverview({
  applied_revision: 1,
  tasks: [{ id: 4, state: "running" }],
});
life.abort();
label = "late output";
pending.shift()(snapshot());
await tick();
assert(
  !status().textContent.includes(label),
  "cancelled page accepted late status",
);

const tableLife = new AbortController();
const requests = [];
const list = await remoteTable(
  [{ key: "id", label: "ID" }],
  (params, signal) => {
    if (!requests.length && params.q === "") {
      requests.push({ signal });
      return Promise.resolve({ items: [], total: 0 });
    }
    return new Promise((resolve, reject) => {
      requests.push({ signal, params, resolve });
      signal.addEventListener(
        "abort",
        () => reject(new DOMException("cancelled", "AbortError")),
        { once: true },
      );
    });
  },
  null,
  { signal: tableLife.signal, sortOptions: [["id", "ID"]] },
);
document.body.append(list);
const search = list.querySelector("input.search");
search.value = "first";
search.dispatchEvent(new Event("input"));
await new Promise((resolve) => setTimeout(resolve, 210));
assert.equal(requests.length, 2);
search.value = "second";
search.dispatchEvent(new Event("input"));
assert(requests[1].signal.aborted, "superseded search was not cancelled");
await new Promise((resolve) => setTimeout(resolve, 210));
assert.equal(requests.length, 3);
list
  .querySelector('select[aria-label="排序方向"]')
  .dispatchEvent(new Event("change"));
assert(requests[2].signal.aborted, "sort did not cancel prior query");
tableLife.abort();
assert(requests.at(-1).signal.aborted, "page cancellation did not stop query");
await tick();
assert(!list.textContent.includes("cancelled"), "aborts rendered an error");
console.log(
  "Subscription lifecycle checks passed: task updates, cached-tab revisit, hidden pages, coalescing, cancellation and delivery status.",
);
