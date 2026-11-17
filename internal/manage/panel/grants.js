import { api } from "./api.js";
import { createAccountShare } from "./sharing.js";
import { card, help, button, remoteTable, date, badge } from "./ui.js";
export async function grants(ctx) {
  const request = ctx.request || api;
  const accounts = await request("/api/account-options");
  return [
    card(
      "下载授权",
      help(
        "下载令牌只决定是否能够获取配置。代理到期、流量限额与凭据轮换由权益管理。创建后令牌只展示一次，之后只能撤销重建。",
      ),
      button(
        "创建账户分享",
        () => createAccountShare(accounts, ctx),
        "primary",
      ),
      await remoteTable(
        [
          { key: "id", label: "授权 ID" },
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
