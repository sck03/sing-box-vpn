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
};
const phases = {
  queued: "等待执行",
  validating: "校验中",
  executing: "执行中",
  publishing: "发布配置",
  "applying-services": "应用服务",
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
    ["阶段", taskPhase(task.phase)],
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
    diagnostic,
    row(button("关闭", () => dialog.close())),
  );
  dialog.showModal();
}
