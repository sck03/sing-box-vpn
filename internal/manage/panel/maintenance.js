import { api, download, restoreForm } from "./api.js";
import { tasksCard } from "./tasks.js";
import { capabilities } from "./capabilities.js";
import { workspace } from "./workspace.js";
import {
  el,
  card,
  row,
  button,
  help,
  formDialog,
  showResult,
  notice,
} from "./ui.js";
export async function maintenance(ctx) {
  let latest = ctx.overview,
    taskTable;
  ctx.updateOverview = (next) => {
    latest = next;
    taskTable?.updateItems(next.tasks);
  };
  async function logsAndTasks() {
    const metadata = await capabilities(ctx.request || api);
    const data = latest || (await (ctx.request || api)("/api/overview"));
    const tasks = tasksCard((latest || data).tasks, ctx);
    taskTable = tasks.querySelector(".data-table");
    const logs = card(
      "运行日志",
      row(
        ...[
          ...(metadata.services || []).map((service) => service.name),
          "sb-panel",
        ].map((service) =>
          button(service, async () =>
            showResult(
              service,
              (await api("/api/logs?service=" + service)).log,
            ),
          ),
        ),
      ),
    );
    return [tasks, logs];
  }
  if (ctx.session.role === "operator") return logsAndTasks();
  const backup = () => {
    const file = el("input", {
        type: "file",
        accept: ".sbvpn",
        "aria-label": "选择加密备份",
      }),
      password = el("input", {
        type: "password",
        autocomplete: "new-password",
        "aria-label": "备份口令",
        placeholder: "备份口令（至少 12 字节）",
      });
    let preview = null;
    const matchesSelection = (candidate) =>
      candidate &&
      candidate.backup === file.files[0] &&
      candidate.password === password.value;
    file.addEventListener("change", () => {
      preview = null;
    });
    password.addEventListener("input", () => {
      preview = null;
    });
    return card(
      "全量加密备份与恢复",
      help(
        "备份包含统一数据库、管理员、凭据、账本与证书。在线恢复仅替换配置，保留本机最新权益、账本、管理员和撤销记录。空目录灾难恢复使用维护 CLI，所有权益默认禁用，核对后再启用。",
      ),
      password,
      row(
        button(
          "下载全量备份",
          async () =>
            download(
              await api(
                "/api/backup",
                { password: password.value },
                { blob: true },
              ),
              "sing-box-vpn.sbvpn",
            ),
          "primary",
        ),
      ),
      file,
      row(
        button("预览恢复", async () => {
          if (!file.files[0]) throw new Error("请选择备份文件");
          const candidate = {
            backup: file.files[0],
            password: password.value,
          };
          preview = null;
          const result = await api(
            "/api/restore",
            restoreForm(candidate.backup, candidate.password),
          );
          if (!matchesSelection(candidate) || ctx.signal?.aborted) return;
          preview = { ...candidate, revision: result.revision };
          showResult("恢复预览", result);
        }),
        button(
          "应用已预览配置",
          async () => {
            if (!matchesSelection(preview))
              throw new Error("请先预览当前选择的备份");
            if (
              !confirm(
                "恢复配置并应用到当前运行实例？最新权益、用量与撤销记录将保留。",
              )
            )
              return;
            const result = await api(
              "/api/restore",
              restoreForm(preview.backup, preview.password, preview.revision),
            );
            notice("恢复任务 #" + result.task_id + " 已排队");
            preview = null;
          },
          "danger",
        ),
      ),
    );
  };
  const upgrade = () =>
    card(
      "程序与受管运行时",
      help(
        "受管核心随经验证的程序发行包更新。官方 sing-box 用于配置验证，不直接替换正在计费的受管运行时。升级和回滚保留原活动与开机启动状态。",
      ),
      row(
        button("更新到正式发行包", () =>
          formDialog(
            "程序升级",
            [{ key: "value", label: "发行标签（留空使用最新正式版）" }],
            { value: "" },
            (v) => ctx.task({ operation: "program-upgrade", value: v.value }),
          ),
        ),
        button(
          "回滚受管运行时",
          async () => {
            if (confirm("验证并切换到上一份受管运行时？原活动实例会重启。"))
              await ctx.task({ operation: "runtime-rollback" });
          },
          "danger",
        ),
      ),
    );
  const network = () =>
    card(
      "高级网络设置",
      help(
        "仅在内核已支持 BBR 时写入项目拥有的 sysctl 文件。FQ / BBR 的实际收益取决于网络，不保证提升速度。",
      ),
      row(
        button("查看当前值", async () =>
          showResult("当前内核网络设置", await api("/api/network")),
        ),
        button("启用 BBR + FQ", () =>
          ctx.task({ operation: "bbr", value: "enable" }),
        ),
        button("恢复启用前的设置", () =>
          ctx.task({ operation: "bbr", value: "restore" }),
        ),
      ),
    );
  return [
    workspace(
      "maintenance",
      { logs: logsAndTasks, backup, upgrade, network },
      ctx,
    ),
  ];
}
