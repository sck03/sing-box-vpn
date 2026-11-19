import { api } from "./api.js";
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
  const panel = el(
    "section",
    { "aria-label": "订阅运行状态" },
    card(
      "运行状态",
      help("显示已发布配置的同步与通知记录。修改草稿不会改变这里的运行状态。"),
      row(button("刷新运行状态", refresh)),
    ),
    body,
  );
  async function refresh() {
    try {
      const status = await (ctx.request || api)("/api/subscriptions/status");
      if (ctx.signal?.aborted) return;
      const notification = status.notification || {},
        n = notification.activity || { state: "disabled" };
      const facts = el("dl", { class: "detail-grid" });
      for (const [label, value] of [
        ["最近尝试", date(n.checked)],
        ["最后成功", date(n.success)],
        ["连续失败", n.failures || 0],
        [
          "最近事件",
          {
            refresh: "来源刷新",
            resource: "模板/规则同步",
            expiry: "来源到期",
            delivery: "分发结果",
          }[notification.event] ||
            notification.event ||
            "—",
        ],
        ["事件对象", notification.object_id || "—"],
      ])
        facts.append(el("dt", {}, label), el("dd", {}, value));
      body.replaceChildren(
        card(
          "已发布输出",
          table(
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
              { key: "validation", label: "实际验证" },
            ],
            status.outputs || [],
            (v) => [
              button("诊断 " + v.diagnostic_count, () =>
                showResult(
                  "输出诊断 · " + v.id,
                  (v.diagnostics || [])
                    .map(
                      (d) =>
                        `${d.stage} · ${d.name || d.node_id || ""} · ${d.message}`,
                    )
                    .join("\n") || "没有诊断记录",
                ),
              ),
            ],
          ),
        ),
        card(
          "Webhook 通知",
          badge(
            n.state === "pending" ? "尚未发送" : labels[n.state] || n.state,
            n.state === "failed" ? "bad" : n.state === "ok" ? "good" : "warn",
          ),
          facts,
          ...(n.error
            ? [el("p", { class: "form-error", role: "alert" }, n.error)]
            : []),
          help(
            notification.configured
              ? "记录最后一次事件通知的结果，不代表远端服务的实时健康状态。可在页面顶部的通知设置修改地址。"
              : "尚未配置 Webhook，可在页面顶部的通知设置启用。",
          ),
        ),
        card("远程模板与规则同步", activityTable(status.resources || [])),
        card("远端来源同步", activityTable(status.sources || [])),
      );
    } catch (error) {
      if (error.name !== "AbortError")
        body.replaceChildren(help("运行状态暂时无法读取：" + error.message));
    }
  }
  await refresh();
  return panel;
}
