import { api, getTask, getTasks } from "./api.js";
import { dialogSession } from "./dialog-session.js";
import { validationLabel } from "./validation.js";
import { table } from "./tables.js";
import { card } from "./dom.js";
import { el, button, row, help, badge, date } from "./dom.js";

export async function openTask(id, signal = undefined, completed = undefined) {
  const session = dialogSession(signal);
  const content = el("div", {}, help("正在读取任务详情…"));
  session.replace(content, row(button("关闭", session.close)));
  let timer,
    previous,
    loading = false,
    terminal = false;
  const visible = () => {
    if (document.hidden) {
      clearTimeout(timer);
      timer = undefined;
    } else if (!loading && !terminal && !timer) void refresh();
  };
  document.addEventListener("visibilitychange", visible);
  session.signal.addEventListener(
    "abort",
    () => {
      clearTimeout(timer);
      document.removeEventListener("visibilitychange", visible);
    },
    { once: true },
  );
  const refresh = async () => {
    if (loading || !session.active()) return;
    timer = undefined;
    loading = true;
    try {
      const task = await getTask(id, session.signal);
      if (!session.active()) return;
      const signature = JSON.stringify(task);
      if (signature !== previous) {
        const expanded = content.querySelector("details")?.open;
        const focused = content.contains(document.activeElement);
        taskDetails(
          task,
          {
            ...session,
            replace: (...children) => content.replaceChildren(...children),
          },
          false,
        );
        const details = content.querySelector("details");
        if (details) details.open = !!expanded;
        if (focused) details?.querySelector("summary")?.focus();
        previous = signature;
      }
      terminal = !["queued", "running"].includes(task.state);
      if (terminal && completed) {
        const actions = await completed(task, session.signal);
        if (session.active() && actions?.length)
          content.append(row(...actions));
      }
      if (!terminal && !document.hidden) timer = setTimeout(refresh, 1500);
    } catch (error) {
      if (session.active())
        content.replaceChildren(
          el("p", { class: "form-error", role: "alert" }, error.message),
          button("重试", refresh),
        );
    } finally {
      loading = false;
    }
  };
  await refresh();
}

export async function taskHistory(ctx) {
  const root = el("div");
  let cursors = [0];
  let loading = false;
  let items = [];
  let pending = false,
    signature = "";
  let nextBefore = 0;
  const notice = el("p", { role: "status", hidden: true });
  const latest = button("返回最新记录", () => load([0]));
  latest.hidden = true;
  const newer = button("较新记录", () => load(cursors.slice(0, -1)));
  const older = button("较早记录", () => load([...cursors, nextBefore]));
  const position = help("");
  const controls = row(newer, position, older, latest);
  ctx.updateOverview = (snapshot) => {
    const key = JSON.stringify([snapshot.tasks, snapshot.task_counts]);
    if (key === signature) return;
    signature = key;
    const recent = new Map(snapshot.tasks.map((task) => [task.id, task]));
    items = items.map((task) => recent.get(task.id) || task);
    root.querySelector(".data-table")?.updateItems(items);
    if (cursors.length === 1) {
      if (loading) pending = true;
      else void load().catch(showError);
    } else {
      notice.textContent = "任务记录已有更新，当前历史页保持不变。";
      notice.hidden = false;
      latest.hidden = false;
    }
  };
  const showError = (error) => {
    if (ctx.signal?.aborted) return;
    notice.textContent = "读取任务失败：" + error.message;
    notice.hidden = false;
    latest.hidden = false;
  };
  const load = async (next = cursors) => {
    if (loading) return;
    loading = true;
    try {
      const page = await (ctx.request
        ? ctx.request(
            "/api/tasks?before=" + next.at(-1) + "&limit=50",
            undefined,
            { signal: ctx.signal },
          )
        : getTasks(next.at(-1), ctx.signal));
      ctx.signal?.throwIfAborted();
      items = page.items;
      cursors = next;
      nextBefore = page.next_before;
      newer.disabled = cursors.length === 1;
      older.disabled = !page.next_before;
      position.textContent = "第 " + cursors.length + " 页 · 每页最多 50 条";
      notice.hidden = latest.hidden = true;
      const existing = root.querySelector(".data-table");
      if (existing) existing.updateItems(items);
      else root.replaceChildren(notice, tasksCard(items, ctx), controls);
    } finally {
      loading = false;
      if (pending && !ctx.signal?.aborted) {
        pending = false;
        void load().catch(showError);
      }
    }
  };
  await load();
  return root;
}

