import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { JSDOM } from "jsdom";
import { capabilitiesFixture } from "./panel-fixtures.mjs";
import {
  createDraftController,
  configurationChanges,
  equalConfiguration,
} from "../internal/manage/panel/draft-controller.js";

const deferred = () => {
  let resolve;
  const promise = new Promise((done) => {
    resolve = done;
  });
  return { promise, resolve };
};
const initial = {
  schema: 8,
  app: { name: "Fixture", address: "example.com", nodes: [] },
};
let loads = 0;
const loading = deferred();
const drafts = createDraftController(() => {
  loads++;
  return loading.promise;
});
const first = drafts.load(),
  second = drafts.load();
assert.equal(loads, 1);
loading.resolve({
  config: structuredClone(initial),
  revision: 7,
  root: "/fixture",
});
assert.equal(await first, await second);
const current = await drafts.load();
current.app.name = "Submitted";
drafts.changed();
const submitted = drafts.snapshot();
assert.deepEqual(drafts.changes(submitted), ["/app/name"]);
current.app.name = "Edited during save";
assert(drafts.saved(submitted));
assert.equal(drafts.revision, 8);
assert(drafts.dirty);
drafts.reset();
assert.equal(drafts.saved(submitted), false);
assert.equal(drafts.revision, 0);
const stale = deferred();
const reset = createDraftController(() => stale.promise);
const rejected = assert.rejects(reset.load(), { name: "AbortError" });
reset.reset();
stale.resolve({ config: structuredClone(initial), revision: 1, root: "" });
await rejected;
assert(equalConfiguration({ a: 1, optional: undefined }, { a: 1 }));
assert(!equalConfiguration([1, 2], [2, 1]));
assert.deepEqual(
  configurationChanges(
    { a: { b: 1 }, nodes: [1] },
    { a: { b: 2 }, nodes: [2] },
  ),
  ["/a/b", "/nodes"],
);

const dom = new JSDOM(
  await readFile(
    new URL("../internal/manage/panel/index.html", import.meta.url),
    "utf8",
  ),
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
const { inbounds } = await import("../internal/manage/panel/network.js");
const { accounts } = await import("../internal/manage/panel/accounts.js");
const { formDialog } = await import("../internal/manage/panel/form-dialog.js");
const { setCSRF } = await import("../internal/manage/panel/api.js");
const tick = () => new Promise((resolve) => setTimeout(resolve, 0));
const dialog = document.querySelector("#dialog"),
  host = document.querySelector("#content");
const click = (text, root = document) => {
  const button = [...root.querySelectorAll("button")].find(
    (value) => value.textContent === text,
  );
  assert(button, text);
  button.click();
};
const submit = () =>
  dialog
    .querySelector("form")
    .dispatchEvent(new Event("submit", { cancelable: true }));
const another = () =>
  formDialog(
    "另一个编辑器",
    [{ key: "name", label: "新草稿" }],
    { name: "保留" },
    () => {},
  );
globalThis.fetch = async (path) => {
  assert.equal(path, "/api/capabilities");
  return { ok: true, json: async () => capabilitiesFixture };
};

try {
  setCSRF("draft-tests");
  for (const mode of ["cancel", "navigate", "edit", "reload", "success"]) {
    const page = new AbortController(),
      response = deferred();
    const original = structuredClone(initial);
    original.app.nodes = [
      { id: "existing", protocol: "vless", port: 24000, tls: {} },
    ];
    let config = original,
      changes = 0,
      sent,
      signal;
    const ctx = {
      config: async () => config,
      signal: page.signal,
      session: { role: "admin" },
      render() {},
      changed() {
        changes++;
      },
      request(path, body, options) {
        assert.equal(path, "/api/preset");
        sent = body;
        signal = options.signal;
        return response.promise;
      },
    };
    host.replaceChildren(...(await inbounds(ctx)));
    click("编辑", host);
    assert(dialog.querySelector('[aria-label="入站 ID"]').disabled);
    dialog.close();
    click("复制", host);
    assert.equal(
      dialog.querySelector('[aria-label="入站 ID"]').disabled,
      false,
    );
    dialog.close();
    click("新增入站", host);
    submit();
    await tick();
    assert.equal(sent.config.app.nodes[0].port, 24000);
    assert.notEqual(sent.config, original);
    if (mode === "cancel") {
      click("取消", dialog);
      another();
    }
    if (mode === "navigate") {
      page.abort();
      another();
    }
    if (mode === "edit") config.app.name = "changed";
    if (mode === "reload") config = structuredClone(initial);
    response.resolve({ id: "added", protocol: "vless", port: 24001, tls: {} });
    await tick();
    assert.equal(changes, mode === "success" ? 1 : 0, mode);
    assert.equal(original.app.nodes.length, mode === "success" ? 2 : 1, mode);
    if (mode === "cancel" || mode === "navigate") {
      assert(signal.aborted);
      assert.equal(dialog.querySelector("h2").textContent, "另一个编辑器");
    }
    if (mode === "edit")
      assert.match(
        dialog.querySelector(".form-error").textContent,
        /草稿发生变化/,
      );
    page.abort();
    dialog.close();
  }

  for (const stage of ["save", "queue", "logout"]) {
    setCSRF("account-" + stage);
    const page = new AbortController(),
      save = deferred(),
      queue = deferred();
    let renders = 0,
      writes = 0;
    const request = async (path) => {
      if (path === "/api/capabilities") return capabilitiesFixture;
      if (path === "/api/subscribers") return [];
      if (path.startsWith("/api/accounts?")) return { items: [], total: 0 };
      if (path.startsWith("/api/inbound-options"))
        return stage === "queue" && path.endsWith("normal")
          ? [{ id: "node", name: "Node" }]
          : [];
      if (path === "/api/accounts") {
        writes++;
        return save.promise;
      }
      throw Error(path);
    };
    host.replaceChildren(
      ...(await accounts({
        signal: page.signal,
        session: { role: "admin" },
        request,
        workflowRequest: request,
        render() {
          renders++;
        },
        task: () => queue.promise,
      })),
    );
    click("新建权益", host);
    await tick();
    dialog.querySelector('[aria-label="名称"]').value = "Late account";
    submit();
    await tick();
    assert.equal(writes, 1);
    if (stage === "queue") {
      save.resolve({ ok: true });
      await tick();
    }
    dialog.close();
    another();
    page.abort();
    if (stage === "logout") setCSRF("");
    if (stage === "queue") queue.resolve(42);
    else save.resolve({ ok: true });
    await tick();
    assert.equal(renders, 0, "late workflow refreshed a new page");
    assert.equal(dialog.querySelector("h2").textContent, "另一个编辑器");
    const results = document.querySelector("#workflow-results");
    assert.equal(results.children.length, stage === "logout" ? 0 : 1);
    if (stage === "save") {
      click("查看权益结果", results);
      assert.equal(dialog.querySelector("h2").textContent, "权益已创建");
      click("稍后设置", dialog);
      assert.equal(results.children.length, 0);
    }
  }
  console.log(
    "Draft lifecycle checks passed: snapshots, candidate cancellation, stable IDs and retained account results.",
  );
} finally {
  setCSRF("");
  dialog.close();
  dom.window.close();
}
