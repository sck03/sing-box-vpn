import { api } from "./api.js";
import { createAccountShare } from "./sharing.js";
import { card, help, button, remoteTable, date, badge } from "./ui.js";
export async function grants(ctx) {
  const request = ctx.request || api;
  const accounts = await request("/api/account-options");
  return [
    card(
      "分享链接",
      help(
        "直接选择权益创建订阅链接，期限和流量在上方统一管理。完整链接只展示一次，请妥善保存；撤销可阻止后续下载。高级输出和已有链接的下载限制也在此列出。",
      ),
      button(
        "创建账户分享",
        () => createAccountShare(accounts, ctx),
        "primary",
      ),
      await remoteTable(
        [
          { key: "id", label: "链接 ID" },
          {
            label: "权益 / 输出",
            value: (g) => g.account_id || (g.outputs || []).join(", "),
          },
          {
            label: "下载次数",
            value: (g) => `${g.downloads} / ${g.max_downloads ?? "不限"}`,
          },
          { label: "下载到期", value: (g) => date(g.expires_at) },
          {
            label: "状态",
            value: (g) =>
              ({
                active: "有效",
                revoked: "已撤销",
                expired: "已到期",
                exhausted: "次数用尽",
              })[g.state] || "未知",
            render: (g) =>
              badge(
                {
                  active: "有效",
                  revoked: "已撤销",
                  expired: "已到期",
                  exhausted: "次数用尽",
                }[g.state] || "未知",
                g.state === "active" ? "good" : "warn",
              ),
          },
        ],
        (params) => request("/api/grants?" + new URLSearchParams(params)),
        (g) => [
          button(
            "撤销",
            async () => {
              if (
                confirm(
                  "撤销后此链接不能再下载。已经下载的代理配置仍受权益限制。",
                )
              ) {
                await api("/api/grants/revoke", { id: g.id });
                ctx.render();
              }
            },
            "danger",
          ),
        ],
        { signal: ctx.signal },
      ),
    ),
  ];
}
