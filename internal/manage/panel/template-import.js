import { previewTemplateImport } from "./api.js";
import { clientOptions } from "./clients.js";
import { resourceID } from "./template-settings.js";
import { dialogSession } from "./dialog-session.js";
import { formDialog, el, help, row, button } from "./ui.js";

export function importTemplate(ctx, config, catalog, changed) {
  const edit = (
    draft = {
      id: resourceID("template"),
      client: "mihomo",
      format: "rule-list",
      default_target: "PROXY",
    },
  ) => {
    const editor = formDialog(
      "导入模板 / 路由规则",
      [
        { key: "name", label: "模板名称", required: true },
        {
          key: "client",
          label: "目标客户端",
          type: "select",
          options: clientOptions(catalog).filter(([id]) =>
            catalog[id].modes.includes("config"),
          ),
        },
        {
          key: "format",
          label: "来源格式",
          type: "select",
          options: [
            ["project-json", "本项目声明式 JSON 模板"],
            ["rule-list", "规则列表（例如 ACL4SSR .list）"],
            ["mihomo-yaml", "Mihomo YAML：提取简单规则"],
            ["sing-box-json", "sing-box JSON：提取简单规则"],
          ],
        },
        {
          key: "content",
          label: "模板内容",
          type: "textarea",
          wide: true,
          required: true,
          help: "不会执行脚本。外部完整配置仅提取简单路由规则，不导入节点凭据、策略组或设备设置；不支持的条件会明确报错。",
        },
        {
          key: "default_target",
          label: "未指定目标的规则",
          type: "select",
          options: [
            ["PROXY", "代理"],
            ["DIRECT", "直连"],
            ["REJECT", "拦截"],
          ],
          visible: (value) => value.format === "rule-list",
        },
        {
          key: "source_url",
          label: "来源地址（记录用途，不会自动下载）",
          type: "url",
          advanced: true,
        },
        { key: "revision", label: "来源版本 / 提交号", advanced: true },
      ],
      draft,
      async (value) => {
        const result = await previewTemplateImport(value, ctx.signal);
        if (
          !editor.dialog.open ||
          !editor.dialog.contains(editor.form) ||
          ctx.signal?.aborted
        )
          return;
        const session = dialogSession(ctx.signal);
        session.replace(
          el("h2", {}, "确认导入模板"),
          help(
            "目标：" +
              catalog[result.template.client].label +
              " · 规则数：" +
              (result.template.rules || []).length,
          ),
          ...result.warnings.map((warning) => help(warning)),
          help(
            "导入后仍需关联输出、预览并校验。来源信息与内容摘要会保存在模板中。",
          ),
          row(
            button("返回编辑", () => edit(value)),
            button(
              "加入草稿",
              () => {
                config.templates.unshift(result.template);
                session.close();
                changed();
              },
              "primary",
            ),
          ),
        );
      },
    );
  };
  edit();
}
