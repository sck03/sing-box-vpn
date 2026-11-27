// Keep only opaque request receipts, never configuration, passwords or URLs.
// Uncertain submissions survive a reload in this tab's authenticated session.
const storageKey = "sbvpn-task-requests";
const paths = new Set(["/api/tasks", "/api/config", "/api/panel/restart"]);
const lifetime = 24 * 60 * 60 * 1000;
/** @typedef {{id: string, created: number}} Entry */
/** @typedef {{key: string, id: string, generation: number}} Request */
/** @type {Map<string, Entry>} */
let entries = new Map();
let session = "",
  owner = "",
  generation = 0,
  restored = false;

function storage() {
  try {
    return globalThis.window?.sessionStorage;
  } catch {
    return undefined;
  }
}
function persist() {
  try {
    storage()?.setItem(
      storageKey,
      JSON.stringify({ owner, entries: [...entries] }),
    );
  } catch {}
}
/** @param {string} value */
export function setTaskRequestSession(value) {
  if (session !== value || !value) {
    session = value;
    generation++;
    entries = new Map();
    restored = false;
  }
  if (!value) {
    try {
      storage()?.removeItem(storageKey);
    } catch {}
  }
}
/** @param {string} value */
async function digest(value) {
  const hash = await crypto.subtle.digest(
    "SHA-256",
    new TextEncoder().encode(value),
  );
  return Array.from(new Uint8Array(hash), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
}
/** @param {string} identity */
function restore(identity) {
  if (restored) return;
  owner = identity;
  restored = true;
  try {
    const saved = JSON.parse(storage()?.getItem(storageKey) || "null");
    if (
      saved?.owner !== owner ||
      !Array.isArray(saved.entries) ||
      saved.entries.length > 128
    )
      return;
    for (const value of saved.entries) {
      if (!Array.isArray(value) || value.length !== 2) continue;
      const [key, entry] = value;
      if (
        typeof key === "string" &&
        /^[a-f0-9]{64}$/.test(key) &&
        typeof entry?.id === "string" &&
        /^[A-Za-z0-9_-]{16,128}$/.test(entry.id) &&
        Number.isSafeInteger(entry.created) &&
        entry.created > 0
      )
        entries.set(key, { id: entry.id, created: entry.created });
    }
  } catch {
    entries.clear();
  }
}

/** @param {string} path @param {string | undefined} body
 * @returns {Promise<Request> | undefined} */
export function taskRequest(path, body) {
  if (!paths.has(path) || body === undefined) return;
  return prepareTaskRequest(path, body);
}

/** @param {string} path @param {string} body @returns {Promise<Request>} */
async function prepareTaskRequest(path, body) {
  const current = generation;
  // Settlement replaces the map. A retry already hashing keeps the identity
  // that was pending when it started, even if another response confirms it.
  const pending = entries;
  // Object key order does not make a retry a new intent.
  const canonical = JSON.stringify(JSON.parse(body), (_key, value) =>
    value && typeof value === "object" && !Array.isArray(value)
      ? Object.fromEntries(
          Object.keys(value)
            .sort()
            .map((key) => [key, value[key]]),
        )
      : value,
  );
  const [identity, key] = await Promise.all([
    digest(session),
    digest(path + "\n" + canonical),
  ]);
  if (current !== generation)
    throw new DOMException("会话已更改", "AbortError");
  restore(identity);
  let entry = pending.get(key) || entries.get(key);
  if (entry && Date.now() - entry.created >= lifetime) {
    if (entries.get(key) === entry) discardEntry(key);
    throw new Error(
      "上次提交已超过重试保留期，请先核对任务结果；再次提交将创建新操作。",
    );
  }
  if (!entry) {
    if (entries.size >= 128)
      throw new Error("待确认的任务请求过多，请核对任务历史后重新登录。");
    entry = { id: crypto.randomUUID(), created: Date.now() };
    entries.set(key, entry);
    persist();
  }
  return { key, id: entry.id, generation: current };
}

/** @param {Request | undefined} request */
export function settleTaskRequest(request) {
  if (
    !request ||
    request.generation !== generation ||
    entries.get(request.key)?.id !== request.id
  )
    return;
  discardEntry(request.key);
}

/** @param {string} key */
function discardEntry(key) {
  entries = new Map(entries);
  entries.delete(key);
  persist();
}
