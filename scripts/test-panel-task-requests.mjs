import assert from "node:assert/strict";
import { webcrypto } from "node:crypto";

// Exercise the receipt boundary without a browser, timers or storage dependency.
const values = new Map();
globalThis.window = {
  sessionStorage: {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
    removeItem: (key) => values.delete(key),
  },
};
const ledger = await import("../internal/manage/panel/task-requests.js");
const path = "/api/tasks";
const body = JSON.stringify({
  operation: "service",
  service: "sb-core",
  value: "restart",
});
const reordered = JSON.stringify({
  value: "restart",
  operation: "service",
  service: "sb-core",
});
const storageKey = "sbvpn-task-requests";
const set = (data) => values.set(storageKey, JSON.stringify(data));
const saved = () => JSON.parse(values.get(storageKey));

// One malformed neighbour must not erase an accepted request after a reload.
ledger.setTaskRequestSession("storage-session");
const original = await ledger.taskRequest(path, body);
const initial = saved();
set({ ...initial, entries: [null, ...initial.entries, false, ["bad"], {}] });
const restored = await import(
  "../internal/manage/panel/task-requests.js?storage"
);
restored.setTaskRequestSession("storage-session");
assert.equal((await restored.taskRequest(path, body)).id, original.id);
assert.equal((await restored.taskRequest(path, reordered)).id, original.id);

// Delay hashing deterministically while another response acknowledges the same
// operation. A later explicit operation must still receive its own identity.
ledger.setTaskRequestSession("concurrent-session");
const first = await ledger.taskRequest(path, body);
const nativeDigest = webcrypto.subtle.digest;
const nativeCrypto = Object.getOwnPropertyDescriptor(globalThis, "crypto");
let releases = [],
  started;
const hashesStarted = new Promise((resolve) => {
  started = resolve;
});
Object.defineProperty(globalThis, "crypto", {
  configurable: true,
  value: {
    randomUUID: () => webcrypto.randomUUID(),
    subtle: {
      async digest(...args) {
        const result = await nativeDigest.apply(webcrypto.subtle, args);
        await new Promise((resolve) => {
          releases.push(resolve);
          if (releases.length === 2) started();
        });
        return result;
      },
    },
  },
});
let delayed;
try {
  delayed = ledger.taskRequest(path, reordered);
  await hashesStarted;
  ledger.settleTaskRequest(first);
} finally {
  Object.defineProperty(globalThis, "crypto", nativeCrypto);
}
const next = await ledger.taskRequest(path, body);
assert.notEqual(next.id, first.id);
for (const release of releases) release();
const overlapping = await delayed;
assert.equal(
  overlapping.id,
  first.id,
  "acknowledgement changed an in-flight retry's identity",
);
ledger.settleTaskRequest(overlapping);
assert.equal(
  (await ledger.taskRequest(path, body)).id,
  next.id,
  "old completion removed a newer request",
);
ledger.settleTaskRequest(next);

// Expired local receipts require explicit acknowledgement before a new intent.
ledger.setTaskRequestSession("expiry-session");
const expiring = await ledger.taskRequest(path, body);
const old = saved();
old.entries[0][1].created = Date.now() - 25 * 60 * 60 * 1000;
set(old);
const expired = await import(
  "../internal/manage/panel/task-requests.js?expiry"
);
expired.setTaskRequestSession("expiry-session");
await assert.rejects(expired.taskRequest(path, body), /超过重试保留期/);
assert.notEqual((await expired.taskRequest(path, body)).id, expiring.id);
expired.setTaskRequestSession("");
assert.equal(values.has(storageKey), false);

console.log(
  "Task receipt checks passed: partial storage corruption, concurrent acknowledgement, stale completion and expiry.",
);
