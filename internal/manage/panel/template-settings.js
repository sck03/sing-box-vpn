import { clientOptions } from "./clients.js";
import { disclosure } from "./sections.js";
import {
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
            { client: "uri", preset: "nodes", name: "" },
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

export function accountTemplateSettings(config, ctx, change) {
  return disclosure(
    "账户下载模板与单独覆盖",
    async () => {
      const profiles = await ctx.request("/api/output-profiles");
      const templateOptions = (id) => {
        const profile = profiles.find((item) => item.id === id);
        return config.templates
          .filter(
            (t) => t.client === profile?.client && t.mode === profile?.mode,
          )
          .map((t) => [t.id, t.name || t.id]);
      };
      const rows = profiles.map((profile) => {
        const policy = config.account_defaults?.[profile.id];
        const template = config.templates.find(
          (t) => t.id === policy?.template,
        );
        return {
          ...profile,
          template: template?.name || template?.id || "内置默认",
          policy,
        };
      });
      const editDefault = (item) =>
        formDialog(
          "默认模板 · " + item.label,
          [
            {
              key: "template",
              label: "下载模板",
              type: "select",
              options: [["", "内置默认"], ...templateOptions(item.id)],
            },
            {
              key: "skip_unsupported",
              label: "跳过不兼容节点并显示说明",
              type: "bool",
              default: true,
              visible: (v) => !!v.template,
            },
          ],
          item.policy || { template: "", skip_unsupported: true },
          (value) => {
            config.account_defaults = { ...config.account_defaults };
            if (value.template) config.account_defaults[item.id] = value;
            else delete config.account_defaults[item.id];
            change();
          },
        );
      return [
        card(
          "账户默认模板",
          help(
            "每种输出用途独立设置。仅节点模板不能改成完整配置，已有分享链接的用途保持明确。",
          ),
          table(
            [
              { key: "label", label: "订阅格式与用途" },
              { key: "template", label: "默认模板" },
            ],
            rows,
            (item) => [button("设置", () => editDefault(item))],
            { rowKey: (item) => item.id },
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
              rowKey: (item) => item.account_id + ":" + item.profile,
              fields: [
                {
                  key: "account_id",
                  label: "权益",
                  type: "select",
                  required: true,
                  options: accounts.map((a) => [a.id, a.name]),
                },
                {
                  key: "profile",
                  label: "订阅格式与用途",
                  type: "select",
                  options: profiles.map((p) => [p.id, p.label]),
                },
                {
                  key: "template",
                  label: "下载模板",
                  type: "select",
                  required: true,
                  options: (v) => templateOptions(v.profile),
                },
                {
                  key: "skip_unsupported",
                  label: "跳过不兼容节点并显示说明",
                  type: "bool",
                },
              ],
              defaults: {
                account_id: accounts[0]?.id,
                profile: "uri",
                template: templateOptions("uri")[0]?.[0],
                skip_unsupported: true,
              },
              columns: [
                { key: "account_id", label: "权益" },
                {
                  label: "订阅用途",
                  value: (v) =>
                    profiles.find((p) => p.id === v.profile)?.label ||
                    v.profile,
                },
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
      ];
    },
    ctx.signal,
  );
}
