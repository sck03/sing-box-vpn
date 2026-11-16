import { api, download, restoreForm } from "./api.js";
import {
  el,
  card,
  row,
  button,
  help,
  table,
  formDialog,
  jsonDialog,
  showResult,
  badge,
  date,
  notice,
} from "./ui.js";
export async function overview(ctx) {
  const data = ctx.overview || (await (ctx.request || api)("/api/overview"));
  ctx.observeRevision(data.revision);
  const stats = el(
    "div",
    { class: "stats" },
    ...[
      [
        data.applied_revision === data.revision ? "已同步" : "待应用",
        "配置状态",
      ],
      [data.revision, "期望版本"],
      [data.applied_revision, "实际版本"],
      [
        data.tasks.filter((t) => ["queued", "running"].includes(t.state))
          .length,
        "进行中任务",
      ],
    ].map(([value, label]) =>
      el(
        "div",
        { class: "stat" },
        el("small", {}, label),
        el("strong", {}, value),
      ),
    ),
  );
  const content = [
    stats,
    card(
      "运行实例",
      help(
        "保存、应用、进程运行和目标可达是不同状态。版本心跳表示受管核心实际启动过；目标可达需要另行检测。",
      ),
      table(
        [
          { key: "instance", label: "实例" },
          { key: "version", label: "核心 / 适配版本" },
          { key: "revision", label: "运行配置版本" },
          {
            label: "心跳",
            render: (v) =>
              badge(
                v.recent ? "近期在线" : "未收到近期心跳",
                v.recent ? "good" : "warn",
              ),
          },
        ],
        data.runtimes,
      ),
    ),
    card(
      "系统服务",
      table(
        [
          { key: "name", label: "服务" },
          { label: "安装", value: (v) => v.LoadState || "本地开发" },
          {
            label: "运行",
            render: (v) =>
              badge(
                v.ActiveState || v.state || "unknown",
                v.ActiveState === "active" ? "good" : "warn",
              ),
          },
          { key: "UnitFileState", label: "开机启动" },
        ],
        data.services,
        ctx.session.role !== "viewer"
          ? (v) =>
              ["start", "stop", "restart"].map((op) =>
                button(
                  { start: "启动", stop: "停止", restart: "重启" }[op],
                  () =>
                    ctx.task({
                      operation: "service",
                      service: v.name,
                      value: op,
                    }),
                ),
              )
          : null,
      ),
    ),
    tasksCard(data.tasks, ctx),
  ];
  const tables = content.flatMap((node) => [
    ...node.querySelectorAll(".data-table"),
  ]);
  ctx.updateOverview = (next) => {
    ctx.observeRevision(next.revision);
    const values = [
      next.applied_revision === next.revision ? "已同步" : "待应用",
      next.revision,
      next.applied_revision,
      next.tasks.filter((t) => ["queued", "running"].includes(t.state)).length,
    ];
    stats.querySelectorAll("strong").forEach((node, i) => {
      node.textContent = values[i];
    });
    [next.runtimes, next.services, next.tasks].forEach((items, i) =>
      tables[i].updateItems(items),
    );
  };
  return content;
}
function tasksCard(tasks, ctx) {
  return card(
    "任务与应用结果",
    help(
      "关闭网页后任务继续运行。队列和结果保存在数据库；失败会显示阶段与原因。",
    ),
    table(
      [
        { key: "id", label: "任务" },
        { key: "kind", label: "操作" },
        {
          label: "状态",
          render: (t) =>
            badge(
              t.state,
              t.state === "succeeded"
                ? "good"
                : t.state === "failed"
                  ? "bad"
                  : "warn",
            ),
        },
        { key: "phase", label: "阶段" },
        { label: "时间", value: (t) => date(t.updated) },
        { key: "error", label: "结果" },
      ],
      tasks,
      (t) => [
        ...(ctx.session.role === "admin" ||
        (ctx.session.role === "operator" &&
          ["service", "gateway"].includes(t.kind))
          ? [
              button("详情", async () =>
                showResult(
                  "任务 #" + t.id,
                  await api("/api/tasks/detail?id=" + t.id),
                ),
              ),
            ]
          : []),
        ...(t.state === "queued" &&
        t.kind !== "apply" &&
        ctx.session.role === "admin"
          ? [
              button("取消", async () => {
                await api("/api/tasks/cancel", { id: t.id });
                ctx.render();
              }),
            ]
          : []),
      ],
    ),
  );
}
export async function maintenance(ctx) {
  const data = await (ctx.request || api)("/api/overview");
  const logs = card(
    "运行日志",
    row(
      ...[
        "sing-box",
        "sing-box-vpngate",
        "yg-vpngate",
        "sb-argo",
        "sb-panel",
        "sb-subscription",
      ].map((service) =>
        button(service, async () =>
          showResult(service, (await api("/api/logs?service=" + service)).log),
        ),
      ),
    ),
  );
  if (ctx.session.role === "operator")
    return [logs, tasksCard(data.tasks, ctx)];
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
  const backup = card(
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
            "sing-box-vpn-v4.sbvpn",
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
        preview = { ...candidate, revision: result.revision };
        showResult("恢复预览", result);
      }),
      button(
        "应用已预览配置",
        async () => {
          if (!preview) throw new Error("请先预览备份");
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
  return [
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
    ),
    backup,
    card(
      "运行日志",
      row(
        ...[
          "sing-box",
          "sing-box-vpngate",
          "yg-vpngate",
          "sb-argo",
          "sb-panel",
          "sb-subscription",
        ].map((service) =>
          button(service, async () =>
            showResult(
              service,
              (await api("/api/logs?service=" + service)).log,
            ),
          ),
        ),
      ),
    ),
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
    ),
    tasksCard(data.tasks, ctx),
  ];
}
export async function admins(ctx) {
  const users = await (ctx.request || api)("/api/admins");
  const edit = (a) =>
    formDialog(
      "管理员",
      [
        { key: "id", label: "用户名", required: true, readonly: !!a.revision },
        {
          key: "role",
          label: "角色",
          type: "select",
          options: [
            ["admin", "管理员：配置、秘密与系统管理"],
            ["operator", "操作员：运行控制和日志"],
            ["viewer", "只读：状态与用量"],
          ],
        },
        { key: "disabled", label: "禁用", type: "bool" },
        {
          key: "password",
          label: a.revision ? "新密码（留空保持）" : "密码（12–72 字节）",
          type: "password",
          required: !a.revision,
        },
      ],
      a,
      async (v) => {
        const { password, ...admin } = v;
        await api("/api/admins", { admin, password });
        notice("管理员已保存，其旧会话已撤销");
        if (v.id === ctx.session.id) ctx.logoutLocal();
        else ctx.render();
      },
    );
  return [
    card(
      "管理员与角色",
      help(
        "角色在每个 API 中验证。修改密码、角色或禁用状态会撤销该管理员的所有会话，最后一位有效管理员不能被禁用。",
      ),
      row(
        button(
          "新增管理员",
          () => edit({ id: "", role: "viewer", disabled: false, revision: 0 }),
          "primary",
        ),
        button("审计记录", async () =>
          showResult("管理审计", await api("/api/admin/audit")),
        ),
      ),
      table(
        [
          { key: "id", label: "用户名" },
          { key: "role", label: "角色" },
          { label: "MFA", value: (a) => (a.mfa ? "已启用" : "未启用") },
          {
            label: "状态",
            render: (a) =>
              badge(a.disabled ? "禁用" : "正常", a.disabled ? "bad" : "good"),
          },
        ],
        users,
        (a) => [
          button("编辑", () => edit(a)),
          button("撤销会话", async () => {
            await api("/api/admin/revoke", { id: a.id });
            notice("会话已撤销");
            if (a.id === ctx.session.id) ctx.logoutLocal();
          }),
        ],
      ),
    ),
    card(
      "本账户 TOTP",
      help(
        "先验证当前密码并生成密钥，在认证器中选择 SHA-1 / 6 位 / 30 秒，再输入验证码确认。确认后需要重新登录；验证码不可重放。失去认证器时通过服务器本机 reset-admin 恢复。",
      ),
      row(
        button("生成认证器密钥", () =>
          formDialog(
            "验证当前密码",
            [
              {
                key: "password",
                label: "当前密码",
                type: "password",
                required: true,
              },
            ],
            {},
            async (v) => {
              const result = await api("/api/mfa/enroll", v);
              showResult("TOTP 密钥（10 分钟内确认）", result);
            },
          ),
        ),
        button("确认启用", () =>
          formDialog(
            "确认 TOTP",
            [{ key: "code", label: "6 位动态验证码", required: true }],
            {},
            async (v) => {
              await api("/api/mfa/confirm", v);
              ctx.logoutLocal();
              notice("TOTP 已启用，请重新登录");
            },
          ),
        ),
      ),
    ),
  ];
}
