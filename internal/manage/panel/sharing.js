import { api } from "./api.js";
import { clientCatalog, clientOptions } from "./clients.js";
import { $, el, button, row, help, formDialog, notice } from "./ui.js";

// Used both from an entitlement and the authorization inventory.
export async function createAccountShare(accounts, ctx) {
  if (!accounts.length) throw new Error("请先创建权益并绑定入站。");
  const catalog = await clientCatalog(ctx.request || api);
  formDialog(
    "创建账户分享",
    [
      { key: "id", label: "授权 ID", required: true },
      {
        key: "account_id",
        label: "使用权益",
        type: "select",
        options: accounts.map((a) => [a.id, a.name]),
        readonly: accounts.length === 1,
      },
      {
        key: "scope",
        label: "运行实例",
        type: "select",
        options: [
          ["", "普通 + VPNGate"],
          ["normal", "普通"],
          ["vpngate", "VPNGate"],
        ],
      },
      {
        key: "clients",
        label: "允许客户端格式（可多选）",
        type: "multiselect",
        options: clientOptions(catalog),
        required: true,
      },
      { key: "expires_at", label: "下载到期", type: "date" },
      {
        key: "max_downloads",
        label: "下载次数上限（留空不限）",
        type: "number",
        nullable: true,
        min: 0,
      },
    ],
    {
      id: "",
      account_id: accounts[0].id,
      scope: "",
      clients: ["sing-box", "mihomo", "uri"],
      expires_at: null,
      max_downloads: null,
    },
    async (value) => {
      const result = await (ctx.request || api)("/api/grants", value);
      const dialog = $("#dialog");
      const address = el("input", { "aria-label": "分享地址", readonly: true });
      address.value = result.user_url;
      dialog.replaceChildren(
        el("h2", {}, "分享已创建"),
        help(
          "请保存这个地址，之后无法再次查看完整链接。接收者可查看用量并下载配置。",
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
        ),
      );
      dialog.showModal();
      ctx.render();
    },
  );
}
