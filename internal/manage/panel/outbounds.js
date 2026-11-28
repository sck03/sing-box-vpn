import { api } from "./api.js";
import { loadDialog } from "./dialog-session.js";
import { outboundOptions } from "./configuration-api.js";
import { capabilities } from "./capabilities.js";
import { workspace } from "./workspace.js";
import { outboundFields, normalizeOutbound } from "./outbound-fields.js";
import { wireguardEditor } from "./wireguard.js";
import {
  el,
  card,
  row,
  button,
  help,
  formDialog,
  jsonDialog,
  resourceEditor,
} from "./ui.js";
export async function outbounds(ctx) {
  const c = await ctx.config(),
    a = structuredClone(c.app.advanced || {});
  let model;
  function networkModel() {
    return (model ??= (ctx.request || api)("/api/server", c.app)
      .then((effective) => {
        a.outbounds ??= effective.outbounds || [];
        a.endpoints ??= effective.endpoints || [];
        return effective;
      })
      .catch((error) => {
        model = null;
        throw error;
      }));
  }
  a.services ??= [];
  const changed = ({ local = false } = {}) => {
    c.app.advanced = a;
    ctx.changed();
    if (local) ctx.invalidateSections?.();
    else ctx.render();
  };
  const selection = () =>
    card(
      "出站选择与检测",
      help(
        "选择修改保存在草稿，应用后重启受影响的普通实例。探测使用已应用配置，每次最多运行一个临时核心。",
      ),
      row(
        button("手动选择", () =>
          formDialog(
            "选择出站组成员",
            [
              {
                key: "tag",
                label: "selector 组",
                type: "select",
                options: a.outbounds
                  .filter((v) => v.type === "selector")
                  .map((v) => v.tag),
              },
              {
                key: "member",
                label: "组成员",
                type: "select",
                options: (v) =>
                  a.outbounds.find((g) => g.tag === v.tag)?.outbounds || [],
                required: true,
              },
            ],
            {},
            (v) => {
              const group = a.outbounds.find((g) => g.tag === v.tag);
              if (!group?.outbounds?.includes(v.member))
                throw new Error("请选择组内成员");
              group.default = v.member;
              changed();
            },
          ),
        ),
        button("按需探测", () =>
          loadDialog(
            ctx.signal,
            "探测已应用出站",
            (signal) => outboundOptions(signal, ctx.workflowRequest || api),
            (applied) => {
              const form = formDialog(
                "探测已应用出站",
                [
                  {
                    key: "id",
                    label: "出站 tag",
                    type: "select",
                    required: true,
                    options: applied.items.map((v) => [
                      v.id,
                      `${v.id} · ${v.type}`,
                    ]),
                  },
                  { key: "value", label: "探测 URL", required: true },
                ],
                {
                  id: applied.items[0]?.id,
                  value: "https://www.gstatic.com/generate_204",
                },
                (v) => ctx.task({ operation: "outbound-probe", ...v }),
              );
              form.form.prepend(
                help(
                  `使用已应用版本 ${applied.revision}${applied.pending || ctx.dirty ? "；草稿变更尚未参与检测。" : "。"}`,
                ),
              );
            },
          ),
        ),
      ),
    );
  const proxy = async () => {
    const [effective, { runtime }] = await Promise.all([
      networkModel(),
      capabilities(),
    ]);
    return [
      selection(),
      resourceEditor({
        title: "出站与出站组",
        rowKey: (item) => item.tag,
        local: true,
        normalize: (value, previous) =>
          normalizeOutbound(value, previous, runtime.outbound_fields),
        preserve: true,
        items: a.outbounds,
        fields: () =>
          outboundFields(
            runtime.outbounds,
            [...a.outbounds, ...a.endpoints].map((v) => v.tag),
            (effective.dns?.servers || []).map((v) => v.tag),
            runtime.outbound_fields,
          ),
        defaults: {
          type: "socks",
          tag: "new-out",
          server: "127.0.0.1",
          server_port: 1080,
          tls: { enabled: true },
        },
        columns: [
          { key: "tag", label: "Tag" },
          { key: "type", label: "类型" },
          { key: "server", label: "地址" },
        ],
        change: changed,
      }),
    ];
  };
  const warp = async () => {
    await networkModel();
    return wireguardEditor(ctx, a.endpoints, changed);
  };
  const services = () =>
    resourceEditor({
      title: "核心服务",
      items: a.services,
      fields: [
        { key: "tag", label: "Tag", required: true },
        { key: "listen", label: "回环监听 IP" },
        { key: "listen_port", label: "监听端口", type: "number" },
      ],
      defaults: {
        type: "resolved",
        tag: "resolved",
        listen: "127.0.0.1",
        listen_port: 5353,
      },
      columns: [
        { key: "tag", label: "Tag" },
        { key: "type", label: "类型" },
        { key: "listen", label: "监听" },
      ],
      change: changed,
    });
  const argo = () =>
    card(
      "Argo 隧道",
      help(
        "固定隧道需要在 Cloudflare 中将入口指向本机 VMess。临时隧道的域名会在启动后写入发布配置。",
      ),
      button("编辑隧道", () =>
        formDialog(
          "Argo 隧道",
          [
            {
              key: "mode",
              label: "模式",
              type: "select",
              options: [
                ["off", "停用"],
                ["temporary", "临时隧道"],
                ["fixed", "固定隧道"],
              ],
            },
            { key: "domain", label: "固定域名" },
            { key: "token", label: "Tunnel token", type: "password" },
            {
              key: "inbound",
              label: "VMess 入站 ID",
              type: "select",
              options: c.app.nodes
                .filter((n) => n.protocol === "vmess")
                .map((n) => n.id),
            },
          ],
          c.tunnel,
          (v) => {
            c.tunnel = v;
            ctx.changed();
            ctx.render();
          },
        ),
      ),
      el(
        "pre",
        {},
        JSON.stringify(
          {
            mode: c.tunnel.mode,
            domain: c.tunnel.domain,
            inbound: c.tunnel.inbound,
          },
          null,
          2,
        ),
      ),
    );
  const logging = () =>
    card(
      "日志与缓存",
      button("高级运行参数", () =>
        jsonDialog(
          "日志 / 缓存",
          {
            log: a.log || { level: "info", timestamp: true },
            experimental: a.experimental || {},
          },
          (v) => {
            a.log = v.log;
            a.experimental = v.experimental;
            changed();
          },
        ),
      ),
    );
  return [
    workspace("outbounds", { proxy, warp, argo, services, logging }, ctx),
  ];
}
