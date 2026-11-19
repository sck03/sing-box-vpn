import { api } from "./api.js";
import { validationLabel } from "./validation.js";
import { table } from "./tables.js";
import { card } from "./dom.js";
import { $, el, button, row, help, badge, date } from "./dom.js";

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

export function taskDetails(task) {
  const dialog = $("#dialog");
  const failed = task.state === "failed";
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
  dialog.replaceChildren(
    el("h2", {}, "任务 #" + task.id),
    badge(failed ? "需要处理" : taskPhase(task.state), failed ? "bad" : "good"),
    details,
    ...(task.error
      ? [el("p", { class: "form-error", role: "alert" }, task.error)]
      : []),
    help(
      failed
        ? task.phase === "rollback-failed"
          ? "请先检查服务状态与日志，确认当前配置后再重新应用。"
          : "请根据错误原因修正配置或运行环境，再重新发起操作。"
        : "关闭窗口后，尚未完成的任务会继续运行。",
    ),
    ...resultDetails(task.result),
    diagnostic,
    row(button("关闭", () => dialog.close())),
  );
  dialog.showModal();
}

export function tasksCard(tasks, ctx) {
  const outcome = (value) =>
    ({
      passed: "全部通过",
      partial: "部分失败",
      failed: "失败",
      cached: "使用旧缓存",
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
          value: (t) => taskPhase(t.state),
          render: (t) =>
            badge(
              taskPhase(t.state),
              t.state === "failed"
                ? "bad"
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
          ? [
              button("详情", async () =>
                taskDetails(await api("/api/tasks/detail?id=" + t.id)),
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

function resultDetails(result) {
  if (!result) return [];
  if (result.retained === false)
    return [help("任务详情已按保留策略清理，最终结果仍保留。")];
  const sections = [];
  if (result.artifact_validation)
    sections.push(
      help(
        `输出格式：${validationLabel(result.artifact_validation)}。实际连接引擎见每个节点。`,
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
        `已通过 ${result.passed} / ${result.total} 个节点${result.interrupted ? " · 检查中断，保留已完成结果" : ""}`,
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
              })[n.state] || (n.success ? "通过" : "失败"),
          },
          { label: "耗时", value: (n) => n.milliseconds + " ms" },
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
