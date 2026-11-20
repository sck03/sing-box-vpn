import { api } from "./api.js";
import { workspace } from "./workspace.js";
import { disclosure } from "./sections.js";
import { requestConfiguration } from "./config-preview.js";
import { subscriptionStatus } from "./subscription-status.js";
import { clientCatalog, templateFields, normalizeTemplate } from "./clients.js";
import { processorFields, templateResources } from "./subscription-fields.js";
import {
  presetTools,
  accountTemplateSettings,
  resourceID,
} from "./template-settings.js";
import { subscriptionWizard } from "./subscription-wizard.js";
import { importTemplate } from "./template-import.js";
import {
  showClientCapabilities,
  verificationLabel,
} from "./client-capabilities.js";
import {
  el,
  card,
  row,
  button,
  help,
  resourceEditor,
  formDialog,
  jsonDialog,
  showResult,
  table,
} from "./ui.js";
const common = [
  { key: "id", label: "ID", required: true },
  { key: "name", label: "名称" },
];
export async function subscriptions(ctx) {
  const c = await ctx.config();
  let s = { ...c.subscriptions };
  for (const key of [
    "sources",
    "collections",
    "templates",
    "outputs",
    "rule_sets",
    "deliveries",
  ])
    s[key] ??= [];
  const request = ctx.request || api;
  let clients;
  let presets;
  const getPresets = () =>
    (presets ??= request("/api/subscription-presets").catch((error) => {
      presets = null;
      throw error;
    }));
  const getClients = () =>
    (clients ??= clientCatalog(request).catch((error) => {
      clients = null;
      throw error;
    }));
  const change = ({ local = false } = {}) => {
    c.subscriptions = s;
    ctx.changed();
    if (local) ctx.invalidateSections?.();
    else ctx.render();
  };
  const views = {
    introduction: () =>
      card(
        "订阅编译与分发",
        help(
          "组合订阅来源，选择客户端与模板，预览后发布。本机用户可在「权益与用量」直接创建分享。",
        ),
        disclosure(
          "客户端适配与验证范围",
          async () => [
            help(
              "原生核心校验与节点连通性分开报告。实验适配表示尚未通过目标应用实机导入；证书固定、传输方式与端口跳跃还需逐节点匹配。",
            ),
            table(
              [
                { key: "label", label: "客户端" },
                {
                  label: "平台",
                  value: (p) => (p.platforms || []).join(" / "),
                },
                { label: "支持输出", value: (p) => p.modes.join(" / ") },
                {
                  label: "验证",
                  value: verificationLabel,
                },
              ],
              Object.values(await getClients()),
              (profile) => [
                button("字段范围", () => showClientCapabilities(profile)),
              ],
            ),
          ],
          ctx.signal,
        ),
        row(
          button(
            "快速创建订阅",
            async () =>
              subscriptionWizard(
                ctx,
                c,
                await getClients(),
                await getPresets(),
              ),
            "primary",
          ),
          button("预览", async () =>
            requestConfiguration(
              (signal) => request("/api/config/preview", c, { signal }),
              [],
              {
                signal: ctx.signal,
                loadOutput: (id, signal) =>
                  request(
                    "/api/config/preview-output",
                    { id, config: c },
                    { signal },
                  ),
              },
            ),
          ),
          button("刷新来源", () =>
            ctx.task({
              operation: "subscription",
              subscription: { operation: "refresh" },
            }),
          ),
          button("高级配置", () =>
            jsonDialog("完整订阅配置", s, (v) => {
              s = v;
              change();
            }),
          ),
          button("通知设置", () =>
            formDialog(
              "订阅事件通知",
              [
                {
                  key: "webhook",
                  label: "Webhook URL",
                  type: "url",
                  wide: true,
                  help: "来源异常、到期和分发状态变化时发送通知。留空停用。",
                },
              ],
              s,
              (v) => {
                s.webhook = v.webhook;
                change();
              },
            ),
          ),
          el("a", { href: "#access" }, "设置公网访问与订阅地址 →"),
        ),
      ),
    sources: async () => {
      const capabilities = await request("/api/subscription-capabilities");
      return resourceEditor({
        title: "订阅来源",
        local: true,
        items: s.sources,
        fields: [
          ...common,
          {
            key: "kind",
            label: "类型",
            type: "select",
            options: ["account", "remote", "inline"],
          },
          {
            key: "account_id",
            label: "权益 ID（账户来源）",
            type: "select",
            options: (await request("/api/account-options")).map((a) => [
              a.id,
              a.name,
            ]),
            required: true,
            visible: (v) => v.kind === "account",
          },
          {
            key: "scope",
            label: "本地范围",
            type: "select",
            options: ["normal", "vpngate", ""],
            visible: (v) => v.kind === "account",
          },
          {
            key: "url",
            label: "远端 URL",
            visible: (v) => v.kind === "remote",
          },
          {
            key: "format",
            label: "格式",
            type: "select",
            options: capabilities.formats,
            visible: (v) => v.kind !== "account",
          },
          {
            key: "user_agent",
            label: "订阅 User-Agent",
            advanced: true,
            visible: (v) => v.kind === "remote",
          },
          {
            key: "headers",
            label: "请求头 JSON",
            type: "json",
            default: {},
            wide: true,
            advanced: true,
            visible: (v) => v.kind === "remote",
            help: "支持需要认证的来源；携带自定义头时仅跟随同源跳转。",
          },
          {
            key: "content",
            label: "内联内容",
            type: "textarea",
            wide: true,
            visible: (v) => v.kind === "inline",
          },
          { key: "required", label: "必需来源", type: "bool" },
          {
            key: "skip_invalid",
            label: "跳过异常或不支持的节点",
            type: "bool",
            visible: (v) => v.kind !== "account",
            help: "保留有效节点并报告跳过原因；全部无效时仍会拒绝更新。",
          },
          {
            key: "interval_seconds",
            label: "刷新间隔（秒）",
            type: "number",
            min: 0,
            max: 2592000,
            help: "0 使用默认间隔；其他值为 60 秒至 30 天。",
          },
          {
            key: "max_stale_seconds",
            label: "最长陈旧时间（秒）",
            type: "number",
            min: 0,
            max: 2592000,
          },
        ],
        defaults: {
          id: "",
          name: "",
          kind: "remote",
          format: "auto",
          skip_invalid: true,
          required: false,
          interval_seconds: 3600,
          max_stale_seconds: 86400,
        },
        columns: [
          { key: "id", label: "ID" },
          { key: "kind", label: "类型" },
          { key: "name", label: "名称" },
        ],
        normalize: (v) => {
          if (v.kind !== "remote") {
            delete v.url;
            delete v.user_agent;
            delete v.headers;
          }
          if (v.kind !== "inline") delete v.content;
          if (v.kind !== "account") {
            delete v.account_id;
            delete v.scope;
          }
          return v;
        },
        change,
      });
    },
    collections: () =>
      resourceEditor({
        title: "节点集合",
        local: true,
        items: s.collections,
        fields: [
          ...common,
          {
            key: "sources",
            label: "选择来源",
            type: "multiselect",
            options: s.sources.map((v) => [v.id, v.name || v.id]),
          },
          ...processorFields,
        ],
        defaults: {
          id: "",
          name: "",
          sources: [],
          processor: { deduplicate: true, sort: true },
        },
        change,
      }),
    templates: async (selectTemplate) => {
      const catalog = await getClients();
      return resourceEditor({
        title: "客户端模板",
        pageSize: 8,
        extraActions: (template) =>
          template.mode === "config" &&
          catalog[template.client]?.features.policy_groups
            ? [
                button("策略与规则", () => selectTemplate?.(template.id)),
                button("来源与版本", () =>
                  showResult("模板来源", template.origin || "本地自定义模板"),
                ),
              ]
            : [
                button("来源与版本", () =>
                  showResult("模板来源", template.origin || "本地自定义模板"),
                ),
              ],
        items: s.templates,
        fields: templateFields(catalog),
        normalize: (v) => normalizeTemplate(v, catalog),
        preserve: true,
        defaults: {
          id: resourceID("template"),
          name: "",
          version: 1,
          client: "sing-box",
          mode: "config",
          preset: "proxy",
          rules: [],
        },
        columns: [
          {
            label: "模板",
            value: (t) => t.name || t.id,
            search: (t) => `${t.name || ""} ${t.id} ${t.client} ${t.mode}`,
            render: (t) =>
              el(
                "div",
                { class: "template-list-label" },
                el("strong", { title: t.id }, t.name || t.id),
                el(
                  "small",
                  {},
                  `${catalog[t.client]?.label || t.client} · ${{ config: "完整配置", provider: "节点 Provider", nodes: "节点列表", rules: "规则" }[t.mode] || t.mode}`,
                ),
              ),
          },
        ],
        change,
      });
    },
    rules: () =>
      resourceEditor({
        title: "规则资源",
        items: s.rule_sets,
        fields: [
          { key: "id", label: "ID", required: true },
          { key: "url", label: "远端规则 URL" },
          { key: "rules", label: "规则 JSON", type: "json", wide: true },
        ],
        defaults: { id: "", rules: [] },
        columns: [
          { key: "id", label: "ID" },
          { key: "url", label: "远端 URL" },
        ],
        change,
      }),
    outputs: () =>
      resourceEditor({
        title: "订阅输出",
        local: true,
        items: s.outputs,
        fields: [
          ...common,
          {
            key: "collection",
            label: "集合 ID",
            type: "select",
            options: s.collections.map((v) => v.id),
          },
          {
            key: "template",
            label: "模板 ID",
            type: "select",
            options: s.templates.map((v) => v.id),
          },
          {
            key: "skip_unsupported",
            label: "显式跳过不支持的节点",
            type: "bool",
          },
        ],
        defaults: {
          id: "",
          name: "",
          collection: s.collections[0]?.id,
          template: s.templates[0]?.id,
        },
        columns: [
          { key: "id", label: "ID" },
          { key: "collection", label: "集合" },
          { key: "template", label: "模板" },
        ],
        change,
      }),
    deliveries: () =>
      resourceEditor({
        title: "分发目的地",
        local: true,
        items: s.deliveries,
        fields: [
          { key: "id", label: "ID", required: true },
          {
            key: "kind",
            label: "渠道",
            type: "select",
            options: [
              "http-put",
              "webdav",
              "github-gist",
              "telegram",
              "gitlab",
            ],
          },
          {
            key: "output",
            label: "输出 ID",
            type: "select",
            options: s.outputs.map((v) => v.id),
          },
          { key: "url", label: "目的地 URL" },
          { key: "username", label: "用户名 / Telegram chat ID / GitLab 分支" },
          { key: "password", label: "密码", type: "password" },
          { key: "token", label: "访问令牌", type: "password" },
          { key: "filename", label: "文件名" },
          { key: "automatic", label: "自动分发", type: "bool" },
        ],
        defaults: {
          id: "",
          kind: "http-put",
          output: s.outputs[0]?.id,
          url: "",
          automatic: false,
        },
        columns: [
          { key: "id", label: "ID" },
          { key: "kind", label: "渠道" },
          { key: "output", label: "输出" },
        ],
        change,
      }),
    sharing: async () => {
      const applied = await request("/api/grant-options");
      const reason =
        applied.unavailable ||
        (!applied.outputs.length && "请先创建并应用一个订阅输出。");
      return card(
        "输出下载授权",
        help(
          "允许下载已应用的输出。留空表示不限，0 禁止下载。创建和撤销立即生效；授权与计数保存在数据库。",
        ),
        applied.pending || ctx.dirty
          ? help("配置有待应用的更改。当前只能分享已生效的输出。")
          : null,
        reason
          ? help(reason)
          : button("创建输出分享", () =>
              formDialog(
                "输出授权",
                [
                  { key: "id", label: "授权 ID", required: true },
                  {
                    key: "outputs",
                    label: "选择输出",
                    type: "multiselect",
                    options: applied.outputs.map((v) => [v.id, v.name || v.id]),
                    required: true,
                  },
                  { key: "expires_at", label: "下载到期", type: "date" },
                  {
                    key: "max_downloads",
                    label: "下载次数（留空不限，0 禁止）",
                    type: "number",
                    nullable: true,
                    min: 0,
                  },
                ],
                { id: "", outputs: [], max_downloads: null },
                async (v) => {
                  if (!v.outputs.length) throw new Error("请选择已生效的输出");
                  const result = await api("/api/grants", v);
                  showResult("请保存分享链接", result.urls.join("\n"));
                  ctx.render();
                },
              ),
            ),
        el("p", {}, el("a", { href: "#grants" }, "查看与撤销分享链接 →")),
      );
    },
    diagnostics: async () => {
      const applied = await request("/api/subscriptions/actions");
      return card(
        "诊断与立即分发",
        help(
          applied.pending || ctx.dirty
            ? "草稿有待应用的更改，以下操作使用已生效配置。"
            : "以下操作使用已生效配置。",
        ),
        row(
          applied.outputs.length
            ? button("检测输出", () =>
                formDialog(
                  "检测",
                  [
                    {
                      key: "id",
                      label: "输出 ID",
                      type: "select",
                      options: applied.outputs.map((v) => [
                        v.id,
                        v.name || v.id,
                      ]),
                    },
                    { key: "probe_url", label: "探测 URL" },
                  ],
                  {
                    id: applied.outputs[0]?.id,
                    probe_url: "https://www.gstatic.com/generate_204",
                  },
                  (v) =>
                    ctx.task({
                      operation: "subscription",
                      subscription: { operation: "check", ...v },
                    }),
                ),
              )
            : help("尚无已生效输出，请先保存并应用。"),
          applied.deliveries.length
            ? button("分发输出", () =>
                formDialog(
                  "立即分发",
                  [
                    {
                      key: "id",
                      label: "目的地",
                      type: "select",
                      options: applied.deliveries.map((v) => v.id),
                    },
                  ],
                  {},
                  (v) =>
                    ctx.task({
                      operation: "subscription",
                      subscription: { operation: "deliver", ...v },
                    }),
                ),
              )
            : help("尚无已生效分发目的地。"),
        ),
      );
    },
  };
  return [
    views.introduction(),
    workspace(
      "subscriptions",
      {
        sources: views.sources,
        collections: views.collections,
        templates: async () => {
          const catalog = await getClients();
          const resources = templateResources(s.templates, catalog, change);
          const selectTemplate = (id) => {
            resources.selectTemplate?.(id);
            resources.scrollIntoView?.({
              block: "nearest",
              behavior: "smooth",
            });
          };
          return [
            presetTools(s, catalog, await getPresets(), change),
            row(
              button("导入模板 / 路由规则", () =>
                importTemplate(ctx, s, catalog, change),
              ),
            ),
            el(
              "div",
              { class: "template-workspace" },
              await views.templates(selectTemplate),
              resources,
            ),
            disclosure("共享规则资源", views.rules, ctx.signal),
            disclosure(
              "账户默认模板与覆盖",
              () => accountTemplateSettings(s, catalog, ctx, change),
              ctx.signal,
            ),
          ];
        },
        outputs: async () => [views.outputs(), await views.sharing()],
        deliveries: async () => [views.deliveries(), await views.diagnostics()],
        status: () => subscriptionStatus(ctx),
      },
      ctx,
    ),
  ];
}
