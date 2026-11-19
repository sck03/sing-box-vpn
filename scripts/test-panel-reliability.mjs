import assert from "node:assert/strict";
import { capabilitiesFixture } from "./panel-fixtures.mjs";
import { readFile } from "node:fs/promises";
import { JSDOM } from "jsdom";

const dom = new JSDOM(
  await readFile(
    new URL("../internal/manage/panel/index.html", import.meta.url),
    "utf8",
  ),
  { url: "https://example.test/", pretendToBeVisual: true },
);
function globals(window) {
  for (const key of [
    "window",
    "document",
    "Node",
    "Event",
    "FormData",
    "location",
    "history",
  ])
    globalThis[key] = window[key];
  window.HTMLDialogElement.prototype.showModal = function () {
    this.open = true;
  };
  window.HTMLDialogElement.prototype.close = function () {
    this.open = false;
  };
}
globals(dom.window);
const { formDialog, notice } = await import("../internal/manage/panel/ui.js");
let saved = 0,
  release;
formDialog("保存", [], {}, () => {
  saved++;
  return new Promise((resolve) => {
    release = resolve;
  });
});
const form = document.querySelector("#dialog form");
form.dispatchEvent(new Event("submit", { cancelable: true }));
form.dispatchEvent(new Event("submit", { cancelable: true }));
assert.equal(saved, 1);
assert.equal(form.querySelector("[type=submit]").disabled, true);
release();
await new Promise((resolve) => setTimeout(resolve, 0));
assert.equal(form.querySelector("[type=submit]").disabled, false);

const { templateFields, normalizeTemplate } =
  await import("../internal/manage/panel/clients.js");
const catalog = {
  "sing-box": {
    label: "sing-box",
    modes: ["nodes", "config"],
    features: { dns: true, tun: true },
  },
  uri: { label: "URI", modes: ["nodes"], features: {} },
};
let selected;
formDialog(
  "模板",
  templateFields(catalog),
  {
    id: "test",
    version: 1,
    client: "sing-box",
    mode: "config",
    dns: ["1.1.1.1"],
    tun: true,
    rules: [],
  },
  (v) => {
    selected = normalizeTemplate(v, catalog);
  },
);
const client = document.querySelector('[aria-label="客户端"]');
document.querySelector('[aria-label="高级路由规则 JSON"]').value =
  "{unfinished";
client.value = "uri";
client.dispatchEvent(new Event("change", { bubbles: true }));
assert.deepEqual(
  [...document.querySelector('[aria-label="模式"]').options].map(
    (o) => o.value,
  ),
  ["nodes"],
);
document
  .querySelector("#dialog form")
  .dispatchEvent(new Event("submit", { cancelable: true }));
await Promise.resolve();
assert.equal(selected.dns, undefined);
assert.equal(selected.tun, undefined);
assert.equal(selected.rules, undefined);

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
const realTimeout = globalThis.setTimeout;
let retry, authError;
globalThis.setTimeout = (fn, delay, ...args) =>
  delay >= 1000 && delay <= 30500
    ? ((retry = fn), -1)
    : realTimeout(fn, delay, ...args);
const tick = () => new Promise((resolve) => realTimeout(resolve, 0));
const sources = [];
globalThis.EventSource = class {
  constructor() {
    sources.push(this);
  }
  addEventListener() {}
  close() {}
};
globalThis.confirm = () => true;
globalThis.fetch = async (path) => {
  if (path === "/api/capabilities")
    return { ok: true, json: async () => capabilitiesFixture };
  if (path === "/api/auth/session" && authError) {
    if (authError === "network") throw new TypeError("network unavailable");
    return { ok: false, status: 401, text: async () => "expired" };
  }
  const value =
    path === "/api/auth/session"
      ? { id: "admin", role: "admin", csrf: "test" }
      : path === "/api/config"
        ? { config: structuredClone(config), revision: 1, root: "/test" }
        : {
            revision: 1,
            applied_revision: 1,
            services: [],
            runtimes: [],
            tasks: [],
          };
  return { ok: true, json: async () => value };
};
await import("../internal/manage/panel/app.js");
await tick();
document.querySelector('[data-page="inbounds"]').click();
await tick();
[...document.querySelectorAll("#content button")]
  .find((b) => b.textContent === "编辑发布信息")
  .click();
document.querySelector('[aria-label="节点名称"]').value = "unsaved";
document
  .querySelector("#dialog form")
  .dispatchEvent(new Event("submit", { cancelable: true }));
await tick();
document.querySelector('[data-page="overview"]').click();
await tick();
authError = "network";
sources.at(-1).onerror();
await retry();
assert.equal(document.querySelector("#shell").hidden, false);
assert.equal(document.querySelector("#draft").hidden, false);
authError = "expired";
sources.at(-1).onerror();
await retry();
assert.equal(document.querySelector("#shell").hidden, true);
clearTimeout(notice.timer);

// Hiding a page cancels a pending reconnect check, including its network I/O.
const { overviewStream } = await import("../internal/manage/panel/live.js");
let reconnectSignal;
const stream = overviewStream({
  request: async (_path, _body, { signal }) => {
    reconnectSignal = signal;
    return new Promise((_resolve, reject) =>
      signal.addEventListener("abort", () => reject(signal.reason), {
        once: true,
      }),
    );
  },
  snapshot() {},
  unauthorized() {
    assert.fail("a cancelled check invalidated the session");
  },
});
stream.setActive(true);
sources.at(-1).onerror();
const pendingCheck = retry();
await tick();
assert.equal(reconnectSignal.aborted, false);
const opened = sources.length;
stream.setActive(false);
assert.equal(reconnectSignal.aborted, true);
await pendingCheck;
assert.equal(
  sources.length,
  opened,
  "hidden page reconnected after cancellation",
);
stream.setActive(true);
assert.equal(sources.length, opened + 1);
stream.stop();
globalThis.setTimeout = realTimeout;
dom.window.close();

const token = "a".repeat(64);
const user = new JSDOM(
  '<button id="user-refresh">刷新</button><span id="user-updated"></span><section id="user-content"></section>',
  {
    url: `https://example.test/sub/user#${token}`,
  },
);
globals(user.window);
let requested, userFailure;
globalThis.fetch = async (url) => {
  if (userFailure === "network") throw new TypeError("network unavailable");
  if (userFailure === "revoked") return { ok: false, status: 404 };
  requested = String(url);
  return {
    ok: true,
    json: async () => ({
      name: "Test",
      state: "active",
      quota_bytes: null,
      charged_bytes: 0,
      clients: [
        {
          client: "mihomo",
          label: "Mihomo",
          available: true,
          nodes: 1,
          input_nodes: 1,
          skipped: 0,
        },
      ],
      downloads: 0,
    }),
  };
};
await import("../internal/manage/panel/user.js");
await tick();
assert.equal(requested, `https://example.test/sub/u/${token}/usage`);
assert.equal(
  document.querySelector("a").href,
  `https://example.test/sub/u/${token}/mihomo`,
);
assert.equal(location.hash, `#${token}`);
userFailure = "network";
document.querySelector("#user-refresh").click();
await tick();
assert(
  document.querySelector("a"),
  "transient errors discarded the last snapshot",
);
userFailure = "revoked";
document.querySelector("#user-refresh").click();
await tick();
assert.equal(
  document.querySelector("a"),
  null,
  "revoked authorization kept stale download actions",
);
user.window.close();
console.log(
  "Panel reliability checks passed: duplicate submission, client modes, reconnect drafts, subpath downloads.",
);