const kinds = {
  apply: "应用配置",
  account: "更新凭据",
  subscription: "更新订阅",
  service: "控制服务",
  gateway: "控制网关",
  "certificate-renew": "续期证书",
  bbr: "网络设置",
  "program-upgrade": "程序升级",
  "runtime-rollback": "核心回滚",
  "outbound-probe": "检测出站",
  "panel-restart": "应用面板访问",
};
const phases = {
  queued: "等待执行",
  validating: "校验中",
  executing: "执行中",
  publishing: "发布配置",
  "applying-services": "应用服务",
  "awaiting-panel-health": "等待新面板健康检查",
  "publishing-credentials": "发布凭据",
  "rolled-back": "已恢复原配置",
  "rollback-failed": "恢复失败",
  interrupted: "执行被中断",
  succeeded: "已完成",
  failed: "失败",
  cancelled: "已取消",
  cancelling: "正在停止探测",
};
export const taskKind = (kind) => kinds[kind] || kind;
export const taskPhase = (phase) => phases[phase] || phase;
export const taskStage = (task) =>
  task.state === "failed"
    ? "失败于：" + taskPhase(task.phase)
    : taskPhase(task.phase);
export const canViewTask = (task, session) =>
  session.role === "admin" ||
  (session.role === "operator" &&
    (task.details?.operators ||
      (task.details?.actor && task.details.actor === session.id)));

export function taskDetails(task, session = dialogSession(), footer = true) {
  const failed = task.state === "failed" || task.outcome === "failed";
  const incomplete = ["partial", "cached", "not_run"].includes(task.outcome);
  const details = el("dl", { class: "detail-grid" });
  for (const [label, value] of [
    ["操作", taskKind(task.kind)],
    ["阶段", taskStage(task)],
    ["发起人", task.actor],
    ["更新时间", date(task.updated)],
  ]) {
    details.append(el("dt", {}, label), el("dd", {}, value || "—"));
  }
  const diagnostic = el(
    "details",
    { class: "advanced" },
    el("summary", {}, "技术详情"),
    el("pre", {}, JSON.stringify(task.result, null, 2)),
  );
  session.replace(
    el("h2", {}, "任务 #" + task.id),
    badge(
      failed
        ? "需要处理"
        : incomplete
          ? "请查看检查详情"
          : taskPhase(task.state),
      failed
        ? "bad"
        : incomplete
          ? "warn"
          : task.state === "succeeded"
            ? "good"
            : "warn",
    ),
    details,
    ...(task.error
      ? [el("p", { class: "form-error", role: "alert" }, task.error)]
      : []),
    help(
      failed
        ? task.phase === "rollback-failed"
          ? "请先检查服务状态与日志，确认当前配置后再重新应用。"
          : "请根据错误原因修正配置或运行环境，再重新发起操作。"
        : incomplete
          ? "部分检查未完成，请查看每项结果；无法检测不代表节点故障。"
          : "关闭窗口后，尚未完成的任务会继续运行。",
    ),
    ...resultDetails(task.result),
    diagnostic,
    ...(footer ? [row(button("关闭", session.close))] : []),
  );
}

export function tasksCard(tasks, ctx, { compact = false } = {}) {
  const outcome = (value) =>
    ({
      passed: "全部通过",
      partial: "部分完成",
      failed: "失败",
      cached: "使用旧缓存",
      not_run: "未进行连接检测",
    })[value] || "—";
  return card(
    "任务与应用结果",
    help("任务在后台持续执行。配置应用、运行状态与业务结果分别展示。"),
    table(
      [
        { key: "id", label: "任务" },
        { label: "操作", value: (t) => taskKind(t.kind) },
        {
          label: "状态",
          value: (t) =>
            taskPhase(
              t.cancel_requested && t.state === "running"
                ? "cancelling"
                : t.state,
            ),
          render: (t) =>
            badge(
              taskPhase(
                t.cancel_requested && t.state === "running"
                  ? "cancelling"
                  : t.state,
              ),
              t.state === "failed" || t.outcome === "failed"
                ? "bad"
                : ["partial", "cached", "not_run"].includes(t.outcome)
                  ? "warn"
                  : t.state === "succeeded"
                    ? "good"
                    : "warn",
            ),
        },
        { label: "阶段", value: (t) => taskStage(t) },
        { label: "业务结果", value: (t) => outcome(t.outcome) },
        { label: "时间", value: (t) => date(t.updated) },
        { key: "error", label: "结果" },
      ],
      tasks,
      (t) => [
        ...(ctx.session.role === "admin" &&
        ["succeeded", "failed", "cancelled"].includes(t.state)
          ? [
              ...(!t.acknowledged
                ? [
                    button("标记已处理", async () => {
                      await api("/api/tasks/acknowledge", { id: t.id });
                      ctx.render();
                    }),
                  ]
                : []),
              button(
                "删除",
                async () => {
                  if (!confirm("删除任务 #" + t.id + " 的历史记录？")) return;
                  await api("/api/tasks/delete", { id: t.id });
                  ctx.render();
                },
                "danger",
              ),
            ]
          : []),
        ...(canViewTask(t, ctx.session)
          ? [button("详情", () => openTask(t.id, ctx.signal))]
          : []),
        ...(t.cancellable && ctx.session.role === "admin"
          ? [
              button(t.state === "running" ? "停止探测" : "取消", async () => {
                await api("/api/tasks/cancel", { id: t.id });
                ctx.render();
              }),
            ]
          : []),
      ],
      { compact, pageSize: compact ? 5 : 20 },
    ),
    ...(compact && ctx.session.role !== "viewer"
      ? [el("a", { href: "#maintenance/logs" }, "查看全部任务 →")]
      : []),
  );
}

