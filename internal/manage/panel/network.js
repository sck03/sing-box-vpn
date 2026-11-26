/** @typedef {import("./contracts.js").ManagedNode} ManagedNode */
import { api } from "./api.js";
import { loadResult } from "./details.js";
import { capabilities } from "./capabilities.js";
import {
  boundCertificate,
  certificateChoices,
} from "./certificate-fields.js";
import {
  el,
  card,
  row,
  button,
  help,
  table,
  formDialog,
  jsonDialog,
  badge,
} from "./ui.js";
const nodeFields = (protocols, protocol) => {
  const fields = [
    { key: "id", label: "入站 ID", required: true },
    {
      key: "protocol",
      label: "协议",
      type: "select",
      options: protocols,
      readonly: true,
    },
    { key: "port", label: "监听端口", type: "number", min: 1, max: 65535 },
    { key: "disabled", label: "停用入站", type: "bool" },
    {
      key: "udp_ports",
      label: protocol === "hysteria2" ? "UDP 跳跃端口" : "额外 UDP 端口",
      type: "csv",
      visible: (v) => ["hysteria2", "tuic"].includes(v.protocol),
      help: "仅转发到当前入站，支持单端口或范围，如 30000:30100；不能与其他入站或服务端口重叠。",
    },
    { key: "tls.server_name", label: "TLS / Reality SNI" },
    {
      key: "tls.enabled",
      label: "启用 TLS",
      type: "bool",
      visible: (v) => v.protocol === "vmess",
    },
    {
      key: "tls.reality_private",
      label: "Reality 私钥",
      type: "password",
      visible: (v) => v.protocol === "vless",
    },
    {
      key: "tls.reality_short_id",
      label: "Reality Short ID",
      visible: (v) => v.protocol === "vless",
    },
    {
      key: "flow",
      label: "VLESS Flow",
      type: "select",
      options: [
        ["", "无"],
        ["xtls-rprx-vision", "Vision"],
      ],
      visible: (v) => v.protocol === "vless",
    },
    {
      key: "transport.early_data",
      label: "WebSocket Early Data（字节）",
      type: "number",
      min: 0,
      max: 65535,
      visible: (v) => v.protocol === "vmess",
    },
    {
      key: "transport.early_data_header",
      label: "Early Data 请求头",
      visible: (v) => v.protocol === "vmess",
    },
    {
      key: "tls.handshake_host",
      label: "Reality 握手域名",
      visible: (v) => v.protocol === "vless",
    },
    {
      key: "tls.handshake_port",
      label: "Reality 握手端口",
      type: "number",
      min: 1,
      max: 65535,
      visible: (v) => v.protocol === "vless",
    },
    {
      key: "transport.path",
      label: "WebSocket 路径",
      visible: (v) => v.protocol === "vmess",
    },
    {
      key: "transport.host",
      label: "WebSocket Host",
      visible: (v) => v.protocol === "vmess",
    },
  ];
  return [
    {
      section: "基本设置",
      matches: (field) => !field.key.includes(".") && field.key !== "flow",
    },
    {
      section: "传输参数",
      matches: (field) => field.key.startsWith("transport."),
    },
    {
      section: "安全与握手",
      matches: (field) => field.key.startsWith("tls.") || field.key === "flow",
    },
  ].flatMap(({ section, matches }) =>
    fields
      .filter(
        (field) =>
          matches(field) && (!field.visible || field.visible({ protocol })),
      )
      .map((field, index) => ({
        ...field,
        ...(index === 0 ? { section } : {}),
      })),
  );
};

