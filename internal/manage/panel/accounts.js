import { api } from "./api.js";
import { clientCatalog, clientOptions } from "./clients.js";
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
  active: "正常",
  not_started: "未开始",
  exhausted: "额度耗尽",
  expired: "已到期",
  disabled: "已禁用",
  subscriber_disabled: "订阅者已停用",
};
export async function accounts(ctx) {
  const request = ctx.request || api;
  const people = await request("/api/subscribers");
  const admin = ctx.session.role === "admin";
  const fields = (existing) => [
    { key: "id", label: "权益 ID", required: true, readonly: existing },
    { key: "name", label: "名称", required: true },
    {
      key: "subscriber_id",
      label: "所属订阅者",
      type: "select",
      options: [["", "独立权益"], ...people.map((p) => [p.id, p.name])],
    },
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
      type: "date",
      readonly: existing,
    },
    {
      key: "duration_seconds",
      label: "有效时长（天，0 不限）",
      type: "number",
      scale: 86400,
      readonly: existing,
      min: 0,
    },
    {
      key: "period.kind",
      label: "额度重置",
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
      type: "number",
      readonly: existing,
    },
    {
      key: "period.day",
      label: "每月几号（短月取月末）",
      type: "number",
      readonly: existing,
      min: 1,
      max: 31,
    },
    {
      key: "period.timezone",
      label: "IANA 时区",
      readonly: existing,
      help: "例如 Asia/Shanghai、America/New_York",
    },
  ];
  const edit = (item) =>
    formDialog(
      item.revision ? "编辑权益" : "新建权益",
      fields(!!item.revision),
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
        notice("权益已保存，运行时会检查最新状态");
        ctx.render();
      },
    );
  const bindingFields = async () => {
    const scopes = ["normal", "vpngate"];
    const choices = await Promise.all(
      scopes.map((scope) => request("/api/inbound-options?scope=" + scope)),
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
        { key: "uuid", label: "自定义 UUID（留空随机）" },
        {
          key: "password",
          label: "自定义密码（16–128 字节，留空随机）",
          type: "password",
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
    const all = (
      await Promise.all(
        ["normal", "vpngate"].map((scope) =>
          api(
            `/api/credentials?id=${encodeURIComponent(item.id)}&scope=${scope}`,
          ),
        ),
      )
    ).flat();
    const dialog = document.querySelector("#dialog");
    dialog.replaceChildren(
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
              dialog.close();
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
                dialog.close();
              }
            },
            "danger",
          ),
        ],
      ),
      button("关闭", () => dialog.close()),
    );
    dialog.showModal();
  };
  const content = [
    card(
      "独立权益与用量",
      help(
        "同一订阅者可以有多个独立权益。普通与 VPNGate 绑定同一权益时共享额度。charged 包含持久预留；上下行表示已结算有效载荷。周期边界会关闭旧周期连接，客户端重连后进入新周期。",
      ),
      admin
        ? button(
            "新建权益",
            () =>
              edit({
                id: "",
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
                  options: (await request("/api/account-options")).map(
                    (item) => [item.id, item.name],
                  ),
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
          { key: "name", label: "名称" },
          { key: "id", label: "权益 ID" },
          {
            label: "状态",
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
          },
          { label: "代理到期", value: (a) => date(a.expires_at) },
          { label: "下次重置", value: (a) => date(a.next_reset) },
        ],
        (params) => request("/api/accounts?" + new URLSearchParams(params)),
        (item) => [
          ...(admin
            ? [
                button("编辑", () => edit(item)),
                button("绑定", () => bind(item)),
                button("凭据", () => credentials(item)),
                button("导出 / 二维码", () =>
                  formDialog(
                    "账户专属导出",
                    [
                      {
                        key: "scope",
                        label: "实例",
                        type: "select",
                        options: ["normal", "vpngate", ""],
                      },
                      {
                        key: "client",
                        label: "格式",
                        type: "select",
                        options: ["uri", "sing-box", "mihomo"],
                      },
                      {
                        key: "tuic_port",
                        label: "指定 TUIC 端口（0 使用主端口）",
                        type: "number",
                      },
                    ],
                    { scope: "normal", client: "uri", tuic_port: 0 },
                    async (v) => {
                      const result = await api("/api/accounts/export", {
                        id: item.id,
                        ...v,
                      });
                      showResult("账户配置", result.content);
                      if (v.client === "uri") {
                        const dialog = document.querySelector("#dialog");
                        for (const link of result.content.trim().split("\n")) {
                          const image = await api("/api/qr", { content: link });
                          dialog.append(
                            el("img", {
                              src: image.image,
                              alt: "账户节点二维码",
                              width: 240,
                              height: 240,
                            }),
                          );
                        }
                      }
                    },
                  ),
                ),
              ]
            : []),
          button("历史", async () =>
            showResult(
              "历史账期",
              await api("/api/history?id=" + encodeURIComponent(item.id)),
            ),
          ),
        ],
        {
          signal: ctx.signal,
          label: "搜索权益",
          placeholder: "搜索 ID、名称或订阅者…",
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
  ctx.updateOverview = () => {
    void list.refresh();
  };
  return content;
}

export async function subscribers(ctx) {
  const people = await (ctx.request || api)("/api/subscribers");
  const edit = (v) =>
    formDialog(
      "订阅者",
      [
        { key: "id", label: "ID", required: true, readonly: !!v.revision },
        { key: "name", label: "名称", required: true },
        { key: "disabled", label: "禁用全部所属权益", type: "bool" },
      ],
      v,
      async (v) => {
        await api("/api/subscribers", v);
        ctx.render();
      },
    );
  return [
    card(
      "订阅者",
      help(
        "禁用订阅者会阻止其全部权益的代理访问和下载。订阅者不是面板管理员。",
      ),
      ctx.session.role === "admin"
        ? button(
            "新增订阅者",
            () => edit({ id: "", name: "", disabled: false, revision: 0 }),
            "primary",
          )
        : null,
      table(
        [
          { key: "id", label: "ID" },
          { key: "name", label: "名称" },
          {
            label: "状态",
            render: (v) =>
              badge(v.disabled ? "禁用" : "正常", v.disabled ? "bad" : "good"),
          },
        ],
        people,
        ctx.session.role === "admin"
          ? (v) => [button("编辑", () => edit(v))]
          : null,
      ),
    ),
  ];
}

export async function grants(ctx) {
  const request = ctx.request || api;
  const [accounts, catalog] = await Promise.all([
    request("/api/account-options"),
    clientCatalog(request),
  ]);
  return [
    card(
      "下载授权",
      help(
        "下载令牌只决定是否能够获取配置。代理到期、流量限额与凭据轮换由权益管理。创建后令牌只展示一次，之后只能撤销重建。",
      ),
      button(
        "创建账户分享",
        () =>
          formDialog(
            "新建下载授权",
            [
              { key: "id", label: "授权 ID", required: true },
              {
                key: "account_id",
                label: "使用权益",
                type: "select",
                options: accounts.map((a) => [a.id, a.name]),
              },
              {
                key: "scope",
                label: "运行实例",
                type: "select",
                options: [
                  ["", "普通 + VPNGate"],
                  ["normal", "普通"],
                  ["vpngate", "VPNGate"],
                ],
              },
              {
                key: "clients",
                label: "允许客户端格式（可多选）",
                type: "multiselect",
                options: clientOptions(catalog),
                required: true,
                help: "实验适配需在目标客户端验证；无法保留证书校验参数的节点会拒绝导出。",
              },
              { key: "expires_at", label: "下载到期", type: "date" },
              {
                key: "max_downloads",
                label: "下载次数上限（留空不限，0 禁止下载）",
                type: "number",
                nullable: true,
                min: 0,
              },
            ],
            {
              id: "",
              account_id: accounts[0]?.id || "",
              scope: "",
              clients: ["sing-box", "mihomo", "uri"],
              expires_at: null,
              max_downloads: null,
            },
            async (v) => {
              const result = await api("/api/grants", v);
              showResult("请保存分享地址", result.user_url);
              ctx.render();
            },
          ),
        "primary",
      ),
      await remoteTable(
        [
          { key: "id", label: "授权 ID" },
          {
            label: "权益 / 输出",
            value: (g) => g.account_id || (g.outputs || []).join(", "),
          },
          {
            label: "下载次数",
            value: (g) => `${g.downloads} / ${g.max_downloads ?? "不限"}`,
          },
          { label: "下载到期", value: (g) => date(g.expires_at) },
          {
            label: "状态",
            value: (g) =>
              ({
                active: "有效",
                revoked: "已撤销",
                expired: "已到期",
                exhausted: "次数用尽",
              })[g.state] || "未知",
            render: (g) =>
              badge(
                {
                  active: "有效",
                  revoked: "已撤销",
                  expired: "已到期",
                  exhausted: "次数用尽",
                }[g.state] || "未知",
                g.state === "active" ? "good" : "warn",
              ),
          },
        ],
        (params) => request("/api/grants?" + new URLSearchParams(params)),
        (g) => [
          button(
            "撤销",
            async () => {
              if (
                confirm(
                  "撤销后此链接不能再下载。已经下载的代理配置仍受权益限制。",
                )
              ) {
                await api("/api/grants/revoke", { id: g.id });
                ctx.render();
              }
            },
            "danger",
          ),
        ],
        { signal: ctx.signal },
      ),
    ),
  ];
}
