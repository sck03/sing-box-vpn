import { api } from "./api.js";
import { capabilities } from "./capabilities.js";
import { outboundFields, normalizeOutbound } from "./outbound-fields.js";
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
  const effective = await (ctx.request || api)("/api/server", c.app);
  const { protocols } = await capabilities();
  a.outbounds ??= effective.outbounds || [];
  a.endpoints ??= effective.endpoints || [];
  a.services ??= [];
  const changed = () => {
    c.app.advanced = a;
    ctx.changed();
    ctx.render();
  };
  return [
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
          formDialog(
            "探测已应用出站",
            [
              {
                key: "id",
                label: "出站 tag",
                type: "select",
                options: [...a.outbounds, ...a.endpoints].map((v) => v.tag),
              },
              { key: "value", label: "探测 URL", required: true },
            ],
            {
              id: a.outbounds[0]?.tag,
              value: "https://www.gstatic.com/generate_204",
            },
            (v) => ctx.task({ operation: "outbound-probe", ...v }),
          ),
        ),
      ),
    ),
    resourceEditor({
      title: "出站与出站组",
      normalize: normalizeOutbound,
      items: a.outbounds,
      fields: outboundFields(
        protocols,
        [...a.outbounds, ...a.endpoints].map((v) => v.tag),
        (effective.dns?.servers || []).map((v) => v.tag),
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
    resourceEditor({
      title: "WARP / WireGuard 端点",
      items: a.endpoints,
      fields: [
        { key: "tag", label: "Tag", required: true },
        { key: "private_key", label: "私钥", type: "password" },
        { key: "address", label: "本地地址 CIDR", type: "csv" },
      ],
      defaults: {
        type: "wireguard",
        tag: "warp-out",
        address: ["172.16.0.2/32"],
        private_key: "",
        peers: [],
      },
      columns: [
        { key: "tag", label: "Tag" },
        { key: "type", label: "类型" },
      ],
      change: changed,
      extraActions: (endpoint) => [
        button("管理对端", () => {
          const dialog = document.querySelector("#dialog");
          const draft = structuredClone(endpoint.peers || []);
          const render = () => {
            dialog.replaceChildren(
              el("h2", {}, "WireGuard 对端 · " + endpoint.tag),
              resourceEditor({
                title: "对端",
                items: draft,
                fields: [
                  { key: "address", label: "服务器地址", required: true },
                  {
                    key: "port",
                    label: "端口",
                    type: "number",
                    min: 1,
                    max: 65535,
                  },
                  { key: "public_key", label: "公钥", required: true },
                  {
                    key: "pre_shared_key",
                    label: "预共享密钥（可留空）",
                    type: "password",
                  },
                  {
                    key: "allowed_ips",
                    label: "允许的 IP CIDR",
                    type: "csv",
                    required: true,
                  },
                  {
                    key: "persistent_keepalive_interval",
                    label: "保活间隔（秒，0 停用）",
                    type: "number",
                    min: 0,
                    max: 65535,
                  },
                ],
                defaults: {
                  address: "",
                  port: 51820,
                  public_key: "",
                  allowed_ips: ["0.0.0.0/0", "::/0"],
                },
                columns: [
                  { key: "address", label: "服务器" },
                  { key: "port", label: "端口" },
                  {
                    label: "允许网段",
                    value: (v) => v.allowed_ips?.join(", "),
                  },
                ],
                normalize: (v) => {
                  if (!v.pre_shared_key) delete v.pre_shared_key;
                  return v;
                },
                change: () => {
                  endpoint.peers = structuredClone(draft);
                  changed();
                  render();
                },
              }),
              button("完成", () => dialog.close()),
            );
            dialog.showModal();
          };
          render();
        }),
      ],
    }),
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
    }),
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
            changed();
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
    ),
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
    ),
  ];
}
