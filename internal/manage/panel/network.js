import { api } from "./api.js";
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
  resourceEditor,
  badge,
  notice,
} from "./ui.js";
const protocols = ["vless", "vmess", "hysteria2", "tuic", "anytls"];
const nodeFields = [
  { key: "id", label: "入站 ID", required: true },
  { key: "protocol", label: "协议", type: "select", options: protocols },
  { key: "port", label: "监听端口", type: "number", min: 1, max: 65535 },
  { key: "disabled", label: "停用入站", type: "bool" },
  { key: "tls.server_name", label: "TLS / Reality SNI" },
  { key: "tls.enabled", label: "启用 TLS", type: "bool" },
  { key: "tls.handshake_host", label: "Reality 握手域名" },
  { key: "tls.handshake_port", label: "Reality 握手端口", type: "number" },
  { key: "transport.path", label: "WebSocket 路径" },
  { key: "transport.host", label: "WebSocket Host" },
];
export async function inbounds(ctx) {
  const c = await ctx.config();
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
        formDialog("编辑入站", nodeFields, n, (v) => {
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
          nodeFields,
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

export async function routing(ctx) {
  const c = await ctx.config(),
    a = structuredClone(c.app.advanced || {});
  const effective = await (ctx.request || api)("/api/server", c.app);
  const changed = () => {
    c.app.advanced = a;
    ctx.changed();
    ctx.render();
  };
  a.route ??= effective.route;
  a.route.rules ??= [];
  a.dns ??= effective.dns || { servers: [] };
  a.dns.servers ??= [];
  a.dns.rules ??= [];
  const rules = card(
    "服务端路由 · 按显示顺序执行",
    help(
      "本页控制服务器转发路径。客户端分流模板在「订阅」页面管理。VPNGate 的受保护出口不会被本页规则覆盖。",
    ),
    row(
      button(
        "新增规则",
        () =>
          formDialog(
            "路由规则",
            [
              { key: "domain_suffix", label: "域名后缀", type: "csv" },
              { key: "ip_cidr", label: "IP CIDR", type: "csv" },
              { key: "inbound", label: "入站 ID", type: "csv" },
              {
                key: "action",
                label: "动作",
                type: "select",
                options: ["route", "reject", "sniff", "resolve"],
              },
              { key: "outbound", label: "出站 tag" },
            ],
            { action: "route" },
            (v) => {
              for (const k of ["domain_suffix", "ip_cidr", "inbound"])
                if (!v[k]?.length) delete v[k];
              if (!v.outbound) delete v.outbound;
              a.route.rules.push(v);
              changed();
            },
          ),
        "primary",
      ),
      button("最终路由 / 高级选项", () =>
        jsonDialog("服务端路由", a.route, (v) => {
          a.route = v;
          changed();
        }),
      ),
    ),
    table(
      [
        { label: "顺序", value: (r) => a.route.rules.indexOf(r) + 1 },
        { label: "匹配 / 动作", value: (r) => JSON.stringify(r) },
      ],
      a.route.rules,
      (r) => [
        button("↑", () => {
          const i = a.route.rules.indexOf(r);
          if (i) {
            [a.route.rules[i - 1], a.route.rules[i]] = [
              r,
              a.route.rules[i - 1],
            ];
            changed();
          }
        }),
        button("↓", () => {
          const i = a.route.rules.indexOf(r);
          if (i < a.route.rules.length - 1) {
            [a.route.rules[i + 1], a.route.rules[i]] = [
              r,
              a.route.rules[i + 1],
            ];
            changed();
          }
        }),
        button("编辑", () =>
          jsonDialog("有序路由规则", r, (v) => {
            a.route.rules[a.route.rules.indexOf(r)] = v;
            changed();
          }),
        ),
        button(
          "删除",
          () => {
            a.route.rules.splice(a.route.rules.indexOf(r), 1);
            changed();
          },
          "danger",
        ),
      ],
    ),
  );
  const dns = resourceEditor({
    title: "服务器 DNS",
    normalize: (v) => {
      for (const k of ["server", "server_port", "detour"])
        if (!v[k]) delete v[k];
      if (["local", "resolved"].includes(v.type)) {
        delete v.server;
        delete v.server_port;
      }
      return v;
    },
    items: a.dns.servers,
    fields: [
      { key: "tag", label: "Tag", required: true },
      {
        key: "type",
        label: "传输",
        type: "select",
        options: ["udp", "tcp", "tls", "https", "local", "resolved"],
      },
      { key: "server", label: "服务器" },
      { key: "server_port", label: "端口", type: "number" },
      { key: "detour", label: "出站 tag（可留空）" },
    ],
    defaults: {
      tag: "dns-new",
      type: "udp",
      server: "1.1.1.1",
      server_port: 53,
    },
    columns: [
      { key: "tag", label: "Tag" },
      { key: "type", label: "传输" },
      { key: "server", label: "服务器" },
    ],
    change: changed,
  });
  return [
    rules,
    dns,
    card(
      "DNS 策略与缓存",
      button("编辑 DNS 规则、最终解析器与缓存", () =>
        jsonDialog("DNS 策略", a.dns, (v) => {
          a.dns = v;
          changed();
        }),
      ),
      el("pre", {}, JSON.stringify(a.dns, null, 2)),
    ),
    card(
      "原菜单域名分流",
      help(
        "为本机 SOCKS5、WARP 或 IPv4/IPv6 优先策略设置域名。使用自定义服务端路由时，以高级路由为准。",
      ),
      button("编辑简明分流", () =>
        formDialog(
          "简明分流",
          ["socks", "warp", "direct4", "direct6"].map((key) => ({
            key,
            label: key + " 域名",
            type: "csv",
          })),
          c.app.routing || {},
          (v) => {
            c.app.routing = v;
            delete a.route;
            changed();
          },
        ),
      ),
    ),
  ];
}

export async function outbounds(ctx) {
  const c = await ctx.config(),
    a = structuredClone(c.app.advanced || {});
  const effective = await (ctx.request || api)("/api/server", c.app);
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
              { key: "member", label: "成员 tag", required: true },
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
      normalize: (v) => {
        for (const k of ["server", "server_port", "outbounds", "detour"]) {
          if (!v[k] || (Array.isArray(v[k]) && !v[k].length)) delete v[k];
        }
        if (["direct", "selector", "urltest"].includes(v.type)) {
          delete v.server;
          delete v.server_port;
        }
        if (!["selector", "urltest"].includes(v.type)) delete v.outbounds;
        return v;
      },
      items: a.outbounds,
      fields: [
        { key: "tag", label: "Tag", required: true },
        {
          key: "type",
          label: "类型",
          type: "select",
          options: ["direct", "socks", "selector", "urltest", ...protocols],
        },
        { key: "server", label: "服务器" },
        { key: "server_port", label: "端口", type: "number" },
        { key: "outbounds", label: "组成员 tag，逗号分隔", type: "csv" },
        { key: "detour", label: "链式上游 tag" },
      ],
      defaults: {
        type: "socks",
        tag: "new-out",
        server: "127.0.0.1",
        server_port: 1080,
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
        { key: "peers", label: "Peers JSON", type: "json", wide: true },
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

export async function vpngate(ctx) {
  const c = await ctx.config();
  const active = c.vpn.active_country || "尚未选择";
  const status = card(
    "VPNGate 运行状态",
    row(
      badge(active),
      button("查询出口验证", async () =>
        showResult("出口验证", await api("/api/gateway?resource=status")),
      ),
      button("候选节点", async () =>
        showResult("候选节点", await api("/api/gateway?resource=nodes")),
      ),
    ),
    help(
      "国家验证、ISP / hosting 筛选和无普通出口回退由独立网关执行。「家宽候选」不是住宅出口保证。",
    ),
    row(
      ...[
        ["start", "启动 / 恢复"],
        ["stop", "暂停"],
        ["swap", "换 IP"],
        ["refresh", "刷新候选"],
      ].map(([op, label]) =>
        button(label, () => ctx.task({ operation: "gateway", config: { op } })),
      ),
    ),
  );
  const controls = card(
    "国家与独立入口",
    button(
      "选择 / 新增国家",
      () =>
        formDialog(
          "选择国家",
          [
            {
              key: "country",
              label: "国家代码",
              type: "select",
              options: [
                "JP",
                "KR",
                "US",
                "TW",
                "HK",
                "SG",
                "TH",
                "VN",
                "DE",
                "GB",
                "CA",
              ],
            },
          ],
          { country: active },
          async (v) => {
            if (
              ctx.dirty &&
              !confirm(
                "选择国家会以服务器保存的配置为基础，请先保存其它草稿。继续？",
              )
            )
              return;
            ctx.replaceConfig(await api("/api/country", v));
            ctx.changed();
            ctx.render();
          },
        ),
      "primary",
    ),
    table(
      [
        { key: "country", label: "国家" },
        { label: "五协议端口", value: (p) => JSON.stringify(p.ports) },
      ],
      Object.entries(c.vpn.profiles).map(([country, p]) => ({ ...p, country })),
      (p) => [
        button("编辑端口 / TLS", () =>
          jsonDialog("VPNGate " + p.country, c.vpn.profiles[p.country], (v) => {
            c.vpn.profiles[p.country] = v;
            ctx.changed();
            ctx.render();
          }),
        ),
      ],
    ),
  );
  return [
    status,
    controls,
    card(
      "运行策略",
      button("编辑来源与筛选", () =>
        jsonDialog("VPNGate 来源与策略", c.gateway, (v) => {
          c.gateway = v;
          ctx.changed();
          ctx.render();
        }),
      ),
      help(
        "支持 sticky / fixed、来源地址、ISP / hosting 策略与 ASN 筛选。更改国家请使用上方国家选择，认证与隔离字段由服务端维护。",
      ),
      el(
        "pre",
        {},
        JSON.stringify(
          {
            country: c.gateway.country,
            mode: c.gateway.mode,
            require_isp: c.gateway.require_isp,
            exclude_hosting: c.gateway.exclude_hosting,
            sources: c.gateway.sources,
          },
          null,
          2,
        ),
      ),
    ),
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
