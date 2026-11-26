import { api, getCertificates } from "./api.js";
import { certificateEditor } from "./certificate-editor.js";
import { certificateSources } from "./certificate-fields.js";
import { observeStatus } from "./status-observer.js";
import { el, card, row, button, help, table } from "./ui.js";

/** @param {string} id @param {import("./contracts.js").CertificateStatus | undefined} status */
function renewalButton(id, status, ctx) {
  const control = button("申请 / 续期", () =>
    ctx.task({ operation: "certificate-renew", id }),
  );
  control.disabled = status?.can_renew !== true;
  const reason = status?.renewal_unavailable || "请先应用证书配置";
  return control.disabled ? row(control, help(reason)) : control;
}

export async function certificates(ctx) {
  const c = await ctx.config();
  let status = new Map(
      (await getCertificates(ctx.signal, ctx.request || api)).map((asset) => [
        asset.id,
        asset,
      ]),
    );
  const draftIDs = new Set(c.certificates.map((a) => a.id));
  const assets = () => [
    ...c.certificates,
    ...[...status.values()].filter((a) => !draftIDs.has(a.id)),
  ].map((asset) => ({ ...asset, status: status.get(asset.id) }));
  const problem = el("div", { role: "status", hidden: true });
  let listing;
  const { refresh } = observeStatus(ctx, async () => {
    try {
      const next = await getCertificates(ctx.signal, ctx.request || api);
      if (ctx.signal?.aborted) return;
      status = new Map(next.map((asset) => [asset.id, asset]));
      listing.updateItems(assets());
      problem.hidden = true;
    } catch (error) {
      if (error.name === "AbortError" || ctx.signal?.aborted) return;
      problem.hidden = false;
      problem.replaceChildren(help("证书状态更新失败：" + error.message), button("重试", refresh));
    }
  });
  const changed = () => {
    ctx.changed();
    ctx.render();
  };
  return [
    card(
      "证书资产",
      el(
        "a",
        { href: "#maintenance/upgrade" },
        "查看证书签发组件版本 / 检查程序更新",
      ),
      help(
        "导入前验证域名、有效期和私钥匹配。内置 ACME 自动申请与续期，无需额外安装程序；域名须指向本机，端口 80 需要空闲且从公网可达。",
      ),
      row(
        button(
          "导入 PEM",
          () => certificateEditor(ctx, c, "import"),
          "primary",
        ),
        button("申请自动证书", () => certificateEditor(ctx, c, "acme")),
      ),
      problem,
      listing = table(
        [
          { key: "id", label: "资产" },
          {
            label: "域名",
            value: (a) => {
              const current = status.get(a.id);
              return current?.applied && current.domain !== a.domain
                ? `${current.domain}（草稿：${a.domain}）`
                : a.domain;
            },
          },
          {
            label: "配置状态",
            value: (a) => {
              const current = status.get(a.id);
              if (!draftIDs.has(a.id))
                return current?.applied
                  ? "已生效 · 草稿待移除"
                  : "已保存 · 草稿待移除";
              if (!current?.saved && !current?.applied) return "仅草稿 · 请先应用";
              if (!current.applied) return "已保存 · 待应用";
              return current.pending ? "已生效 · 有待应用变更" : "已生效";
            },
          },
          {
            label: "续期归属",
            value: (a) =>
              certificateSources[status.get(a.id)?.provider || a.provider] || a.provider,
          },
          {
            label: "自动续期",
            value: (a) =>
              ({
                automatic: "已登记",
                registration_pending: "待自动登记（需端口 80）",
                manual: "手动更新",
              })[status.get(a.id)?.renewal_state] || "待应用",
          },
          {
            label: "生效证书 / 有效期",
            value: (a) => {
              const current = status.get(a.id);
              if (current?.days_left == null) return current?.error || "待申请";
              return current.days_left < 0
                ? `已过期 ${-current.days_left} 天`
                : `剩余 ${current.days_left} 天`;
            },
          },
          {
            label: "引用",
            value: (a) => (status.get(a.id)?.used_by || []).join(", "),
          },
        ],
        assets(),
        (a) => [
          ...(a.provider === "acme" || status.get(a.id)?.provider === "acme"
            ? [renewalButton(a.id, status.get(a.id), ctx)]
            : []),
          ...(draftIDs.has(a.id)
            ? [button("从草稿移除", () => {
                if (confirm("移除证书资产？请先解除入口引用。")) {
                  c.certificates = c.certificates.filter((asset) => asset.id !== a.id);
                  changed();
                }
              }, "danger")]
            : []),
        ],
      ),
    ),
    card(
      "使用方式",
      help(
        "证书材料保存为受限文件，配置保存引用。前往「入站」→「证书」选择资产。导入新的资产后再切换引用，可保留旧证书用于回滚。",
      ),
    ),
  ];
}
