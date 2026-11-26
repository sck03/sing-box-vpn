import { api, query, mutate, getCertificates } from "./api.js";
import { card, help, el, row, button, formDialog, notice } from "./ui.js";
import { workspace } from "./workspace.js";
import { grantOptions } from "./configuration-api.js";
import { observeStatus } from "./status-observer.js";
import { dialogSession } from "./dialog-session.js";
import { withCertificateStatus } from "./certificate-fields.js";
import { certificateEditor } from "./certificate-editor.js";
import {
  accessValue,
  accessFields,
  accessOptions,
  wireAccessMode,
} from "./public-access-fields.js";

export async function access(ctx) {
  const request = ctx.request || api;
  const [settings, c, status] = await Promise.all([
    query("/api/panel/settings", ctx.signal, request),
    ctx.config(),
    getCertificates(ctx.signal, request),
  ]);
  const certificates = () =>
    withCertificateStatus(c.certificates || [], status);
  const restart = async (revision) => {
    const session = dialogSession(ctx.signal);
    session.replace(
      el("h2", {}, "正在提交重启任务"),
      help("连接会短暂中断，请稍后使用新地址重新登录。"),
    );
    try {
      const result = await api(
        "/api/panel/restart",
        { revision },
        { signal: session.signal },
      );
      if (!session.active()) return;
      notice("面板重启任务 #" + result.task_id + " 已提交。");
      session.replace(
        el("h2", {}, "面板正在重启"),
        help(
          "健康检查失败会恢复原访问配置。请稍后打开：" +
            (settings.options.public_url || "原 SSH 转发地址"),
        ),
        button("关闭", session.close),
      );
    } catch (error) {
      if (session.active()) session.fail(error);
    }
  };
  const certificateActions = (editor, reopen) => {
    const open = (provider) => {
      const draft = editor.value();
      if (typeof draft.trusted_proxy === "string")
        draft.trusted_proxy = draft.trusted_proxy
          .split(",")
          .map((v) => v.trim())
          .filter(Boolean);
      certificateEditor(
        ctx,
        c,
        provider,
        (asset) => reopen({ ...draft, certificate_asset: asset.id }),
        () => reopen(draft),
      );
    };
    const tools = el(
      "div",
      { class: "wide certificate-actions" },
      row(
        button("导入证书", () => open("import")),
        button("申请证书", () => open("acme")),
      ),
    );
    editor.form.querySelector(".fields").append(tools);
    const refresh = () => {
      tools.hidden = editor.value().mode !== "tls";
    };
    editor.form.addEventListener("change", refresh);
    refresh();
  };
  const edit = (draft = accessValue(settings.options, certificates())) => {
    const editor = formDialog(
      "配置面板访问",
      accessFields(certificates()),
      draft,
      async (v) => {
        const next = accessOptions(v, certificates());
        const session = dialogSession(ctx.signal);
        session.replace(
          el("h2", {}, "正在校验访问配置"),
          help("检查监听地址、证书有效期和域名匹配…"),
        );
        try {
          const validated = await mutate(
            "/api/panel/settings/preview",
            { options: next },
            session.signal,
          );
          if (!session.active()) return;
          session.replace(
            el("h2", {}, "确认面板访问配置"),
            help(
              "直接 HTTPS 无需 Nginx/Caddy。保存后重启生效，健康检查失败会恢复原设置。请先在云安全组放行 HTTPS 端口。",
            ),
            el("pre", {}, JSON.stringify(validated.options, null, 2)),
            row(
              button("返回编辑", () => edit(v)),
              button(
                "保存配置",
                async () => {
                  await mutate(
                    "/api/panel/settings",
                    { options: validated.options, revision: settings.revision },
                    session.signal,
                  );
                  const saved = await query("/api/panel/settings", session.signal);
                  if (!session.active()) return;
                  settings.options = saved.options;
                  settings.revision = saved.revision;
                  session.replace(
                    el("h2", {}, "面板访问配置已保存"),
                    help("点击重启后，使用新地址重新登录。"),
                    row(
                      button("稍后重启", () => {
                        session.close();
                        ctx.render();
                      }),
                      button(
                        "重启并应用",
                        () => restart(saved.revision),
                        "primary",
                      ),
                    ),
                  );
                },
                "primary",
              ),
            ),
          );
        } catch (error) {
          if (session.active())
            session.replace(
              el("h2", {}, "访问配置未通过校验"),
              help(error.message),
              button("返回编辑", () => edit(v)),
            );
        }
      },
    );
    editor.form.querySelector('button[type="submit"]').textContent =
      "预览访问配置";
    wireAccessMode();
    certificateActions(editor, edit);
  };
  const panel = () =>
    card(
      "面板公网访问",
      help(
        "直接 HTTPS：面板自行加载证书，无需安装 Nginx/Caddy。HTTPS 反向代理：已有代理负责证书和公网入口，面板验证可信代理。SSH 转发适用于本机维护。",
      ),
      help(
        "账户订阅和高级输出均可共用 HTTPS 面板域名。域名 DNS 应指向 VPS，证书可先申请或导入。",
      ),
      el(
        "p",
        { class: "access-address" },
        "当前访问地址：",
        el("code", {}, settings.active.public_url || settings.active.listen),
      ),
      settings.restart_required
        ? help("已保存配置与当前运行配置不同，重启后生效。")
        : null,
      row(
        button("配置面板访问", () => edit(), "primary"),
        ...(settings.restart_required
          ? [button("重启并应用", () => restart(settings.revision))]
          : []),
      ),
    );
  const subscription = async () => {
    const live = el("div", { role: "status" });
    const { refresh } = observeStatus(ctx, async () => {
      try {
        const current = await grantOptions(ctx.signal, request);
        if (ctx.signal?.aborted) return;
        live.replaceChildren(
          help(current.listener_enabled
            ? "已应用：独立下载监听 · " + (current.public_url || "地址不可用")
            : "已应用：分享使用面板地址。"),
          ...(current.pending ? [help("服务器有待应用的配置变更。下载仍使用已应用配置。")] : []),
          ...(current.unavailable ? [help(current.unavailable)] : []),
        );
      } catch (error) {
        if (error.name === "AbortError" || ctx.signal?.aborted) return;
        live.replaceChildren(help("已应用状态暂不可读取：" + error.message), button("重试", refresh));
      }
    }, { section: "subscription" });
    await refresh();
    return card(
      "独立订阅地址（可选）",
      help(
        "将订阅和后台分开时启用。直接 HTTPS 使用内置 TLS，无需额外代理；反向代理方式需要事先自行安装和配置 Nginx/Caddy。普通分享与高级输出都会使用这个地址。",
      ),
      live,
      el(
        "p",
        {},
        c.listener.enabled
          ? "草稿：启用独立下载监听 · " + (c.listener.public_base_url || "尚未填写公网 URL")
          : "草稿：关闭独立下载监听。",
      ),
      button("配置独立下载监听", () => {
        const options = {
          ...c.listener,
          listen: `${c.listener.listen.includes(":") ? "[" + c.listener.listen + "]" : c.listener.listen}:${c.listener.port}`,
          public_url: c.listener.public_base_url || "",
        };
        const initial = accessValue(options, certificates());
        if (initial.mode === "local") initial.mode = "tls";
        const editSubscription = (draft = initial) => {
          const editor = formDialog(
            "独立下载监听",
            accessFields(certificates(), true),
            draft,
            (v) => {
              const next = accessOptions(v, certificates()),
                match = next.listen.match(/^(?:\[([^\]]+)\]|([^:]+)):(\d+)$/);
              if (!match) throw new Error("监听地址需要 IP:端口");
              c.listener = {
                enabled: v.enabled,
                listen: match[1] || match[2],
                port: Number(match[3]),
                public_base_url: next.public_url,
                certificate: next.certificate,
                key: next.key,
                trusted_proxy: next.trusted_proxy,
              };
              ctx.changed();
              ctx.render();
            },
          );
          wireAccessMode();
          certificateActions(editor, editSubscription);
        };
        editSubscription();
      }),
    );
  };
  return [workspace("access", { panel, subscription }, ctx)];
}
