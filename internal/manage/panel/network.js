import { api } from "./api.js";
import { capabilities } from "./capabilities.js";
import {
  el,
  card,
  row,
  button,
  help,
  table,
  formDialog,
  jsonDialog,
  showResult,
  badge,
  notice,
} from "./ui.js";
const nodeFields = (protocols) => [
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
  { key: "tls.server_name", label: "TLS / Reality SNI" },
  { key: "tls.enabled", label: "启用 TLS", type: "bool" },
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
export async function inbounds(ctx) {
  const c = await ctx.config();
  const { protocols } = await capabilities();
  const change = () => {
    ctx.changed();
    ctx.render();
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
            { key: "cdn_address", label: "VMess CDN 地址（可留空）" },
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
  const items = c.app.nodes || [];
  const add = button(
    "新增入站",
    () =>
      formDialog(
        "选择协议预设",
        [
          { key: "id", label: "唯一 ID", required: true },
          {
            key: "protocol",
            label: "协议",
            type: "select",
            options: protocols,
          },
        ],
        { id: "", protocol: "vless" },
        async (v) => {
          const node = await api("/api/preset", v);
          items.push(node);
          change();
        },
      ),
    "primary",
  );
  const list = table(
    [
      { key: "id", label: "入站" },
      { key: "protocol", label: "协议" },
      { key: "port", label: "端口" },
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
        showResult(
          "入站关联 · " + n.id,
          await api(
            "/api/inbounds/impact?scope=normal&id=" + encodeURIComponent(n.id),
          ),
        ),
      ),
      button("编辑", () =>
        formDialog("编辑入站", nodeFields(protocols), n, (v) => {
          items[items.indexOf(n)] = v;
          change();
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
              options: (c.certificates || []).map((a) => [
                a.id,
                a.id + " · " + a.domain,
              ]),
            },
          ],
          {},
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
            change();
          },
        ),
      ),
      button("复制", () =>
        formDialog(
          "复制入站",
          nodeFields(protocols),
          { ...structuredClone(n), id: n.id + "-copy", port: n.port + 1 },
          (v) => {
            items.push(v);
            change();
          },
        ),
      ),
      button(n.disabled ? "启用" : "停用", () => {
        n.disabled = !n.disabled;
        change();
      }),
      button("高级", () =>
        jsonDialog("入站高级参数", n, (v) => {
          items[items.indexOf(n)] = v;
          change();
        }),
      ),
      button(
        "删除",
        async () => {
          const impact = await api(
            "/api/inbounds/impact?scope=normal&id=" + encodeURIComponent(n.id),
          );
          if (
            confirm(
              `删除入站 ${n.id}？\n关联权益：${impact.accounts.join(", ") || "无"}\n可能受影响输出：${impact.outputs.join(", ") || "无"}\n账户下载授权：${impact.download_grants.join(", ") || "无"}\n应用后这些账户不能再通过此入口连接。`,
            )
          ) {
            items.splice(items.indexOf(n), 1);
            change();
          }
        },
        "danger",
      ),
    ],
  );
  const ports = card(
    "UDP 转发端口",
    help(
      "Hy2 跳跃与 TUIC 多端口共用端口冲突检查和防火墙事务。填写端口或范围，例如 30000:30100。",
    ),
    button("编辑端口范围", () =>
      formDialog(
        "UDP 端口范围",
        [
          { key: "hopping", label: "Hy2 跳跃端口，逗号分隔", type: "csv" },
          { key: "tuic_ports", label: "TUIC 复用端口，逗号分隔", type: "csv" },
        ],
        c.app,
        (v) => {
          c.app = v;
          change();
        },
      ),
    ),
    el(
      "pre",
      {},
      JSON.stringify(
        { hopping: c.app.hopping || [], tuic_ports: c.app.tuic_ports || [] },
        null,
        2,
      ),
    ),
  );
  return [
    host,
    card(
      "入站管理",
      row(add, el("span", { class: "muted" }, `${items.length} 个入站`)),
      list,
    ),
    ports,
  ];
}

export async function certificates(ctx) {
  const c = await ctx.config(),
    status = await (ctx.request || api)("/api/certificates");
  const changed = () => {
    ctx.changed();
    ctx.render();
  };
  return [
    card(
      "证书资产",
      help(
        "导入前验证域名、有效期和私钥匹配。Certbot 统一负责申请与续期，端口 80 需要从公网可达。",
      ),
      row(
        button(
          "导入 PEM",
          () =>
            formDialog(
              "导入证书",
              [
                { key: "id", label: "资产 ID", required: true },
                { key: "domain", label: "证书域名", required: true },
                {
                  key: "certificate",
                  label: "证书 PEM",
                  type: "textarea",
                  wide: true,
                },
                { key: "key", label: "私钥 PEM", type: "textarea", wide: true },
              ],
              {},
              async (v) => {
                const asset = await api("/api/certificates", v);
                if (c.certificates.some((a) => a.id === asset.id))
                  throw new Error("资产 ID 已存在，请使用新的 ID");
                c.certificates.push(asset);
                changed();
              },
            ),
          "primary",
        ),
        button("新增 ACME 资产", () =>
          formDialog(
            "Certbot 资产",
            [
              { key: "id", label: "资产 ID", required: true },
              { key: "domain", label: "域名", required: true },
              {
                key: "email",
                label: "联系邮箱",
                type: "email",
                required: true,
              },
            ],
            {},
            (v) => {
              if (c.certificates.some((a) => a.id === v.id))
                throw new Error("ID 已存在");
              const root = ctx.root;
              c.certificates.push({
                ...v,
                provider: "certbot",
                certificate: root + "/certificates/" + v.id + "/cert.pem",
                key: root + "/certificates/" + v.id + "/key.pem",
              });
              changed();
              notice(
                "先应用资产配置，再点击「申请 / 续期」。证书成功后可在入站页绑定。",
              );
            },
          ),
        ),
      ),
      table(
        [
          { key: "id", label: "资产" },
          { key: "domain", label: "域名" },
          { key: "provider", label: "续期归属" },
          {
            label: "自动续期",
            value: (a) =>
              ({
                automatic: "已登记",
                registration_pending: "待自动登记（需端口 80）",
                manual: "手动更新",
              })[status.find((v) => v.id === a.id)?.renewal_state] || "待应用",
          },
          {
            label: "有效期",
            value: (a) =>
              status.find((v) => v.id === a.id)?.expires_at ||
              status.find((v) => v.id === a.id)?.error ||
              "待申请",
          },
          {
            label: "引用",
            value: (a) =>
              (status.find((v) => v.id === a.id)?.used_by || []).join(", "),
          },
        ],
        c.certificates,
        (a) => [
          ...(a.provider === "certbot"
            ? [
                button("申请 / 续期", () =>
                  ctx.task({ operation: "certificate-renew", id: a.id }),
                ),
              ]
            : []),
          button(
            "移除",
            () => {
              if (confirm("移除证书资产？请先解除入口引用。")) {
                c.certificates.splice(c.certificates.indexOf(a), 1);
                changed();
              }
            },
            "danger",
          ),
        ],
      ),
    ),
    card(
      "使用方式",
      help(
        "证书材料保存为受限文件，配置保存引用。前往「入站」→「证书」选择资产。导入新的资产后再切换引用，可保留旧证书用于回滚。",
      ),
    ),
  ];
}
