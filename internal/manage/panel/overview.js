import { api } from "./api.js";
import { capabilities } from "./capabilities.js";
import { tasksCard } from "./tasks.js";
import { el, card, help, table, badge, button } from "./ui.js";
export async function overview(ctx) {
  const metadata = await capabilities();
  const data = ctx.overview || (await (ctx.request || api)("/api/overview"));
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
      [
        data.applied_revision === data.revision ? "已同步" : "待应用",
        "配置状态",
      ],
      [data.runtimes.filter((v) => v.recent).length, "近期在线实例"],
      [data.tasks.filter((v) => v.state === "failed").length, "失败任务"],
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
      help("服务启动只运行已保存的配置。点击服务对应的设置入口配置功能；VPNGate 节点核心与隧道网关分工运行，共用同一 VPNGate 页面。"),
      table(
        [
          { key: "name", label: "服务" },
          { label: "职责", value: (v) => v.description || v.label || "",
            render: (v) => el("div", {}, el("strong", {}, v.label || v.name), help(v.description || "")) },
          { label: "功能设置", value: (v) => v.page || "",
            render: (v) => {
              const allowed = ctx.session.role === "admin" || (ctx.session.role === "operator" && v.page === "vpngate");
              return v.page && allowed ? el("a", { href: "#" + v.page }, ({ inbounds: "入站与发布", vpngate: "VPNGate", access: "公网访问 / 下载监听", outbounds: "出站与隧道 / Argo" })[v.page] || "功能设置") : el("span", {}, "请联系管理员配置");
            } },
          { label: "安装", value: (v) => v.LoadState || "本地开发" },
          {
            label: "运行",
            value: (v) => v.ActiveState || v.state || "unknown",
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
      ),
    ),
    tasksCard(data.tasks, ctx),
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
      next.applied_revision === next.revision ? "已同步" : "待应用",
      next.runtimes.filter((v) => v.recent).length,
      next.tasks.filter((v) => v.state === "failed").length,
      next.tasks.filter((t) => ["queued", "running"].includes(t.state)).length,
    ];
    stats.querySelectorAll("strong").forEach((node, i) => {
      node.textContent = values[i];
    });
    [next.runtimes, next.services, next.tasks, next.credentials || []].forEach(
      (items, i) => tables[i].updateItems(items),
    );
  };
  return content;
}
