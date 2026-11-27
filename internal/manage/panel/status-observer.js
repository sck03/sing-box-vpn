/** @typedef {import('./contracts.js').OverviewSnapshot} OverviewSnapshot */
/** @typedef {{signal?: AbortSignal, overview?: OverviewSnapshot, activeSection?: string,
 * section?: string, updateOverview?: ((snapshot: OverviewSnapshot) => void) | null,
 * sectionChanged?: (section: string) => void}} StatusContext */

// Resource invalidation is independent of the bounded task list. A visible-page
// fallback also covers clock expiry and changes made by another manager process.
/** @param {StatusContext} ctx @param {() => Promise<void>} read
 * @param {{section?: string}} [options] */
export function observeStatus(ctx, read, { section } = {}) {
  const signature = (/** @type {OverviewSnapshot | undefined} */ snapshot) =>
    JSON.stringify([
      snapshot?.resource_version,
      snapshot?.revision,
      snapshot?.applied_revision,
      snapshot?.task_counts,
    ]);
  let version = signature(ctx.overview);
  let visible =
    !section || (ctx.activeSection || ctx.section || section) === section;
  let queued = false,
    stopped = false;
  /** @type {Promise<void> | undefined} */
  let pending;
  const active = () =>
    !stopped && visible && !document.hidden && !ctx.signal?.aborted;
  function refresh() {
    if (stopped) return Promise.resolve();
    queued = true;
    if (pending) return pending;
    if (!active()) return Promise.resolve();
    pending = (async () => {
      while (queued && active()) {
        queued = false;
        await read();
      }
    })().finally(() => {
      pending = undefined;
    });
    return pending;
  }
  const updateOverview = ctx.updateOverview;
  const update = (/** @type {OverviewSnapshot} */ snapshot) => {
    updateOverview?.(snapshot);
    const next = signature(snapshot);
    if (next === version) return;
    version = next;
    void refresh();
  };
  ctx.updateOverview = update;
  const sectionChanged = ctx.sectionChanged;
  const changeSection = (/** @type {string} */ next) => {
    sectionChanged?.(next);
    const wasVisible = visible;
    visible = next === section;
    if (visible && !wasVisible) void refresh();
  };
  if (section) {
    ctx.sectionChanged = changeSection;
  }
  const visibility = () => {
    if (active()) void refresh();
  };
  const timer = window.setInterval(visibility, 60000);
  document.addEventListener("visibilitychange", visibility);
  const stop = () => {
    if (stopped) return;
    stopped = true;
    queued = false;
    window.clearInterval(timer);
    document.removeEventListener("visibilitychange", visibility);
    ctx.signal?.removeEventListener("abort", stop);
    if (ctx.updateOverview === update) ctx.updateOverview = updateOverview;
    if (section && ctx.sectionChanged === changeSection)
      ctx.sectionChanged = sectionChanged;
  };
  if (ctx.signal?.aborted) stop();
  else ctx.signal?.addEventListener("abort", stop, { once: true });
  return { refresh, dispose: stop };
}
