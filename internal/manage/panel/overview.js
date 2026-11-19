import { api, getOverview } from "./api.js";
import { capabilities } from "./capabilities.js";
import { tasksCard } from "./tasks.js";
import { canView, pageFor, sectionsFor, routeFor } from "./page-catalog.js";
import { el, card, help, table, badge, button } from "./ui.js";

const serviceState = (value) => ({ active: "运行中", inactive: "已停止", activating: "启动中", deactivating: "停止中", failed: "失败", unavailable: "环境不可用", unknown: "未知", loaded: "已安装", "not-found": "未安装", enabled: "已启用", disabled: "已关闭", static: "静态单元", masked: "已屏蔽" })[value] || value || "—";

function configurationStatus(snapshot) {
  if (snapshot.revision === snapshot.applied_revision)
    return `已应用 v${snapshot.applied_revision}`;
  const task = snapshot.tasks.find((entry) => entry.kind === "apply");
  const stage =
    { queued: "排队中", running: "应用中", failed: "应用失败" }[task?.state] ||
    "待应用";
  return `v${snapshot.revision} ${stage} · 生效 v${snapshot.applied_revision}`;
}
export async function overview(ctx) {
  const metadata = await capabilities();
  const data = ctx.overview || (await (ctx.request ? ctx.request("/api/overview") : getOverview(ctx.signal)));
  ctx.observeRevision(data.revision);
  const health = el("div", { class: "health-summary", role: "status" });
  function updateHealth(next) {
    const failed = next.services.filter(
      (s) => s.ActiveState === "failed",
    ).length;
    const pending = (next.credentials || []).filter(
      (v) => v.revision > v.published || v.revision > v.loaded,
    ).length;
    const configurationPending = next.revision !== next.applied_revision;
    const online = next.runtimes.some((v) => v.recent);
    const uncertain = next.services.some(
      (s) => s.error || s.state === "unavailable",
    );
    const warning = pending || configurationPending || !online || uncertain;
    const title = failed
      ? `${failed} 个服务异常`
      : configurationPending
        ? "配置尚未应用完成"
        : pending
          ? `${pending} 个实例有待加载凭据`
          : uncertain
            ? "主机服务状态不可用"
            : !online
              ? "未收到近期运行心跳"
              : "配置与凭据已同步";
    health.replaceChildren(
      badge(
        failed ? "需要处理" : warning ? "请查看状态" : "运行状态",
        failed ? "bad" : warning ? "warn" : "good",
      ),
      el(
        "div",
        {},
        el("strong", {}, title),
        el(
          "p",
          {},
          failed
            ? "系统会定期重试失败服务，可在下方查看状态和日志。"
            : pending
              ? "已停止的实例会在下次启动时加载；运行中的实例会自动重试发布。"
              : "配置版本、进程心跳和出口可达性分别检查；出口与目标可达性请使用网络检测确认。",
        ),
      ),
    );
  }
  updateHealth(data);
  const stats = el(
    "div",
    { class: "stats" },
    ...[
      [configurationStatus(data), "配置状态"],
      [data.runtimes.filter((v) => v.recent).length, "近期在线实例"],
      [data.task_counts.attention, "需要关注"],
      [
        data.task_counts.active,
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
    health,
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
            value: (v) => (v.recent ? "近期在线" : "未收到近期心跳"),
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
      help(
        "服务启动只运行已保存的配置。点击服务对应的设置入口配置功能；VPNGate 接入核心与出口网关分别进入对应工作区。",
      ),
      table(
        [
          { key: "name", label: "服务" },
          {
            label: "职责",
            value: (v) => v.description || v.label || "",
            render: (v) =>
              el(
                "div",
                {},
                el("strong", {}, v.label || v.name),
                help(v.description || ""),
              ),
          },
          {
            label: "功能设置",
            value: (v) => v.page || "",
            render: (v) => {
              const target = routeFor(
                v.page + (v.section ? "/" + v.section : ""),
                ctx.session.role,
              );
              const section = sectionsFor(v.page, ctx.session.role).find(
                ([key]) => key === target.section,
              );
              return canView(v.page, ctx.session.role)
                ? el(
                    "a",
                    { href: "#" + target.hash },
                    pageFor(v.page, ctx.session.role).title +
                      (section ? " / " + section[1] : ""),
                  )
                : el("span", {}, "请联系管理员配置");
            },
          },
          { label: "安装", value: (v) => serviceState(v.LoadState) },
          {
            label: "运行",
            value: (v) => serviceState(v.ActiveState || v.state || "unknown"),
            render: (v) =>
              badge(
                serviceState(v.ActiveState || v.state || "unknown"),
                v.ActiveState === "active" ? "good" : "warn",
              ),
          },
          { label: "开机启动", value: (v) => serviceState(v.UnitFileState) },
        ],
        data.services,
        ctx.session.role !== "viewer"
          ? (v) =>
              metadata.service_actions.map((op) =>
                button(
                  {
                    start: "启动",
                    stop: "停止",
                    restart: "重启",
                    enable: "开机启动",
                    disable: "取消自启",
                  }[op],
                  () =>
                    ctx.task({
                      operation: "service",
                      service: v.name,
                      value: op,
                    }),
                ),
              )
          : null,
        { controls: false },
      ),
    ),
    tasksCard(data.tasks.slice(0, 5), ctx, { compact: true }),
    card(
      "凭据发布",
      table(
        [
          { key: "runtime", label: "实例" },
          { key: "revision", label: "目标版本" },
          { key: "published", label: "已发布" },
          { key: "loaded", label: "已加载" },
        ],
        data.credentials || [],
      ),
    ),
  ];
  const tables = content.flatMap((node) => [
    ...node.querySelectorAll(".data-table"),
  ]);
  ctx.updateOverview = (next) => {
    updateHealth(next);
    ctx.observeRevision(next.revision);
    const values = [
      configurationStatus(next),
      next.runtimes.filter((v) => v.recent).length,
      next.task_counts.attention,
      next.task_counts.active,
    ];
    stats.querySelectorAll("strong").forEach((node, i) => {
      node.textContent = values[i];
    });
    [next.runtimes, next.services, next.tasks.slice(0, 5), next.credentials || []].forEach(
      (items, i) => tables[i].updateItems(items),
    );
  };
  const shortcuts = [
    ["inbounds", "配置节点", "选择协议，设置端口与发布地址"],
    ctx.session.role === "admin"
      ? ["accounts", "添加用户并分享", "创建权益、绑定入口、发送分享链接"]
      : ["accounts", "查看用户用量", "流量、有效期与历史账期"],
    ["access", "设置公网访问", "配置 HTTPS 与可分享的订阅地址"],
    [
      "maintenance/logs",
      "查看任务与日志",
      ctx.session.role === "operator"
        ? "检查操作结果和运行日志"
        : "检查操作结果、备份和程序更新",
    ],
  ].filter(([id]) => canView(id.split("/")[0], ctx.session.role));
  return [
    health,
    stats,
    card(
      "常用操作",
      el(
        "div",
        { class: "shortcut-grid" },
        ...shortcuts.map(([id, title, description]) =>
          el(
            "a",
            { href: "#" + id, class: "shortcut" },
            el("strong", {}, title),
            el("small", {}, description),
          ),
        ),
      ),
    ),
    content[3],
    content[4],
    el(
      "details",
      { class: "advanced" },
      el("summary", {}, "运行详情：核心版本与凭据发布"),
      content[2],
      content[5],
    ),
  ];
}
