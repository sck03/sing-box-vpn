import { api } from "./api.js";
import { clientOptions } from "./clients.js";
import { formDialog } from "./ui.js";
import { showConfiguration } from "./config-preview.js";
import { resourceID } from "./template-settings.js";

export function subscriptionWizard(ctx, config, catalog, presets) {
  const request = ctx.workflowRequest || api;
  const defaultClient = catalog["sing-box"]
    ? "sing-box"
    : Object.keys(catalog)[0];
  formDialog(
    "快速创建订阅 · 来源与客户端",
    [
      { key: "name", label: "订阅名称", required: true },
      {
        key: "kind",
        label: "来源",
        type: "select",
        options: [
          ["remote", "订阅链接"],
          ["inline", "粘贴节点内容"],
        ],
      },
      {
        key: "url",
        label: "订阅 URL",
        type: "url",
        wide: true,
        required: true,
        visible: (v) => v.kind === "remote",
      },
      {
        key: "content",
        label: "节点内容",
        type: "textarea",
        wide: true,
        required: true,
        visible: (v) => v.kind === "inline",
      },
      {
        key: "client",
        label: "客户端",
        type: "select",
        options: clientOptions(catalog),
      },
      {
        key: "preset",
        label: "模板用途",
        type: "select",
        options: (v) =>
          presets
            .filter((p) => p.templates[v.client])
            .map((p) => [p.id, p.name]),
      },
      {
        key: "skip_unsupported",
        label: "跳过不兼容节点并显示说明",
        type: "bool",
        default: true,
      },
    ],
    {
      name: "",
      kind: "remote",
      url: "",
      content: "",
      client: defaultClient,
      preset: "proxy",
      skip_unsupported: true,
    },
    async (value) => {
      const before = JSON.stringify(config),
        candidate = structuredClone(config);
      const s = candidate.subscriptions,
        source = resourceID("source"),
        collection = resourceID("collection"),
        id = resourceID("output");
      for (const key of ["sources", "collections", "templates", "outputs"])
        s[key] ??= [];
      const preset = presets.find((p) => p.id === value.preset);
      const template = structuredClone(preset.templates[value.client]);
      template.id = resourceID("template");
      template.name = `${value.name} · ${preset.name}`;
      s.sources.push({
        id: source,
        name: value.name,
        kind: value.kind,
        format: "auto",
        required: true,
        skip_invalid: true,
        ...(value.kind === "remote"
          ? { url: value.url, interval_seconds: 3600, max_stale_seconds: 86400 }
          : { content: value.content }),
      });
      s.collections.push({
        id: collection,
        name: value.name,
        sources: [source],
        processor: { deduplicate: true, sort: true },
      });
      s.templates.push(template);
      s.outputs.push({
        id,
        name: value.name,
        collection,
        template: template.id,
        skip_unsupported: value.skip_unsupported,
      });
      const preview = await request("/api/config/preview", candidate);
      const current = async () => {
        if (
          (await ctx.config()) !== config ||
          JSON.stringify(config) !== before
        )
          throw new Error(
            "预览期间草稿发生变化，请重新创建，避免覆盖其他修改。",
          );
      };
      await current();
      showConfiguration(preview, [], {
        loadOutput: (id) =>
          request("/api/config/preview-output", { id, config: candidate }),
        applyDraft: async () => {
          await current();
          config.subscriptions = candidate.subscriptions;
          ctx.changed();
          ctx.render();
        },
      });
    },
  );
}
