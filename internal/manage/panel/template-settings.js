import { clientOptions } from "./clients.js";
import { disclosure } from "./sections.js";
import {
  el,
  card,
  row,
  help,
  button,
  table,
  formDialog,
  resourceEditor,
} from "./ui.js";

export const resourceID = (prefix) =>
  `${prefix}-${crypto.randomUUID().replaceAll("-", "").slice(0, 12)}`;

export function presetTools(config, catalog, presets, change) {
  return card(
    "模板预设",
    help("选择用途后生成可编辑的模板；策略组、DNS 和规则仍可独立调整。"),
    row(
      button(
        "从预设添加",
        () =>
          formDialog(
            "选择客户端与模板用途",
            [
              {
                key: "client",
                label: "客户端",
                type: "select",
                options: clientOptions(catalog),
              },
              {
                key: "preset",
                label: "用途",
                type: "select",
                options: (v) =>
                  presets
                    .filter((p) => p.templates[v.client])
                    .map((p) => [p.id, p.name]),
              },
              { key: "name", label: "模板名称（可留空）" },
            ],
            { client: "sing-box", preset: "proxy", name: "" },
            (value) => {
              const preset = presets.find((p) => p.id === value.preset);
              const template = structuredClone(preset.templates[value.client]);
              template.id = resourceID("template");
              template.name =
                value.name || `${catalog[value.client].label} · ${preset.name}`;
              config.templates.unshift(template);
              change();
            },
          ),
        "primary",
      ),
    ),
  );
}

export function accountTemplateSettings(config, catalog, ctx, change) {
  const templateOptions = (client) =>
    config.templates
      .filter((t) => t.client === client && t.mode !== "rules")
      .map((t) => [t.id, `${t.name || t.id} · ${t.mode}`]);
  const rows = Object.entries(catalog).map(([client, profile]) => {
    const policy = config.account_defaults?.[client];
    const template = config.templates.find((t) => t.id === policy?.template);
    return {
      client,
      label: profile.label,
      template: template?.name || template?.id || "内置默认",
      policy,
    };
  });
  return disclosure(
    "账户下载模板与单独覆盖",
    async () => [
      card(
        "账户默认模板",
        help(
          "普通账户分享与直接导出共同使用这些模板。可以明确允许跳过不兼容节点，页面会显示实际数量。",
        ),
        table(
          [
            { key: "label", label: "客户端" },
            { key: "template", label: "默认模板" },
          ],
          rows,
          (item) => [
            button("设置", () =>
              formDialog(
                `账户默认模板 · ${item.label}`,
                [
                  {
                    key: "template",
                    label: "下载模板",
                    type: "select",
                    options: templateOptions(item.client),
                    required: true,
                  },
                  {
                    key: "skip_unsupported",
                    label: "跳过不兼容节点并显示说明",
                    type: "bool",
                    default: true,
                  },
                ],
                item.policy || {
                  template: templateOptions(item.client)[0]?.[0],
                  skip_unsupported: true,
                },
                (value) => {
                  config.account_defaults = {
                    ...config.account_defaults,
                    [item.client]: value,
                  };
                  change();
                },
              ),
            ),
          ],
          { rowKey: (item) => item.client },
        ),
      ),
      disclosure(
        "为指定账户覆盖模板",
        async () => {
          const accounts = await ctx.request("/api/account-options");
          const overrides = config.account_overrides || [];
          return resourceEditor({
            title: "账户模板覆盖",
            items: overrides,
            rowKey: (item) => `${item.account_id}:${item.client}`,
            fields: [
              {
                key: "account_id",
                label: "权益",
                type: "select",
                required: true,
                options: accounts.map((a) => [a.id, a.name]),
              },
              {
                key: "client",
                label: "客户端",
                type: "select",
                options: clientOptions(catalog),
              },
              {
                key: "template",
                label: "下载模板",
                type: "select",
                required: true,
                options: (v) => templateOptions(v.client),
              },
              {
                key: "skip_unsupported",
                label: "跳过不兼容节点并显示说明",
                type: "bool",
              },
            ],
            defaults: {
              account_id: accounts[0]?.id,
              client: "sing-box",
              template: templateOptions("sing-box")[0]?.[0],
              skip_unsupported: true,
            },
            columns: [
              { key: "account_id", label: "权益" },
              { key: "client", label: "客户端" },
              { key: "template", label: "模板" },
            ],
            change: () => {
              config.account_overrides = overrides;
              change();
            },
          });
        },
        ctx.signal,
      ),
    ],
    ctx.signal,
  );
}
