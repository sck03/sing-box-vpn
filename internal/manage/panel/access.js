import { api } from "./api.js";
import {
  card,
  help,
  el,
  row,
  button,
  formDialog,
  showResult,
  notice,
} from "./ui.js";
import { workspace } from "./workspace.js";
import {
  accessValue,
  accessFields,
  accessOptions,
  wireAccessMode,
} from "./public-access-fields.js";

export async function access(ctx) {
  const request = ctx.request || api;
  const settings = await request("/api/panel/settings");
  const c = await ctx.config(),
    certificates = c.certificates || [];
  const restart = async (revision) => {
    const result = await api("/api/panel/restart", { revision });
    notice("面板重启任务 #" + result.task_id + " 已提交；连接会短暂中断。");
    showResult(
      "面板正在重启",
      "请稍后打开新地址并重新登录。健康检查失败会自动恢复原访问配置。\n\n" +
        (settings.options.public_url || "使用原 SSH 转发方式"),
    );
  };
  const edit = () => {
    formDialog(
      "配置面板访问",
      accessFields(certificates),
      accessValue(settings.options),
      async (v) => {
        const next = accessOptions(v, certificates);
        const validated = await api("/api/panel/settings/preview", {
          options: next,
        });
        const dialog = document.querySelector("#dialog");
        dialog.replaceChildren(
          el("h2", {}, "确认面板访问配置"),
          help(
            "直接 HTTPS 使用内置 TLS；反向代理需要事先自行配置 Nginx/Caddy。保存后点击重启使配置生效，失败会自动恢复原设置。云安全组需放行实际 HTTPS 端口。",
          ),
          el("pre", {}, JSON.stringify(validated.options, null, 2)),
          row(
            button("返回编辑", edit),
            button(
              "保存配置",
              async () => {
                await api("/api/panel/settings", {
                  options: validated.options,
                  revision: settings.revision,
                });
                const saved = await api("/api/panel/settings");
                settings.options = saved.options;
                settings.revision = saved.revision;
                dialog.replaceChildren(
                  el("h2", {}, "面板访问配置已保存"),
                  help("点击重启后，稍后使用新地址登录。"),
                  row(
                    button("稍后重启", () => {
                      dialog.close();
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
        dialog.showModal();
      },
    );
    wireAccessMode();
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
        button("配置面板访问", edit, "primary"),
        ...(settings.restart_required
          ? [button("重启并应用", () => restart(settings.revision))]
          : []),
      ),
    );
  const subscription = () =>
    card(
      "独立订阅地址（可选）",
      help(
        "将订阅和后台分开时启用。直接 HTTPS 使用内置 TLS，无需额外代理；反向代理方式需要事先自行安装和配置 Nginx/Caddy。普通分享与高级输出都会使用这个地址。",
      ),
      el(
        "p",
        {},
        c.listener.enabled
          ? "已启用：" + (c.listener.public_base_url || "尚未填写公网 URL")
          : "未启用，分享使用面板地址。",
      ),
      button("配置独立下载监听", () => {
        const options = {
          ...c.listener,
          listen: `${c.listener.listen.includes(":") ? "[" + c.listener.listen + "]" : c.listener.listen}:${c.listener.port}`,
          public_url: c.listener.public_base_url || "",
        };
        const initial = accessValue(options);
        if (initial.mode === "local") initial.mode = "tls";
        formDialog(
          "独立下载监听",
          accessFields(certificates, true),
          initial,
          (v) => {
            const next = accessOptions(v, certificates),
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
      }),
    );
  return [workspace("access", { panel, subscription }, ctx)];
}
