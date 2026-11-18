import { api } from "./api.js";
import { workspace } from "./workspace.js";
import { showConfiguration } from "./config-preview.js";
import { clientCatalog, templateFields, normalizeTemplate } from "./clients.js";
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
  const c = await ctx.config(),
    s = c.subscriptions;
  const request = ctx.request || api;
  const catalog = await clientCatalog(request);
  const accountOptions = await request("/api/account-options");
  s.rule_sets ??= [];
  const change = () => {
    ctx.changed();
    ctx.render();
  };
  const content = [
    card(
      "订阅编译与分发",
      help(
        "本机独立账户请在「下载授权」创建分享。要自定义模板或分发，添加账户来源，再创建集合和输出。新安装不发布共用入口凭据。第三方节点只能控制下载，不能执行代理流量额度。",
      ),
      el(
        "details",
        { class: "advanced" },
        el("summary", {}, "客户端适配与验证范围"),
        help(
          "原生核心校验与节点连通性分开报告。实验适配表示尚未通过目标应用实机导入；证书固定、传输方式与端口跳跃还需逐节点匹配。",
        ),
        table(
          [
            { key: "label", label: "客户端" },
            { label: "支持输出", value: (p) => p.modes.join(" / ") },
            {
              label: "验证",
              value: (p) =>
                p.tested_version
                  ? p.verification + " · " + p.tested_version
                  : p.verification,
            },
          ],
          Object.values(catalog),
        ),
      ),
      row(
        button(
          "预览",
          async () => showConfiguration(await api("/api/config/preview", c)),
          "primary",
        ),
        button("刷新来源", () =>
          ctx.task({
            operation: "subscription",
            subscription: { operation: "refresh" },
          }),
        ),
        button("高级配置", () =>
          jsonDialog("完整订阅配置", s, (v) => {
            c.subscriptions = v;
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
        button("下载监听", () =>
          formDialog(
            "下载监听",
            [
              { key: "enabled", label: "启用独立下载监听", type: "bool" },
              { key: "listen", label: "监听 IP" },
              {
                key: "port",
                label: "端口",
                type: "number",
                min: 1,
                max: 65535,
              },
              { key: "public_base_url", label: "公网下载 URL（推荐 HTTPS）" },
            ],
            c.listener,
            (v) => {
              c.listener = v;
              change();
            },
          ),
        ),
      ),
    ),
    resourceEditor({
      title: "订阅来源",
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
          options: accountOptions.map((a) => [a.id, a.name]),
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
        { key: "url", label: "远端 URL", visible: (v) => v.kind === "remote" },
        {
          key: "format",
          label: "格式",
          type: "select",
          options: ["uri", "sing-box", "mihomo", "base64", "nodes"],
        },
        {
          key: "content",
          label: "内联内容",
          type: "textarea",
          wide: true,
          visible: (v) => v.kind === "inline",
        },
        { key: "required", label: "必需来源", type: "bool" },
        { key: "interval_seconds", label: "刷新间隔（秒）", type: "number" },
        {
          key: "max_stale_seconds",
          label: "最长陈旧时间（秒）",
          type: "number",
        },
      ],
      defaults: {
        id: "",
        name: "",
        kind: "remote",
        format: "uri",
        required: false,
        interval_seconds: 3600,
        max_stale_seconds: 86400,
      },
      columns: [
        { key: "id", label: "ID" },
        { key: "kind", label: "类型" },
        { key: "name", label: "名称" },
      ],
      change,
    }),
    resourceEditor({
      title: "节点集合",
      items: s.collections,
      fields: [
        ...common,
        {
          key: "sources",
          label: "选择来源",
          type: "multiselect",
          options: s.sources.map((v) => [v.id, v.name || v.id]),
        },
        {
          key: "processor",
          label: "过滤 / 排序 / 去重",
          type: "json",
          wide: true,
        },
      ],
      defaults: { id: "", name: "", sources: [], processor: {} },
      change,
    }),
    resourceEditor({
      title: "客户端模板",
      items: s.templates,
      fields: templateFields(catalog),
      normalize: (v) => normalizeTemplate(v, catalog),
      defaults: {
        id: "",
        version: 1,
        client: "sing-box",
        mode: "config",
        preset: "proxy",
        rules: [],
      },
      columns: [
        { key: "id", label: "ID" },
        { key: "client", label: "客户端" },
        { key: "mode", label: "模式" },
      ],
      change,
    }),
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
    resourceEditor({
      title: "订阅输出",
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
    resourceEditor({
      title: "分发目的地",
      items: (s.deliveries ??= []),
      fields: [
        { key: "id", label: "ID", required: true },
        {
          key: "kind",
          label: "渠道",
          type: "select",
          options: ["http-put", "webdav", "github-gist", "telegram", "gitlab"],
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
    card(
      "输出下载授权",
      help(
        "允许下载已应用的输出。留空表示不限，0 禁止下载。创建和撤销立即生效；授权与计数保存在数据库。",
      ),
      button("创建输出分享", () =>
        formDialog(
          "输出授权",
          [
            { key: "id", label: "授权 ID", required: true },
            {
              key: "outputs",
              label: "选择输出",
              type: "multiselect",
              options: s.outputs.map((v) => [v.id, v.name || v.id]),
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
            if (
              !v.outputs.length ||
              v.outputs.some((id) => !s.outputs.some((o) => o.id === id))
            )
              throw new Error("请选择存在的输出");
            const result = await api("/api/grants", v);
            showResult("请保存分享链接", result.urls.join("\n"));
            ctx.render();
          },
        ),
      ),
      el("p", {}, el("a", { href: "#grants" }, "查看与撤销全部下载授权 →")),
    ),
    card(
      "诊断与立即分发",
      row(
        button("检测输出", () =>
          formDialog(
            "检测",
            [
              {
                key: "id",
                label: "输出 ID",
                type: "select",
                options: s.outputs.map((v) => v.id),
              },
              { key: "probe_url", label: "探测 URL" },
            ],
            {
              id: s.outputs[0]?.id,
              probe_url: "https://www.gstatic.com/generate_204",
            },
            (v) =>
              ctx.task({
                operation: "subscription",
                subscription: { operation: "check", ...v },
              }),
          ),
        ),
        button("分发输出", () =>
          formDialog(
            "立即分发",
            [
              {
                key: "id",
                label: "目的地",
                type: "select",
                options: (s.deliveries || []).map((v) => v.id),
              },
            ],
            {},
            (v) =>
              ctx.task({
                operation: "subscription",
                subscription: { operation: "deliver", ...v },
              }),
          ),
        ),
      ),
    ),
  ];
  return [
    content[0],
    workspace("subscriptions", [
      ["来源", [content[1]]],
      ["集合", [content[2]]],
      ["模板与规则", [content[3], content[4]]],
      ["输出与授权", [content[5], content[7]]],
      ["分发与检测", [content[6], content[8]]],
    ]),
  ];
}
