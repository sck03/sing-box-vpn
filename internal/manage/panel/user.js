const token = location.hash.slice(1);
history.replaceState(null, "", location.pathname);
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
  const response = await fetch("/u/" + token + "/usage", {
    credentials: "omit",
    cache: "no-store",
  });
  if (!response.ok) throw new Error("授权已过期、撤销或账户不可用。");
  const u = await response.json();
  target.replaceChildren(
    element("h2", u.name),
    element(
      "p",
      "状态：" +
        ({
          active: "正常",
          not_started: "未开始",
          exhausted: "额度耗尽",
          expired: "到期",
          disabled: "禁用",
        }[u.state] || u.state),
    ),
    element(
      "p",
      "已计费 / 额度：" +
        format(u.charged_bytes) +
        " / " +
        format(u.quota_bytes),
    ),
    element("p", "代理到期：" + when(u.expires_at)),
    element("p", "下次额度重置：" + when(u.next_reset)),
    element("p", "下载到期：" + when(u.download_expires_at)),
    element(
      "p",
      "下载次数：" + u.downloads + " / " + (u.max_downloads ?? "不限"),
    ),
  );
  for (const client of u.clients) {
    const wrap = document.createElement("div");
    wrap.className = "card";
    const address = location.origin + "/u/" + token + "/" + client;
    const link = element("a", "下载 " + client);
    link.href = address;
    link.rel = "noreferrer";
    const copy = element("button", "复制订阅链接");
    copy.addEventListener("click", async () => {
      try {
        await navigator.clipboard.writeText(address);
        copy.textContent = "已复制";
      } catch {
        const input = document.createElement("input");
        input.value = address;
        input.readOnly = true;
        wrap.append(input);
        input.select();
      }
    });
    wrap.append(link, document.createTextNode(" "), copy);
    target.append(wrap);
  }
}
load().catch((error) => target.replaceChildren(element("p", error.message)));
