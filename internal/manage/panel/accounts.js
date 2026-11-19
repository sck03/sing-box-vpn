import { api } from "./api.js";
import { createAccountShare } from "./sharing.js";
import { accountClientDialog } from "./account-clients.js";
import { showExport } from "./export-results.js";
import { loadRecords } from "./details.js";
import { dialogSession } from "./dialog-session.js";
import { disclosure } from "./sections.js";
import {
  el,
  card,
  row,
  button,
  help,
  table,
  remoteTable,
  formDialog,
  showResult,
  badge,
  bytes,
  date,
  notice,
} from "./ui.js";
const states = {
  archived: "已归档",
  active: "正常",
  not_started: "未开始",
  exhausted: "额度耗尽",
  expired: "已到期",
  disabled: "已禁用",
  subscriber_disabled: "订阅者已停用",
};
export async function accounts(ctx) {
  const request = ctx.request || api;
  const admin = ctx.session.role === "admin";
  const fields = (existing, people) => [
    {
      key: "id",
      label: "权益 ID",
      required: true,
      readonly: existing,
      advanced: true,
    },
    { key: "name", label: "名称", required: true },
    ...(people.length
      ? [
          {
            key: "subscriber_id",
            label: "用户分组（可选）",
            advanced: true,
            type: "select",
            options: [["", "独立权益"], ...people.map((p) => [p.id, p.name])],
          },
        ]
      : []),
    {
      key: "quota_bytes",
      label: "每周期额度（GiB）",
      type: "number",
      scale: 1073741824,
      nullable: true,
      min: 0,
      help: "留空不限；0 表示零额度。支持小数，保存时取最接近的整数字节。",
    },
    { key: "expires_at", label: "绝对到期时间", type: "date" },
    { key: "disabled", label: "管理员禁用", type: "bool" },
    {
      key: "start_mode",
      label: "开始策略",
      advanced: true,
      type: "select",
      readonly: existing,
      options: [
        ["created", "创建时开始"],
        ["first_use", "首次成功代理认证"],
        ["scheduled", "指定时间"],
      ],
    },
    {
      key: "starts_at",
      label: "指定开始时间",
      advanced: true,
      type: "date",
      readonly: existing,
      visible: (v) => v.start_mode === "scheduled",
    },
    {
      key: "duration_seconds",
      label: "有效时长（天，0 不限）",
      advanced: true,
      type: "number",
      scale: 86400,
      readonly: existing,
      min: 0,
    },
    {
      key: "period.kind",
      label: "额度重置",
      advanced: true,
      type: "select",
      readonly: existing,
      options: [
        ["none", "总额不重置"],
        ["days", "每 N 个日历日"],
        ["monthly", "每月指定日期"],
      ],
    },
    {
      key: "period.days",
      label: "间隔日数",
      advanced: true,
      type: "number",
      readonly: existing,
      min: 1,
      max: 3660,
      visible: (v) => v.period?.kind === "days",
    },
    {
      key: "period.day",
      label: "每月几号（短月取月末）",
      advanced: true,
      type: "number",
      readonly: existing,
      min: 1,
      max: 31,
      visible: (v) => v.period?.kind === "monthly",
    },
    {
      key: "period.timezone",
      label: "IANA 时区",
      advanced: true,
      readonly: existing,
      help: "例如 Asia/Shanghai、America/New_York",
      visible: (v) => ["days", "monthly"].includes(v.period?.kind),
    },
  ];
  const edit = async (item) =>
    formDialog(
      item.revision ? "编辑权益" : "新建权益",
      fields(
        !!item.revision,
        await (ctx.workflowRequest || api)("/api/subscribers"),
      ),
      item,
      async (v) => {
        const account = {};
        for (const key of [
          "id",
          "name",
          "subscriber_id",
          "quota_bytes",
          "expires_at",
          "disabled",
          "start_mode",
          "starts_at",
          "duration_seconds",
          "period",
          "revision",
          "created_at",
        ])
          if (v[key] !== undefined) account[key] = v[key];
        await api("/api/accounts", { account });
        notice("权益已保存，相关订阅会自动更新");
        ctx.render();
        if (!item.revision) {
          const dialog = /** @type {HTMLDialogElement} */ (document.querySelector("#dialog"));
          dialog.replaceChildren(
            el("h2", {}, "权益已创建"),
            help("接下来绑定可用入站，再创建分享地址。"),
            row(
              button("绑定入站", () => bind(account), "primary"),
              button("稍后设置", () => dialog.close()),
            ),
          );
          dialog.showModal();
        }
      },
    );
  const bindingFields = async () => {
    const scopes = ["normal", "vpngate"];
    const choices = await Promise.all(
      scopes.map((scope) =>
        (ctx.workflowRequest || api)("/api/inbound-options?scope=" + scope),
      ),
    );
    return [
      {
        key: "scope",
        label: "运行实例",
        type: "select",
        options: [
          ["normal", "普通节点"],
          ["vpngate", "VPNGate"],
        ],
      },
      {
        key: "inbound_ids",
        label: "入站入口（可多选）",
        type: "multiselect",
        wide: true,
        options: (value) =>
          choices[scopes.indexOf(value.scope)].map((entry) => [
            entry.id,
            entry.name,
          ]),
        help: "显示已应用配置中的启用入口；不选表示绑定该实例全部启用入口。",
      },
    ];
  };
  const bind = async (item) =>
    formDialog(
      "绑定入站",
      [
        ...(await bindingFields()),
        { key: "uuid", label: "自定义 UUID（留空随机）", advanced: true },
        {
          key: "password",
          label: "自定义密码（16–128 字节，留空随机）",
          type: "password",
          advanced: true,
        },
      ],
      { scope: "normal", inbound_ids: [] },
      (v) =>
        ctx.task({
          operation: "account",
          account: { operation: "bind", id: item.id, ...v },
        }),
    );
  const credentials = async (item) => {
    const session = dialogSession(ctx.signal);
    session.replace(el("h2", {}, "凭据绑定 · " + item.name), help("正在加载…"));
    let all;
    try {
      all = (
      await Promise.all(
        ["normal", "vpngate"].map((scope) =>
          api(
            `/api/credentials?id=${encodeURIComponent(item.id)}&scope=${scope}`,
            undefined,
            { signal: session.signal },
          ),
        ),
      )
    ).flat();
    } catch (error) { session.fail(error); return; }
    session.replace(
      el("h2", {}, "凭据绑定 · " + item.name),
      help(
        "轮换和解除绑定会立即撤销旧代次；加载新凭据需要重启该实例，影响该实例其他连接。",
      ),
      table(
        [
          { key: "id", label: "凭据 ID" },
          { key: "runtime_id", label: "实例" },
          { key: "inbound_id", label: "入站" },
          { key: "generation", label: "代次" },
        ],
        all,
        (c) => [
          button("轮换", async () => {
            if (confirm("立即撤销旧凭据并重启受影响实例？")) {
              await ctx.task({
                operation: "account",
                account: {
                  operation: "rotate",
                  id: c.id,
                  generation: c.generation,
                  scope: c.runtime_id,
                },
              });
              session.close();
            }
          }),
          button(
            "解除",
            async () => {
              if (confirm("解除此入口绑定并断开旧凭据？")) {
                await ctx.task({
                  operation: "account",
                  account: {
                    operation: "unbind",
                    id: c.id,
                    generation: c.generation,
                    scope: c.runtime_id,
                  },
                });
                session.close();
              }
            },
            "danger",
          ),
        ],
      ),
      button("关闭", () => session.close()),
    );
  };
  const lifecycle = el(
    "select",
    { "aria-label": "权益范围" },
    el("option", { value: "current" }, "当前权益"),
    el("option", { value: "archived" }, "归档记录"),
  );
  const content = [
    card(
      "权益记录",
      admin
        ? el(
            "ol",
            { class: "workflow", "aria-label": "账户配置流程" },
            ...[
              ["创建权益", "设置额度与有效期"],
              ["绑定入站", "选择用户可以连接的节点"],
              ["创建分享", "发送订阅链接，随时查看用量"],
            ].map(([title, detail]) =>
              el("li", {}, el("strong", {}, title), el("small", {}, detail)),
            ),
          )
        : null,
      admin
        ? button(
            "新建权益",
            () =>
              edit({
                id: "user-" + crypto.randomUUID().replaceAll("-", ""),
                name: "",
                subscriber_id: "",
                quota_bytes: null,
                expires_at: null,
                disabled: false,
                revision: 0,
                start_mode: "created",
                starts_at: null,
                duration_seconds: 0,
                period: {
                  kind: "none",
                  timezone: "Asia/Shanghai",
                  days: 30,
                  day: 1,
                },
              }),
            "primary",
          )
        : null,
      admin
        ? button("批量绑定入站", async () =>
            formDialog(
              "批量绑定（最多 100 个权益）",
              [
                {
                  key: "account_ids",
                  label: "权益（可多选，最多 100 个）",
                  type: "multiselect",
                  options: (
                    await (ctx.workflowRequest || api)("/api/account-options")
                  ).map((item) => [item.id, item.name]),
                  required: true,
                  wide: true,
                },
                ...(await bindingFields()),
              ],
              { account_ids: [], scope: "normal", inbound_ids: [] },
              (v) => {
                if (!v.account_ids.length)
                  throw new Error("请选择至少一个权益");
                if (v.account_ids.length > 100)
                  throw new Error("每次最多绑定 100 个权益");
                return ctx.task({
                  operation: "account",
                  account: { operation: "bind", ...v },
                });
              },
            ),
          )
        : null,
      await remoteTable(
        [
          {
            key: "name", label: "权益",
            render: (a) => el("div", { class: "account-identity" },
              el("strong", {}, a.name),
              ...(a.name !== a.id ? [el("small", { class: "help" }, a.id)] : [])),
          },
          {
            label: "状态",
            value: (a) => states[a.state] || a.state,
            render: (a) =>
              badge(
                states[a.state] || a.state,
                a.state === "active"
                  ? "good"
                  : a.state === "not_started"
                    ? "warn"
                    : "bad",
              ),
          },
          {
            label: "用量 / 额度",
            value: (a) => `${bytes(a.charged_bytes)} / ${bytes(a.quota_bytes)}`,
            render: (a) => el("div", { class: "quota-cell" },
              el("span", {}, `${bytes(a.charged_bytes)} / ${bytes(a.quota_bytes)}`),
              ...(a.quota_bytes !== null ? [el("progress", {
                max: 100,
                value: a.quota_bytes > 0 ? Math.min(100, 100 * a.charged_bytes / a.quota_bytes) : 100,
                "aria-label": a.name + " 已用额度",
                class: a.state === "exhausted" ? "exhausted" : "",
              })] : [])),
          },
          {
            label: "分享状态",
            value: (a) =>
              a.download_denial === "no_bound_inbound"
                ? "待绑定可用入站"
                : a.download_denial
                  ? states[a.state] || "不可分享"
                  : "可分享",
          },
          { label: "代理到期", value: (a) => date(a.effective_expires_at) },
          { label: "下次重置", value: (a) => date(a.next_reset) },
        ],
        (params) =>
          request(
            "/api/accounts?" +
              new URLSearchParams({
                ...params,
                archived: lifecycle.value === "archived",
              }),
          ),
        (item) => [
          ...(admin && !item.archived
            ? [
                ...(item.download_denial
                  ? [
                      button(
                        "绑定",
                        () => bind(item),
                        item.download_denial === "no_bound_inbound"
                          ? "primary"
                          : "",
                      ),
                    ]
                  : [
                      button(
                        "分享",
                        () => createAccountShare([item], ctx),
                        "primary",
                      ),
                      button("绑定", () => bind(item)),
                    ]),
                button("编辑", () => edit(item)),
                button("凭据", () => credentials(item)),
                button(
                  "归档",
                  async () => {
                    if (
                      !confirm(
                        `永久归档「${item.name}」？凭据与下载链接将撤销，账本和历史可在归档记录中查看。`,
                      )
                    )
                      return;
                    await api("/api/accounts/archive", {
                      id: item.id,
                      revision: item.revision,
                    });
                    await list.refresh(true);
                    notice("权益已归档");
                  },
                  "danger",
                ),
                button("导出 / 二维码", async () =>
                  accountClientDialog({
                    accounts: [item],
                    ctx,
                    title: "账户专属导出",
                    fields: [
                      {
                        key: "tuic_port",
                        label: "指定 TUIC 端口（0 使用主端口）",
                        type: "number",
                      },
                    ],
                    defaults: { scope: "", tuic_port: 0 },
                    save: async (v) => {
                      const result = await api("/api/accounts/export", {
                        id: item.id,
                        scope: v.scope,
                        client: v.client,
                        tuic_port: v.tuic_port,
                      });
                      showExport(result, v.client);
                    },
                  }),
                ),
              ]
            : []),
          button("历史", async () =>
            loadRecords(
              "历史账期",
              [
                { label: "周期开始", value: (p) => date(p.period_id) },
                { label: "计费流量", value: (p) => bytes(p.charged_bytes) },
                { label: "上传", value: (p) => bytes(p.upload_bytes) },
                { label: "下载", value: (p) => bytes(p.download_bytes) },
              ],
              "/api/history?id=" + encodeURIComponent(item.id),
              ctx.signal,
            ),
          ),
        ],
        {
          signal: ctx.signal,
          actionVersion: (a) => [
            a.id,
            a.revision,
            a.download_denial,
            a.download_scopes,
          ],
          label: "搜索权益",
          filter: lifecycle,
          placeholder: "搜索 ID、名称或用户分组…",
          sortOptions: [
            ["created", "创建时间"],
            ["name", "名称"],
            ["id", "ID"],
          ],
        },
      ),
    ),
  ];
  const list = content[0].querySelector(".remote-table");
  let lastRefresh = performance.now();
  ctx.updateOverview = () => {
    if (performance.now() - lastRefresh < 5000) return;
    lastRefresh = performance.now();
    void list.refresh();
  };
  if (admin) {
    content.push(
      el("p", {}, el("a", { href: "#grants" }, "查看与撤销分享链接 →")),
    );
    content.push(
      disclosure(
        "高级：用户分组（同一用户有多份权益时使用）",
        async () => (await import("./subscribers.js")).subscribers(ctx),
        ctx.signal,
      ),
    );
  }
  return content;
}
