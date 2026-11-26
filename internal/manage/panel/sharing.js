import { api, sessionSignal } from "./api.js";
import { createShare } from "./account-api.js";
import { accountOutputDialog } from "./account-outputs.js";
import { $, el, button, row, help, notice } from "./ui.js";
import { retainDialogResult } from "./workflow-results.js";

// Link limits can narrow the entitlement; they never extend its time or quota.
export async function createAccountShare(accounts, ctx) {
  accounts = accounts.filter(
    (a) => !a.download_denial && a.download_scopes?.length,
  );
  if (!accounts.length) throw new Error("请先创建权益并绑定入站。");
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
    save: (value, active) => createShareResult(
      value, ctx, active,
      accounts.find((account) => account.id === value.account_id)?.name || value.account_id,
    ),
  });
}

// Account and compiled-output shares have the same result ownership rules.
export async function createShareResult(value, ctx, active, name = value.id) {
  const signal = sessionSignal();
  const result = await createShare(value, ctx.workflowRequest || api);
  signal.throwIfAborted();
  const retained = retainDialogResult(
    `${name}：分享已创建，请保存地址`,
    (clear) => {
      const dialog = $("#dialog");
      const urls = result.user_url ? [result.user_url] : result.urls;
      const address = el(urls.length > 1 ? "textarea" : "input", {
        "aria-label": "分享地址", readonly: true,
      });
      address.value = urls.join("\n");
      dialog.replaceChildren(
        el("h2", {}, "分享已创建"),
        help(
          "请保存这个地址。点击完成后无法再次查看完整链接；关闭窗口后仍可从页面上方查看，退出登录后清除。可在「用户与分享 → 分享链接」撤销。",
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
          button("完成", () => {
            clear();
            dialog.close();
            dialog.replaceChildren();
          }),
          el(
            "a",
            { href: "#grants", onclick: () => dialog.close() },
            "管理分享链接 →",
          ),
        ),
      );
      dialog.showModal();
    },
    signal,
  );
  // Refreshing a page also aborts its dialog requests. Only the editor
  // that submitted this write may replace its dialog or refresh the page.
  if (active() && !ctx.signal?.aborted) {
    retained.show();
    ctx.render();
  }
}
