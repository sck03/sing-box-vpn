// Keep the fragment for reloads; it is never sent in HTTP requests or referrers.
const base = new URL("./", location.href);
const target = document.querySelector("#user-content");
const format = (n) =>
  n === null ? "不限" : (Number(n) / 1073741824).toFixed(2) + " GiB";
const when = (t) => (t ? new Date(t).toLocaleString() : "不限");
function element(tag, text) {
  const node = document.createElement(tag);
  node.textContent = text;
  return node;
}
async function load(token, signal, current) {
  if (!/^[a-f0-9]{64}$/.test(token))
    throw new Error("请使用包含完整授权令牌的分享地址。");
  const response = await fetch(new URL("u/" + token + "/usage", base), {
    credentials: "omit",
    cache: "no-store",
    signal: AbortSignal.any([signal, AbortSignal.timeout(15000)]),
  });
  if (!response.ok) {
    const error = new Error(
      response.status === 404 || response.status === 403
        ? "授权已过期、撤销或权益不可用。"
        : "服务暂时不可用，请稍后刷新。",
    );
    throw Object.assign(error, { status: response.status });
  }
  const u = await response.json();
  if (!current()) return;
  const status = element(
    "span",
    {
      active: "正常",
      not_started: "尚未开始",
      exhausted: "额度耗尽",
      expired: "已到期",
      disabled: "已停用",
    }[u.state] || u.state,
  );
  status.className = "tag " + (u.state === "active" ? "good" : "");
  const heading = element("div", "");
  heading.className = "row";
  heading.append(element("h2", u.name), status);
  const usage = element("div", "");
  usage.className = "usage-summary";
  usage.append(
    element("small", "本期已计费 / 总额度"),
    element("strong", format(u.charged_bytes) + " / " + format(u.quota_bytes)),
  );
  if (u.quota_bytes !== null && u.quota_bytes > 0) {
    const progress = element("progress", "");
    progress.max = u.quota_bytes;
    progress.value = Math.min(u.charged_bytes, u.quota_bytes);
    progress.setAttribute("aria-label", "本期额度使用比例");
    usage.append(progress);
  }
  const details = element("dl", "");
  details.className = "detail-grid";
  for (const [label, value] of [
    ["上传 / 下载", format(u.upload_bytes) + " / " + format(u.download_bytes)],
    [
      "权益开始",
      u.started_at
        ? when(u.started_at)
        : u.start_mode === "first_use"
          ? "首次成功代理连接时起算"
          : "尚未开始",
    ],
    ["代理到期", when(u.effective_expires_at)],
    ["额度重置", when(u.next_reset)],
    ["下载到期", when(u.download_expires_at)],
    ["已下载次数", u.downloads + " / " + (u.max_downloads ?? "不限")],
  ])
    details.append(element("dt", label), element("dd", value));
  target.replaceChildren(heading, usage, details);
  if (u.download_denial) {
    target.append(
      element(
        "p",
        {
          download_limit:
            "下载次数已用完。已导入的配置按权益额度和有效期继续使用。",
          no_bound_inbound:
            "此链接对应的入站已解绑或停用。请联系管理员重新绑定。",
          quota_exhausted: "本期流量额度已耗尽，额度恢复后可再次下载。",
        }[u.download_denial] || "权益暂不可下载。",
      ),
    );
    return;
  }
  target.append(
    element("h2", "添加到客户端"),
    element("p", "选择对应客户端，复制订阅链接导入，或直接下载配置。"),
  );
  const grid = element("div", "");
  grid.className = "client-grid";
  target.append(grid);
  for (const profile of u.clients) {
    const client = profile.client;
    const label = profile.label;
    const wrap = document.createElement("div");
    wrap.className = "client-download";
    const address = new URL("u/" + token + "/" + client, base).href;
    wrap.append(
      element("h3", label),
      element(
        "small",
        profile.experimental
          ? "实验适配 · 需要目标应用验收"
          : profile.reference_version
            ? "参考核心版本 " + profile.reference_version
            : "通用节点格式",
      ),
    );
    wrap.append(
      element(
        "p",
        `${profile.nodes}/${profile.input_nodes} 个可用节点${profile.skipped ? ` · 跳过 ${profile.skipped} 个不兼容节点` : ""}`,
      ),
    );
    if (profile.diagnostics?.some((diagnostic) => diagnostic.stage === "tls"))
      wrap.append(
        element(
          "p",
          "此格式存在证书导入限制，建议优先使用 sing-box 或 Mihomo 原生配置。",
        ),
      );
    if (!profile.available) {
      wrap.append(
        element(
          "p",
          profile.reason || "当前绑定与下载模板不兼容，请联系管理员。",
        ),
      );
      grid.append(wrap);
      continue;
    }
    const link = element("a", "下载 " + label);
    link.href = address;
    link.rel = "noreferrer";
    link.className = "download-link";
    const copy = element("button", "复制订阅链接");
    copy.addEventListener("click", async () => {
      try {
        await navigator.clipboard.writeText(address);
        copy.textContent = "已复制";
      } catch {
        const input =
          wrap.querySelector("input") || document.createElement("input");
        input.value = address;
        input.readOnly = true;
        input.setAttribute("aria-label", client + " 订阅链接");
        wrap.append(input);
        input.focus();
        input.select();
      }
    });
    wrap.append(link, document.createTextNode(" "), copy);
    grid.append(wrap);
  }
}
let loading = false,
  generation = 0,
  request;
const refresh = /** @type {HTMLButtonElement} */ (document.querySelector("#user-refresh")),
  updated = document.querySelector("#user-updated");
async function reloadUsage(force = false) {
  if (loading && !force) return;
  const current = ++generation;
  request?.abort();
  request = new AbortController();
  loading = true;
  if (refresh) refresh.disabled = true;
  try {
    await load(
      location.hash.slice(1),
      request.signal,
      () => current === generation,
    );
    if (current !== generation) return;
    if (updated)
      updated.textContent = "更新于 " + new Date().toLocaleTimeString();
  } catch (error) {
    if (current !== generation || error.name === "AbortError") return;
    if ([401, 403, 404].includes(error.status))
      target.replaceChildren(element("p", error.message));
    if (updated) updated.textContent = error.message;
    else target.replaceChildren(element("p", error.message));
  } finally {
    if (current === generation) {
      loading = false;
      if (refresh) refresh.disabled = false;
    }
  }
}
refresh?.addEventListener("click", () => reloadUsage());
window.addEventListener("hashchange", () => {
  target.replaceChildren(element("p", "正在读取…"));
  void reloadUsage(true);
});
document.addEventListener("visibilitychange", () => {
  if (!document.hidden) void reloadUsage();
});
void reloadUsage();
