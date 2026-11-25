import { accountFields } from "./account-fields.js";
import {
  openAccountBindingTask,
  accountCreated,
} from "./account-onboarding.js";
import { api, exportAccount } from "./api.js";
import { saveAccount, archiveAccount, inboundOptions, accountOptions, accountCredentials } from "./account-api.js";
import { createAccountShare } from "./sharing.js";
import { accountOutputDialog } from "./account-outputs.js";
import { showExport } from "./export-results.js";
import { loadRecords } from "./details.js";
import { dialogSession, loadDialog } from "./dialog-session.js";
import { disclosure } from "./sections.js";
import {
  el,
  card,
  button,
  help,
  table,
  remoteTable,
  formDialog,
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
  const edit = async (item) =>
    loadDialog(
      ctx.signal,
      item.revision ? "编辑权益" : "新建权益",
      (signal) =>
        Promise.all([
          (ctx.workflowRequest || api)("/api/subscribers", undefined, {
            signal,
          }),
          item.revision ? [] : bindingFields(signal),
        ]),
      ([subscribers, bindings]) =>
        formDialog(
          item.revision ? "编辑权益" : "新建权益",
          [
            ...accountFields(!!item.revision, subscribers),
            ...(!item.revision
              ? [
                  {
                    key: "bind_now",
                    label: "创建后自动绑定节点",
                    type: "bool",
                    section: "可连接的节点",
                    readonly: !bindings[0]?.options.length,
                    help: bindings[0]?.options.length
                      ? "绑定成功后可直接创建分享链接。"
                      : "尚无已应用的节点，可先创建权益，稍后再绑定。",
                  },
                  ...bindings.map((field) => ({
                    ...field,
                    visible: (v) => v.bind_now,
                  })),
                ]
              : []),
          ],
          {
            ...item,
            bind_now: !!bindings[0]?.options.length,
            scope: bindings[0]?.options[0]?.[0] || "normal",
          },
          async (v) => {
            /** @type {import('./contracts.js').AccountSave['account']} */
            const account = { id: v.id, name: v.name };
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
            await saveAccount({ account });
            notice("权益已保存，相关订阅会自动更新");
            ctx.render();
            if (!item.revision) {
              if (!v.bind_now) accountCreated(account, bind);
              else {
                try {
                  const id = await ctx.task({
                    operation: "account",
                    account: {
                      operation: "bind",
                      id: account.id,
                      scope: v.scope,
                      inbound_ids: v.inbound_ids,
                    },
                  });
                  await openAccountBindingTask(ctx, account, id, bind);
                } catch (error) {
                  accountCreated(
                    account,
                    bind,
                    "权益已经保存，绑定未排队：" + error.message,
                  );
                }
              }
            }
          },
        ),
    );
  const bindingFields = async (signal) => {
    const scopes = ["normal", "vpngate"];
    const choices = await Promise.all(
      scopes.map((scope) =>
        inboundOptions(scope, signal, ctx.workflowRequest || api),
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
        ].filter((_, index) => choices[index].length),
      },
      {
        key: "inbound_ids",
        label: "入站入口（可多选）",
        type: "multiselect",
        wide: true,
        options: (value) =>
          (choices[scopes.indexOf(value.scope)] || []).map((entry) => [
            entry.id,
            entry.name,
          ]),
        required: true,
        initial: (value) => (choices[scopes.indexOf(value.scope)] || []).map((entry) => entry.id),
        resetOn: ["scope"],
        help: "默认勾选当前启用入口，可搜索后调整。至少选择一个；排队期间新增的入口不会自动加入。",
      },
    ];
  };
  const bind = async (item, signal) =>
    loadDialog(signal, "绑定入站", bindingFields, (fields) =>
      formDialog(
        "绑定入站",
        [
          ...fields,
          { key: "uuid", label: "自定义 UUID（留空随机）", advanced: true },
          {
            key: "password",
            label: "自定义密码（16–128 字节，留空随机）",
            type: "password",
            advanced: true,
          },
        ],
        { scope: fields[0].options[0]?.[0] || "" },
        async (v) => {
          const id = await ctx.task({
            operation: "account",
            account: { operation: "bind", id: item.id, ...v },
          });
          await openAccountBindingTask(ctx, item, id, bind);
        },
      ),
    );
  const credentials = async (item) => {
    const session = dialogSession(ctx.signal);
    session.replace(el("h2", {}, "凭据绑定 · " + item.name), help("正在加载…"));
    let all;
    try {
      all = (
        await Promise.all(
          ["normal", "vpngate"].map((scope) =>
            accountCredentials(item.id, scope, session.signal),
          ),
        )
      ).flat();
    } catch (error) {
      session.fail(error);
      return;
    }
    session.replace(
      el("h2", {}, "凭据绑定 · " + item.name),
      help(
        "轮换和解除绑定会立即撤销旧代次；新凭据发布后自动重载对应入站，该入站连接可能短暂中断，其他入站继续运行。",
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
          button("轮换", () => {
            session.close();
            formDialog("轮换凭据", [
              { key: "uuid", label: "自定义 UUID（留空随机）", advanced: true },
              { key: "password", label: "自定义密码（留空随机）", type: "password", minBytes: 16, maxBytes: 128, advanced: true },
              { key: "confirmed", label: "撤销旧凭据并重载对应入站（该入站连接可能短暂中断）", type: "bool", required: true },
            ], {}, async ({ uuid, password, confirmed }) => {
              if (!confirmed) throw new Error("请确认撤销旧凭据");
              await ctx.task({
                operation: "account",
                account: {
                  operation: "rotate",
                  id: c.id,
                  generation: c.generation,
                  scope: c.runtime_id,
                  uuid,
                  password,
                },
              });
            });
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
        ? button("批量绑定入站", () =>
            loadDialog(
              ctx.signal,
              "批量绑定入站",
              (signal) =>
                Promise.all([
                  accountOptions(signal, ctx.workflowRequest || api),
                  bindingFields(signal),
                ]),
              ([accounts, fields]) =>
                formDialog(
                  "批量绑定（最多 100 个权益）",
                  [
                    {
                      key: "account_ids",
                      label: "权益（可多选，最多 100 个）",
                      type: "multiselect",
                      maxSelected: 100,
                      options: accounts.map((item) => [item.id, item.name]),
                      required: true,
                      wide: true,
                    },
                    ...fields,
                  ],
                  { account_ids: [], scope: fields[0].options[0]?.[0] || "" },
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
            ),
          )
        : null,
      await remoteTable(
        [
          {
            key: "name",
            label: "权益",
            render: (a) =>
              el(
                "div",
                { class: "account-identity" },
                el("strong", {}, a.name),
                ...(a.name !== a.id
                  ? [el("small", { class: "help" }, a.id)]
                  : []),
              ),
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
            render: (a) =>
              el(
                "div",
                { class: "quota-cell" },
                el(
                  "span",
                  {},
                  `${bytes(a.charged_bytes)} / ${bytes(a.quota_bytes)}`,
                ),
                el(
                  "small",
                  { class: "muted" },
                  a.quota_bytes === null
                    ? "不限额度"
                    : "剩余 " +
                        bytes(Math.max(0, a.quota_bytes - a.charged_bytes)),
                ),
                ...(a.quota_bytes !== null
                  ? [
                      el("progress", {
                        max: 100,
                        value:
                          a.quota_bytes > 0
                            ? Math.min(
                                100,
                                (100 * a.charged_bytes) / a.quota_bytes,
                              )
                            : 100,
                        "aria-label": a.name + " 已用额度",
                        class: a.state === "exhausted" ? "exhausted" : "",
                      }),
                    ]
                  : []),
              ),
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
          {
            label: "代理到期",
            secondary: true,
            value: (a) => date(a.effective_expires_at),
          },
          {
            label: "下次重置",
            secondary: true,
            value: (a) => date(a.next_reset),
          },
        ],
        (params, signal) =>
          request(
            "/api/accounts?" +
              new URLSearchParams({
                ...params,
                archived: lifecycle.value === "archived",
              }),
            undefined,
            { signal },
          ),
        (item) => [
          ...(admin && !item.archived
            ? [
                ...(item.download_denial
                  ? [
                      button(
                        "绑定",
                        () => bind(item, ctx.signal),
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
                      button("绑定", () => bind(item, ctx.signal)),
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
                    await archiveAccount({
                      id: item.id,
                      revision: item.revision,
                    });
                    await list.refresh(true);
                    notice("权益已归档");
                  },
                  "danger",
                ),
                button("导出 / 二维码", async () =>
                  accountOutputDialog({
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
                      const result = await exportAccount({
                        id: item.id,
                        scope: v.scope,
                        profile: v.profile,
                        tuic_port: v.tuic_port,
                      });
                      showExport(result, v.profile);
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
