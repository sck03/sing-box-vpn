import { api, download } from "./api.js";
import { loadDialog } from "./dialog-session.js";
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
export async function historyControls(ctx) {
  const request = ctx.request || api,
    data = await request("/api/tasks/history");
  const p = data.policy,
    u = data.usage;
  const clean = async (only) => {
    if (
      !confirm(
        only
          ? "清理已标记处理的任务？"
          : "清理所有已结束的任务？进行中的任务将保留。",
      )
    )
      return;
    const result = await api("/api/tasks/history/clear", {
      acknowledged_only: only,
    });
    notice("已清理 " + result.deleted + " 条任务记录");
    ctx.render();
  };
  return card(
    "历史保留与清理",
    help(
      `已结束 ${u.completed} 条 · 进行中 ${u.pending} 条 · 详情 ${(u.result_bytes / 1048576).toFixed(2)} MiB；当前保留 ${p.count} 条 / ${p.days} 天。`,
    ),
    help(
      "自动清理按数量、时间和详情预算执行。清理详情保留最终状态；数据库释放的空间会供后续记录复用。",
    ),
    row(
      button("保留策略", () =>
        formDialog(
          "任务保留策略",
          [
            {
              key: "count",
              label: "保留数量",
              type: "number",
              min: 20,
              max: 2000,
            },
            {
              key: "days",
              label: "保留天数",
              type: "number",
              min: 1,
              max: 365,
            },
            {
              key: "result_bytes",
              label: "详情预算（MiB）",
              type: "number",
              min: 0.25,
              max: 64,
              step: 0.25,
              scale: 1048576,
            },
          ],
          p,
          async (v) => {
            await api("/api/tasks/history", {
              count: v.count,
              days: v.days,
              result_bytes: v.result_bytes,
            });
            ctx.render();
          },
          false,
          ctx.signal,
        ),
      ),
      button("按策略立即清理", async () => {
        await api("/api/tasks/history/prune", {});
        ctx.render();
      }),
      button("清理已处理", () => clean(true)),
      button("清理全部已结束", () => clean(false), "danger"),
    ),
  );
}
export function journalCard(services, ctx) {
  const service = el(
    "select",
    { "aria-label": "日志服务" },
    el("option", { value: "" }, "所有项目服务"),
    ...services.map((v) =>
      el("option", { value: v.name }, v.label + " · " + v.name),
    ),
    el("option", { value: "sb-panel" }, "管理面板 · sb-panel"),
  );
  const since = el(
    "select",
    { "aria-label": "日志时间" },
    ...["1 hour ago", "24 hours ago", "7 days ago"].map((v, i) =>
      el("option", { value: v }, ["近一小时", "近一天", "近七天"][i]),
    ),
  );
  const params = () =>
    "?" +
    new URLSearchParams({
      service: service.value,
      since: since.value,
      lines: "1000",
    });
  const controls = row(
    service,
    since,
    button("查看日志", () =>
      loadDialog(
        ctx.signal, "运行日志",
        (signal) => api("/api/logs" + params(), undefined, { signal }),
        (value) => showResult("运行日志", value.log),
      ),
    ),
    button("下载日志", async () =>
      download(
        await api("/api/logs/download" + params(), undefined, { blob: true, signal: ctx.signal }),
        "sb-vpn.log",
      ),
    ),
    button("查看占用", () =>
      loadDialog(
        ctx.signal, "项目日志占用",
        (signal) => api("/api/logs/usage", undefined, { signal }),
        (value) => showResult("项目日志占用", value.usage + "\n上限 64 MiB，最长保留 7 天。"),
      ),
    ),
  );
  controls.classList.add("journal-controls");
  if (ctx.session.role === "admin")
    controls.append(
      button(
        "清理运行日志",
        async () => {
          if (!confirm("清理项目 sb-vpn 命名空间中的历史运行日志？")) return;
          await api("/api/logs/clear", {});
          notice("项目历史运行日志已清理。");
        },
        "danger",
      ),
    );
  return card(
    "运行日志",
    help(
      "项目日志独立存储在 sb-vpn 命名空间，按大小和时间自动轮转。可筛选、下载和清理历史日志。",
    ),
    controls,
  );
}
