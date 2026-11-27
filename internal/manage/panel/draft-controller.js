/** @typedef {import('./contracts.js').PlatformConfig} PlatformConfig */
/** @typedef {{config: PlatformConfig, revision: number, generation: number}} DraftSnapshot */

// Configuration values are JSON data. Visit each value once rather than
// serializing every parent subtree while calculating a preview diff.
/** @param {unknown} a @param {unknown} b @returns {boolean} */
export function equalConfiguration(a, b) {
  if (a === b) return true;
  if (!a || !b || typeof a !== "object" || typeof b !== "object") return false;
  if (Array.isArray(a) || Array.isArray(b))
    return (
      Array.isArray(a) &&
      Array.isArray(b) &&
      a.length === b.length &&
      a.every((value, index) => equalConfiguration(value, b[index]))
    );
  const left = /** @type {Record<string, unknown>} */ (a);
  const right = /** @type {Record<string, unknown>} */ (b);
  const keys = Object.keys(left).filter((key) => left[key] !== undefined);
  return (
    keys.length ===
      Object.keys(right).filter((key) => right[key] !== undefined).length &&
    keys.every(
      (key) =>
        Object.hasOwn(right, key) && equalConfiguration(left[key], right[key]),
    )
  );
}

/** @param {unknown} before @param {unknown} after @returns {string[]} */
export function configurationChanges(before, after) {
  const paths = /** @type {string[]} */ ([]);
  /** @param {unknown} a @param {unknown} b @param {string} path */
  function visit(a, b, path) {
    if (a === b) return;
    if (
      a &&
      b &&
      typeof a === "object" &&
      typeof b === "object" &&
      !Array.isArray(a) &&
      !Array.isArray(b)
    ) {
      const left = /** @type {Record<string, unknown>} */ (a);
      const right = /** @type {Record<string, unknown>} */ (b);
      for (const key of new Set([...Object.keys(left), ...Object.keys(right)]))
        visit(left[key], right[key], path + "/" + key);
    } else if (!equalConfiguration(a, b)) paths.push(path);
  }
  visit(before, after, "");
  return paths;
}

/** @param {() => Promise<import('./contracts.js').ConfigurationState>} read */
export function createDraftController(read) {
  let generation = 0,
    revision = 0,
    root = "",
    dirty = false;
  let current = /** @type {PlatformConfig | null} */ (null);
  let baseline = /** @type {PlatformConfig | null} */ (null);
  let pending = /** @type {Promise<PlatformConfig> | null} */ (null);
  return {
    get dirty() {
      return dirty;
    },
    get root() {
      return root;
    },
    get revision() {
      return revision;
    },
    get loaded() {
      return current !== null;
    },
    async load() {
      if (current) return current;
      const version = generation;
      pending ??= read()
        .then((value) => {
          if (version !== generation)
            throw new DOMException("草稿已重新载入", "AbortError");
          current = value.config;
          baseline = structuredClone(current);
          revision = value.revision;
          root = value.root;
          return current;
        })
        .finally(() => {
          if (version === generation) pending = null;
        });
      return pending;
    },
    reset() {
      generation++;
      current = baseline = null;
      pending = null;
      revision = 0;
      root = "";
      dirty = false;
    },
    changed() {
      dirty = true;
    },
    /** @param {PlatformConfig} config */
    replace(config) {
      current = config;
      dirty = true;
    },
    snapshot() {
      if (!current) throw new Error("没有可保存或校验的配置");
      return { config: structuredClone(current), revision, generation };
    },
    /** @param {DraftSnapshot} snapshot */
    active(snapshot) {
      return generation === snapshot.generation;
    },
    /** @param {DraftSnapshot} snapshot */
    changes(snapshot) {
      return configurationChanges(baseline, snapshot.config);
    },
    /** @param {DraftSnapshot} snapshot */
    saved(snapshot) {
      if (generation !== snapshot.generation) return false;
      revision = snapshot.revision + 1;
      baseline = snapshot.config;
      dirty = !equalConfiguration(current, baseline);
      return true;
    },
  };
}

// Candidate reads belong to their editor and current draft. The snapshot is
// also the exact candidate seen by the backend allocator.
/** @template T
 * @param {{config: () => Promise<PlatformConfig>}} context
 * @param {(config: PlatformConfig, signal?: AbortSignal) => Promise<T>} prepare
 * @param {(config: PlatformConfig, result: T) => void} commit
 * @param {AbortSignal} [signal]
 * @param {() => boolean} [active]
 * @param {PlatformConfig} [expected] */
export async function prepareDraft(
  context,
  prepare,
  commit,
  signal,
  active = () => true,
  expected,
) {
  signal?.throwIfAborted();
  const current = await context.config();
  signal?.throwIfAborted();
  if (!active() || (expected && current !== expected))
    throw new DOMException("编辑窗口或草稿已更改", "AbortError");
  const submitted = structuredClone(current);
  const result = await prepare(submitted, signal);
  signal?.throwIfAborted();
  if (!active() || current !== (await context.config()))
    throw new DOMException("编辑窗口或草稿已更改", "AbortError");
  signal?.throwIfAborted();
  if (!active()) throw new DOMException("编辑窗口已关闭", "AbortError");
  if (!equalConfiguration(current, submitted))
    throw new Error("准备期间草稿发生变化，请重试。");
  commit(current, result);
}