function resultDetails(result) {
  if (!result) return [];
  if (result.retained === false)
    return [help("任务详情已按保留策略清理，最终结果仍保留。")];
  const sections = [];
  if (Array.isArray(result.items) && typeof result.selected === "number")
    sections.push(...deliveryDetails(result));
  if (typeof result.published === "boolean")
    sections.push(...refreshDetails(result));
  if (result.artifact_validation)
    sections.push(
      help(
        `输出格式：${validationLabel(result.artifact_validation)}。连接检测使用 sing-box，不代表目标客户端已实际导入。`,
      ),
    );
  if (result.target_version)
    sections.push(
      help(
        `目标版本 ${result.target_version} · 实际版本 ${result.actual_version || "等待确认"}`,
      ),
    );
  if (result.passed !== undefined)
    sections.push(
      help(
        `已通过 ${result.passed} / ${result.total} 个节点${result.skipped ? ` · ${result.skipped} 个超出检测能力，未判定连通性` : ""}${result.interrupted ? " · 检查中断，保留已完成结果" : ""}`,
      ),
    );
  if (Array.isArray(result.nodes))
    sections.push(
      table(
        [
          { key: "name", label: "节点" },
          { key: "engine", label: "连接引擎" },
          {
            label: "结果",
            value: (n) =>
              ({
                passed: "通过",
                failed: "失败",
                cancelled: "已中断",
                not_run: "未执行",
                unsupported: "无法检测（能力不支持）",
              })[n.state] || (n.success ? "通过" : "失败"),
          },
          {
            label: "耗时",
            value: (n) =>
              ["unsupported", "not_run"].includes(n.state)
                ? "—"
                : n.milliseconds + " ms",
          },
          { key: "error", label: "说明" },
        ],
        result.nodes,
      ),
    );
  if (Array.isArray(result.failures))
    sections.push(
      table(
        [
          { key: "id", label: "来源" },
          { key: "error", label: "问题" },
          { label: "最后成功", value: (v) => date(v.last_success) },
        ],
        result.failures,
      ),
    );
  if (result.outbound)
    sections.push(
      help(
        `出站 ${result.outbound} · ${result.success ? "连接成功" : "连接失败"} · ${result.milliseconds} ms`,
      ),
    );
  if (result.outputs !== undefined && !Array.isArray(result.outputs))
    sections.push(
      help(
        `可用输出 ${result.outputs} / ${result.expected_outputs ?? result.outputs}`,
      ),
    );
  return sections;
}

/** @param {import("./contracts.js").RefreshResult} result */
function refreshDetails(result) {
  return [
    help(
      `已检查 ${result.checked} 项 · 成功 ${result.updated} 项 · ${result.published ? "已发布通过校验的输出" : "本轮未发布输出"}` +
        (result.deferred
          ? ` · ${result.deferred} 项留待后续刷新，自动调度会继续处理`
          : "") +
        (result.interrupted
          ? " · 执行中断，已完成的来源抓取会在配置版本仍匹配时保存"
          : ""),
    ),
  ];
}

/** @param {import("./contracts.js").DeliveryResult} result */
function deliveryDetails(result) {
  return [
    help(
      `已选择 ${result.selected} 个投递目标 · 已处理 ${result.items.length} 个${result.interrupted ? " · 任务中断，已保存确认完成的记录" : ""}`,
    ),
    table(
      [
        { key: "id", label: "投递目标" },
        {
          label: "结果",
          value: (item) =>
            ({
              delivered: "已送达",
              unchanged: "内容未变化",
              failed: "失败",
              uncertain: "远端接收状态未知",
              interrupted: "已中断",
            })[item.status] || item.status,
        },
        {
          label: "记录",
          value: (item) =>
            item.persisted ? "已保存" : "未保存（配置变化或写入失败）",
        },
        { key: "error", label: "说明" },
      ],
      result.items,
    ),
  ];
}
