import assert from "node:assert/strict";
import { JSDOM } from "jsdom";
import { capabilitiesFixture } from "./panel-fixtures.mjs";

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
const tick = () => new Promise((resolve) => setTimeout(resolve, 0));
const dialog = document.querySelector("dialog");
const requests = [];
globalThis.fetch = async (url, options) => {
  if (url === "/api/capabilities")
    return { ok: true, json: async () => capabilitiesFixture };
  if (url === "/api/tasks/cancel") {
    requests.push(JSON.parse(options.body));
    return { ok: true, json: async () => ({ ok: true }) };
  }
  throw new Error("Unexpected request " + url);
};

const { showConfiguration } = await import(
  "../internal/manage/panel/config-preview.js"
);
showConfiguration({
  valid: true,
  ordinary_server: {},
  outputs: [],
  diagnostics: {},
  deployment: {
    runtime_observed: true,
    applied_revision: 4,
    changes: [
      { name: "sb-argo", action: "restart", reason: "tunnel-connection" },
    ],
  },
});
assert.match(dialog.textContent, /服务变更计划/);
assert.match(dialog.textContent, /sb-argo/);
assert.match(dialog.textContent, /隧道连接参数变化/);
assert.match(dialog.textContent, /当前运行状态/);
dialog.close();
showConfiguration({
  valid: true,
  ordinary_server: {},
  outputs: [],
  diagnostics: {},
  deployment: { runtime_observed: false, applied_revision: 4, changes: [] },
});
assert.match(dialog.textContent, /未读取主机运行状态/);
assert.match(dialog.textContent, /无需重启服务/);
dialog.close();

const { vpngate } = await import("../internal/manage/panel/vpngate.js");
const assets = [
  {
    id: "other",
    domain: "other.example",
    certificate: "/certs/other.pem",
    key: "/certs/other.key",
  },
  {
    id: "selected",
    domain: "selected.example",
    certificate: "C:/certs/selected.pem",
    key: "C:/certs/selected.key",
  },
];
const config = {
  certificates: assets,
  gateway: {},
  vpn: {
    active_country: "JP",
    proxy_port: 7928,
    profiles: {
      JP: {
        ports: { vmess: 8080 },
        certificate: "C:\\certs\\selected.pem",
        key: "C:\\certs\\selected.key",
        tls_sni: "selected.example",
      },
    },
  },
};
let changes = 0;
const ctx = {
  session: { role: "admin" },
  section: "access",
  signal: new AbortController().signal,
  config: async () => config,
  changed: () => changes++,
  render() {},
};
document.querySelector("main").replaceChildren(...(await vpngate(ctx)));
await tick();
[...document.querySelectorAll("button")]
  .find((b) => b.textContent === "绑定证书")
  .click();
assert.equal(dialog.querySelector("select").value, "selected");
assert.equal(changes, 0, "opening certificate binding mutated the draft");
dialog.close();

const { tasksCard } = await import("../internal/manage/panel/tasks.js");
const probe = {
  id: 1,
  kind: "outbound-probe",
  actor: "admin",
  state: "running",
  phase: "executing",
  cancellable: true,
  cancel_requested: false,
  created: 1,
  updated: 1,
  details: { operators: false, actor: "admin" },
};
document
  .querySelector("main")
  .replaceChildren(
    tasksCard(
      [
        probe,
        { ...probe, id: 2, kind: "subscription", cancellable: false },
        { ...probe, id: 3, cancellable: false, cancel_requested: true },
      ],
      ctx,
    ),
  );
const cancelButtons = [...document.querySelectorAll("button")].filter(
  (b) => b.textContent === "停止探测",
);
assert.equal(
  cancelButtons.length,
  1,
  "client ignored server cancellation capability",
);
assert.match(document.querySelector("main").textContent, /正在停止探测/);
cancelButtons[0].click();
await tick();
assert.deepEqual(requests, [{ id: 1 }]);
console.log(
  "Deployment panel checks passed: restart impact, observed state, bound certificates and running probe cancellation.",
);