export async function inbounds(ctx) {
  const c = await ctx.config();
  const { protocols } = await capabilities();
  const change = () => {
    ctx.changed();
    ctx.render();
  };
  const count = el("span", { class: "muted" });
  const changeNodes = () => {
    ctx.changed();
    ctx.invalidateSections?.();
    list.updateItems(items);
    count.textContent = `${items.length} 个入站`;
  };
  const host = card(
    "发布地址",
    help(
      "连接地址、TLS SNI 与 WebSocket Host 分别配置。实际代理凭据由账户绑定管理。",
    ),
    row(
      el("strong", {}, c.app.address),
      button("编辑发布信息", () =>
        formDialog(
          "发布信息",
          [
            { key: "name", label: "节点名称", required: true },
            { key: "address", label: "IP / 域名", required: true },
            {
              key: "cdn_address",
              label: "VMess CDN IP / 域名（可留空，不含协议、端口或路径）",
            },
          ],
          c.app,
          (v) => {
            c.app = v;
            change();
          },
        ),
      ),
    ),
  );
  const items = (c.app.nodes ??= []);
  const add = button(
    "新增入站",
    () =>
      formDialog(
        "选择协议预设",
        [
          { key: "id", label: "唯一 ID", required: true, advanced: true },
          {
            key: "protocol",
            label: "协议",
            type: "select",
            options: protocols,
          },
        ],
        {
          id: "node-" + crypto.randomUUID().replaceAll("-", ""),
          protocol: "vless",
        },
        async (v) => {
          const node = await api("/api/preset", v);
          items.push(node);
          changeNodes();
        },
      ),
    "primary",
  );
  const list = table(
    [
      { key: "id", label: "入站" },
      { key: "protocol", label: "协议" },
      { key: "port", label: "端口" },
      { label: "额外 UDP 端口", value: (n) => (n.udp_ports || []).join(", ") || "—" },
      {
        label: "状态",
        value: (n) => (n.disabled ? "已停用" : "已启用"),
        render: (n) =>
          badge(n.disabled ? "已停用" : "已启用", n.disabled ? "warn" : "good"),
      },
      { label: "TLS / SNI", value: (n) => n.tls?.server_name },
    ],
    items,
    (n) => [
      button("关联影响", async () =>
        loadResult(
          "入站关联 · " + n.id,
          "/api/inbounds/impact?scope=normal&id=" + encodeURIComponent(n.id),
          ctx.signal,
        ),
      ),
      button("编辑", () =>
        formDialog("编辑入站", nodeFields(protocols, n.protocol), n, (v) => {
          items[items.indexOf(n)] = v;
          changeNodes();
        }),
      ),
      button("证书", () =>
        formDialog(
          "绑定证书",
          [
            {
              key: "id",
              label: "证书资产",
              type: "select",
              options: certificateChoices(c.certificates || []),
              help: "请选择域名匹配的有效证书。自签名证书仅用于测试；普通公网访问请申请或导入受信任证书。",
            },
          ],
          { id: boundCertificate(n.tls || {}, c.certificates || []) },
          (v) => {
            const a = c.certificates.find((a) => a.id === v.id);
            if (!a) throw new Error("请先导入或申请证书");
            if (n.protocol === "vless")
              throw new Error("VLESS 预设使用 Reality，请编辑 Reality 参数");
            n.tls = {
              ...n.tls,
              enabled: true,
              certificate: a.certificate,
              key: a.key,
              server_name: a.domain,
            };
            changeNodes();
          },
        ),
      ),
      button("复制", () =>
        formDialog(
          "复制入站",
          nodeFields(protocols, n.protocol),
          { ...structuredClone(n), id: n.id + "-copy", port: n.port + 1, udp_ports: [] },
          (v) => {
            items.push(v);
            changeNodes();
          },
        ),
      ),
      button(n.disabled ? "启用" : "停用", () => {
        n.disabled = !n.disabled;
        changeNodes();
      }),
      button("高级", () =>
        jsonDialog("入站高级参数", n, (v) => {
          items[items.indexOf(n)] = v;
          changeNodes();
        }),
      ),
      button(
        "删除",
        async () => {
          const impact = await api(
            "/api/inbounds/impact?scope=normal&id=" + encodeURIComponent(n.id),
            undefined,
            { signal: ctx.signal },
          );
          if (
            confirm(
              `删除入站 ${n.id}？\n关联权益：${impact.accounts.join(", ") || "无"}\n可能受影响输出：${impact.outputs.join(", ") || "无"}\n账户下载授权：${impact.download_grants.join(", ") || "无"}\n应用后这些账户不能再通过此入口连接。`,
            )
          ) {
            items.splice(items.indexOf(n), 1);
            changeNodes();
          }
        },
        "danger",
      ),
    ],
  );
  count.textContent = `${items.length} 个入站`;
  return [
    host,
    card(
      "入站管理",
      row(add, count),
      list,
    ),
  ];
}
