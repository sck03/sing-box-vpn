import assert from "node:assert/strict";
import { JSDOM } from "jsdom";

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
const { api, setCSRF } = await import("../internal/manage/panel/api.js");
const { pageContext } = await import("../internal/manage/panel/lifecycle.js");
const { loadDialog } = await import(
  "../internal/manage/panel/dialog-session.js"
);
const { subscribers } = await import("../internal/manage/panel/subscribers.js");
const { admins } = await import("../internal/manage/panel/admins.js");
const { historyControls } = await import(
  "../internal/manage/panel/log-maintenance.js"
);
const tick = () => new Promise((resolve) => setTimeout(resolve, 0));
const response = (value) => ({ ok: true, json: async () => value });
const command = { operation: "service", service: "sb-core", value: "restart" };
setCSRF("submission-session");
let calls = [],
  fail = true;
globalThis.fetch = async (_path, options) => {
  calls.push(options.headers["Idempotency-Key"]);
  if (fail) throw new TypeError("response lost after acceptance");
  return response({ task_id: 12 });
};
await assert.rejects(api("/api/tasks", command), /response lost/);
fail = false;
await api("/api/tasks", {
  value: "restart",
  service: "sb-core",
  operation: "service",
});
assert(
  calls[0] && calls[0] === calls[1],
  "retry did not preserve its request identity",
);
await api("/api/tasks", command);
assert.notEqual(
  calls[1],
  calls[2],
  "an acknowledged operation blocked a new intent",
);

let attempted;
globalThis.fetch = async (_path, options) => {
  attempted = options.headers["Idempotency-Key"];
  return {
    ok: false,
    status: 503,
    text: async () =>
      JSON.stringify({
        error: {
          code: "temporarily_unavailable",
          message: "retry",
          retryable: true,
        },
      }),
  };
};
await assert.rejects(api("/api/tasks", command));
const uncertain = attempted;
globalThis.fetch = async (_path, options) => {
  assert.equal(options.headers["Idempotency-Key"], uncertain);
  return response({ task_id: 15 });
};
await api("/api/tasks", command);

// Reload restores only hashed bodies and opaque keys; logout discards them.
const ledger = await import("../internal/manage/panel/task-requests.js");
ledger.setTaskRequestSession("reload-session");
const secretBody = JSON.stringify({
  revision: 1,
  config: { password: "do-not-store-secret" },
});
const pending = await ledger.taskRequest("/api/config", secretBody);
assert(
  !window.sessionStorage
    .getItem("sbvpn-task-requests")
    .includes("do-not-store-secret"),
);
const reloaded = await import(
  "../internal/manage/panel/task-requests.js?reload"
);
reloaded.setTaskRequestSession("reload-session");
assert.equal(
  (await reloaded.taskRequest("/api/config", secretBody)).id,
  pending.id,
);
reloaded.setTaskRequestSession("");
reloaded.setTaskRequestSession("new-session");
assert.notEqual(
  (await reloaded.taskRequest("/api/config", secretBody)).id,
  pending.id,
);

// Successful old writes cannot abort a newly loading editor, either after
// navigation or after replacement on the same page, including history policy.
for (const { page, edit } of [
  { page: subscribers, edit: "编辑" },
  { page: admins, edit: "编辑" },
  { page: async (ctx) => [await historyControls(ctx)], edit: "保留策略" },
]) {
  for (const navigated of [false, true]) {
    setCSRF("editor-session-" + page.name + navigated);
    let finishSave,
      finishRead,
      renders = 0;
    let current = new AbortController();
    globalThis.fetch = async (path, options) => {
      if (options.method === "POST")
        return new Promise((resolve) => {
          finishSave = resolve;
        });
      if (path === "/api/tasks/history")
        return response({
          policy: { count: 100, days: 7, result_bytes: 1048576 },
          usage: { completed: 0, pending: 0, result_bytes: 0 },
        });
      if (path === "/api/capabilities")
        return response({ password_bytes: { min: 8, max: 72 } });
      return response([
        {
          id: "other",
          name: "Other",
          role: "admin",
          revision: 1,
          disabled: false,
        },
      ]);
    };
    const base = {
      session: { id: "self", role: "admin" },
      render() {
        renders++;
        current.abort();
      },
    };
    const context = pageContext(base, current.signal);
    document.querySelector("main").replaceChildren(...(await page(context)));
    [...document.querySelectorAll("main button")]
      .find((button) => button.textContent === edit)
      .click();
    document
      .querySelector("dialog form")
      .dispatchEvent(new Event("submit", { cancelable: true }));
    await tick();
    assert(finishSave, "write did not start");
    if (navigated) {
      current.abort();
      current = new AbortController();
    }
    const next = current;
    const loading = loadDialog(
      next.signal,
      "新的编辑器",
      () =>
        new Promise((resolve) => {
          finishRead = resolve;
        }),
      () => {},
    );
    finishSave(response({ ok: true }));
    await tick();
    await tick();
    assert.equal(renders, 0, "late write refreshed the current page");
    assert(!next.signal.aborted, "late write aborted a newer request");
    assert(
      document.querySelector("dialog").open,
      "late write closed a newer dialog",
    );
    finishRead({});
    await loading;
    current.abort();
  }
}
// An enrollment response may not reveal a secret over a newer editor.
for (const navigated of [false, true]) {
  setCSRF("mfa-session-" + navigated);
  let finishEnrollment, finishRead;
  let current = new AbortController();
  globalThis.fetch = async (path, options) => {
    if (path === "/api/mfa/enroll")
      return new Promise((resolve) => {
        finishEnrollment = resolve;
      });
    if (path === "/api/capabilities")
      return response({ password_bytes: { min: 8, max: 72 } });
    return response([]);
  };
  const context = pageContext(
    { session: { id: "self", role: "admin" }, render() {} },
    current.signal,
  );
  document.querySelector("main").replaceChildren(...(await admins(context)));
  [...document.querySelectorAll("main button")]
    .find((button) => button.textContent === "生成认证器密钥")
    .click();
  const form = document.querySelector("dialog form");
  form.querySelector('input[type="password"]').value = "enrollment-password";
  assert(form.checkValidity());
  form.requestSubmit();
  await tick();
  assert(finishEnrollment);
  if (navigated) {
    current.abort();
    current = new AbortController();
  }
  const loading = loadDialog(
    current.signal,
    "新的编辑器",
    () =>
      new Promise((resolve) => {
        finishRead = resolve;
      }),
    () => {},
  );
  finishEnrollment(
    response({ secret: "must-not-cover-new-editor", uri: "otpauth://test" }),
  );
  await tick();
  await tick();
  assert(document.querySelector("dialog").textContent.includes("新的编辑器"));
  assert(
    !document
      .querySelector("dialog")
      .textContent.includes("must-not-cover-new-editor"),
  );
  assert(document.querySelector("#notice").textContent.includes("重新生成"));
  finishRead({});
  await loading;
  current.abort();
}
// A stale page's generic render callback is also inert, even without a dialog.
let renders = 0;
const stopped = new AbortController();
const view = pageContext(
  {
    render() {
      renders++;
    },
  },
  stopped.signal,
);
stopped.abort();
view.render();
assert.equal(renders, 0);
setCSRF("");
dom.window.close();
console.log(
  "Panel submissions passed: retry identity, reload, logout and stale editor ownership.",
);
