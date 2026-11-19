import { api } from "./api.js";
import { capabilities } from "./capabilities.js";
import {
  ruleFields,
  cleanRule,
  routeFields,
  routeDefaults,
  dnsFields,
} from "./network-fields.js";
import {
  el,
  card,
  row,
  button,
  help,
  table,
  formDialog,
  jsonDialog,
  resourceEditor,
} from "./ui.js";
export async function routing(ctx) {
  const c = await ctx.config(),
    a = structuredClone(c.app.advanced || {});
  const effective = await (ctx.request || api)("/api/server", c.app);
  const supported = (await capabilities()).runtime;
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
          formDialog("路由规则", ruleFields, { action: "route" }, (v) => {
            a.route.rules.push(cleanRule(v));
            changed();
          }),
        "primary",
      ),
      button("默认路由与网卡", () =>
        formDialog(
          "服务端路由",
          routeFields,
          { ...routeDefaults, ...a.route },
          (v) => {
            a.route = v;
            changed();
          },
        ),
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
          formDialog("有序路由规则", ruleFields, r, (v) => {
            a.route.rules[a.route.rules.indexOf(r)] = cleanRule(v);
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
      for (const k of [
        "server",
        "server_port",
        "detour",
        "domain_resolver",
        "path",
      ])
        if (!v[k]) delete v[k];
      if (["local", "resolved"].includes(v.type)) {
        delete v.server;
        delete v.server_port;
        delete v.domain_resolver;
      }
      if (v.type !== "https") delete v.path;
      return v;
    },
    items: a.dns.servers,
    fields: [
      { key: "tag", label: "Tag", required: true },
      {
        key: "type",
        label: "传输",
        type: "select",
        options: supported.dns,
      },
      { key: "server", label: "服务器" },
      { key: "server_port", label: "端口", type: "number" },
      {
        key: "path",
        label: "DoH 路径",
        default: "/dns-query",
        visible: (v) => v.type === "https",
      },
      {
        key: "domain_resolver",
        label: "服务器域名解析器",
        type: "select",
        options: (v) => [
          ["", "默认解析器"],
          ...a.dns.servers.filter((s) => s.tag !== v.tag).map((s) => s.tag),
        ],
        visible: (v) => !["local", "resolved"].includes(v.type),
      },
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
      button("解析器与缓存设置", () =>
        formDialog("DNS 策略", dnsFields, a.dns, (v) => {
          a.dns = v;
          changed();
        }),
      ),
      el(
        "details",
        { class: "advanced" },
        el("summary", {}, "高级规则与完整配置"),
        button("编辑 DNS JSON", () =>
          jsonDialog("DNS 高级配置", a.dns, (v) => {
            a.dns = v;
            changed();
          }),
        ),
        button("编辑路由 JSON", () =>
          jsonDialog("路由高级配置", a.route, (v) => {
            a.route = v;
            changed();
          }),
        ),
      ),
    ),
    card(
      "快捷域名分流",
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
