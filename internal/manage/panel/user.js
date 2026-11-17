const token = location.hash.slice(1);
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
async function load() {
  if (!/^[a-f0-9]{64}$/.test(token))
    throw new Error("请使用包含完整授权令牌的分享地址。");
  const response = await fetch(new URL("u/" + token + "/usage", base), {
    credentials: "omit",
    cache: "no-store",
  });
  if (!response.ok) throw new Error("授权已过期、撤销或账户不可用。");
  const u = await response.json();
  const status = element(
    "span",
    {
      active: "正常",
      not_started: "待首次使用",
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
    ["代理到期", when(u.expires_at)],
    ["额度重置", when(u.next_reset)],
    ["下载到期", when(u.download_expires_at)],
    ["已下载次数", u.downloads + " / " + (u.max_downloads ?? "不限")],
  ])
    details.append(element("dt", label), element("dd", value));
  target.replaceChildren(
    heading,
    usage,
    details,
    element("h2", "添加到客户端"),
    element("p", "选择对应客户端，复制订阅链接导入，或直接下载配置。"),
  );
  if (u.max_downloads !== null && u.downloads >= u.max_downloads) {
    target.append(
      element(
        "p",
        "下载次数已用完。用量仍可查询；已导入的配置按权益额度和有效期继续使用。",
      ),
    );
    return;
  }
  for (const client of u.clients) {
    const wrap = document.createElement("div");
    wrap.className = "client-download";
    const address = new URL("u/" + token + "/" + client, base).href;
    const link = element("a", "下载 " + client);
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
    target.append(wrap);
  }
}
load().catch((error) => target.replaceChildren(element("p", error.message)));
