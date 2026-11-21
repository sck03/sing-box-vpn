import { api } from "./api.js";
import { accountOutputDialog } from "./account-outputs.js";
import { $, el, button, row, help, notice } from "./ui.js";

// Link limits can narrow the entitlement; they never extend its time or quota.
export async function createAccountShare(accounts, ctx) {
  accounts = accounts.filter(
    (a) => !a.download_denial && a.download_scopes?.length,
  );
  if (!accounts.length) throw new Error("请先创建权益并绑定入站。");
  const request = ctx.workflowRequest || api;
  await accountOutputDialog({
    accounts,
    ctx,
    title: "创建账户分享",
    multiple: true,
    fields: [
      { key: "expires_at", label: "链接到期时间", type: "date", advanced: true,
        help: "留空则跟随权益；设置后仍受权益有效期限制。" },
      { key: "max_downloads", label: "最多下载次数", type: "number", min: 1,
        nullable: true, advanced: true, help: "留空不限次数；达到限制后链接停止下载。" },
    ],
    defaults: {
      id: "share-" + crypto.randomUUID().replaceAll("-", ""),
      account_id: accounts[0].id,
      scope: "",
      profiles: ["uri", "base64", "mihomo-provider", "sing-box-nodes"],
      expires_at: null,
      max_downloads: null,
    },
    save: async (value) => {
      const result = await request("/api/grants", value);
      const dialog = $("#dialog");
      const address = el("input", { "aria-label": "分享地址", readonly: true });
      address.value = result.user_url;
      dialog.replaceChildren(
        el("h2", {}, "分享已创建"),
        help(
          "请保存这个地址，之后无法再次查看完整链接。链接限制与权益有效期、流量同时生效；可在「用户与分享 → 分享链接」撤销。",
        ),
        address,
        row(
          button(
            "复制分享地址",
            async () => {
              try {
                await navigator.clipboard.writeText(address.value);
                notice("分享地址已复制");
              } catch {
                address.focus();
                address.select();
                notice("请复制选中的分享地址");
              }
            },
            "primary",
          ),
          button("完成", () => dialog.close()),
          el(
            "a",
            { href: "#grants", onclick: () => dialog.close() },
            "管理分享链接 →",
          ),
        ),
      );
      dialog.showModal();
      ctx.render();
    },
  });
}
