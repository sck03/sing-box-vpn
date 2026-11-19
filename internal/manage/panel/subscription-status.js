import { api } from "./api.js";
import { validationLabel } from "./validation.js";
import {
  el,
  card,
  row,
  button,
  help,
  table,
  badge,
  date,
  showResult,
} from "./ui.js";

const labels = {
  pending: "尚未检查",
  ok: "成功",
  failed: "失败",
  disabled: "未启用",
};
function activityTable(items) {
  return table(
    [
      { key: "name", label: "名称" },
      {
        label: "类型",
        value: (v) =>
          ({ source: "订阅来源", template: "远程模板", rules: "规则集" })[
            v.kind
          ] || v.kind,
      },
      {
        label: "状态",
        value: (v) => labels[v.state] || v.state,
        render: (v) =>
          badge(
            labels[v.state] || v.state,
            v.state === "failed" ? "bad" : v.state === "ok" ? "good" : "warn",
          ),
      },
      { label: "最近检查", value: (v) => date(v.checked) },
      { label: "最后成功", value: (v) => date(v.success) },
      { label: "下次尝试", value: (v) => date(v.next) },
      { key: "error", label: "说明" },
    ],
    items,
  );
}

export async function subscriptionStatus(ctx) {
  const body = el("div");
  const error = el("p", { class: "form-error", role: "alert", hidden: true });
  const panel = el("section", { "aria-label": "订阅运行状态" }, error, body);
  async function refresh() {
    try {
      const status = await (ctx.request || api)("/api/subscriptions/status");
      if (ctx.signal?.aborted) return;
      error.hidden = true;
      const outputs = status.outputs || [];
      const available = outputs.filter((output) => output.available).length;
      const notification = status.notification || {};
      const activity = notification.activity || { state: "disabled" };
      const content = [
        card(
          "已发布输出",
          help(
            `${available} / ${outputs.length} 个输出已发布。客户端格式检查和实际连通性分别记录。`,
          ),
          outputs.length
            ? table(
                [
                  { label: "输出", value: (v) => v.name || v.id },
                  { key: "client", label: "客户端" },
                  { key: "nodes", label: "有效节点" },
                  {
                    label: "状态",
                    value: (v) => (v.available ? "已发布" : "已撤下"),
                    render: (v) =>
                      badge(
                        v.available ? "已发布" : "已撤下",
                        v.available ? "good" : "bad",
                      ),
                  },
                  {
                    label: "实际验证",
                    value: (v) => validationLabel(v.validation),
                  },
                ],
                outputs,
                outputs.some((v) => v.diagnostic_count)
                  ? (v) =>
                      v.diagnostic_count
                        ? [
                            button("查看诊断", () =>
                              showResult(
                                "输出诊断 · " + v.id,
                                (v.diagnostics || [])
                                  .map(
                                    (d) =>
                                      `${d.stage} · ${d.name || d.node_id || ""} · ${d.message}`,
                                  )
                                  .join("\n"),
                              ),
                            ),
                          ]
                        : []
                  : undefined,
              )
            : help(
                "使用页面上方的快速创建订阅，或配置来源、集合与模板后保存应用。",
              ),
        ),
      ];
      if (notification.configured) {
        const facts = el("dl", { class: "detail-grid" });
        for (const [label, value] of [
          ["最近尝试", date(activity.checked)],
          ["最后成功", date(activity.success)],
          ["连续失败", activity.failures || 0],
          ["事件对象", notification.object_id || "—"],
        ])
          facts.append(el("dt", {}, label), el("dd", {}, value));
        content.push(
          card(
            "Webhook 通知",
            badge(
              activity.state === "pending"
                ? "尚未发送"
                : labels[activity.state] || activity.state,
              activity.state === "failed"
                ? "bad"
                : activity.state === "ok"
                  ? "good"
                  : "warn",
            ),
            facts,
            activity.error
              ? el("p", { class: "form-error" }, activity.error)
              : null,
          ),
        );
      }
      if (status.resources?.length)
        content.push(
          card("远程模板与规则同步", activityTable(status.resources)),
        );
      if (status.sources?.length)
        content.push(card("远端来源同步", activityTable(status.sources)));
      if (
        !notification.configured &&
        !status.resources?.length &&
        !status.sources?.length
      )
        content.push(
          help(
            "当前没有远端同步任务或 Webhook；本地订阅与账户下载可独立使用。",
          ),
        );
      body.replaceChildren(...content);
    } catch (failure) {
      if (failure.name === "AbortError" || ctx.signal?.aborted) return;
      error.hidden = false;
      error.replaceChildren(
        document.createTextNode("状态暂不可读取：" + failure.message + " "),
        button("重试", refresh),
      );
    }
  }
  await refresh();
  return panel;
}
