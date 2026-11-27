import { api } from "./api.js";
import { capabilities } from "./capabilities.js";
import { dnsServerFields, normalizeDNSServer } from "./dns-server-fields.js";
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
  const fields = () => ruleFields((effective.inbounds || []).map((v) => v.tag), [...(effective.outbounds || []), ...(effective.endpoints || [])].map((v) => v.tag));
  const changed = ({ local = false } = {}) => {
    c.app.advanced = a;
    ctx.changed();
    if (!local) ctx.render();
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
          formDialog("路由规则", fields(), { action: "route" }, (v) => {
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
          true,
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
          formDialog("有序路由规则", fields(), r, (v) => {
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
    rowKey: (item) => item.tag,
    preserve: true,
    local: true,
    normalize: normalizeDNSServer,
    items: a.dns.servers,
    fields: () => dnsServerFields(supported.dns, a.dns.servers),
    defaults: {
      tag: "dns-new",
      type: "udp",
      server: "1.1.1.1",
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
      help("选择现有出站或 WireGuard 端点，规则按添加顺序写入上方路由列表。"),
      button("添加域名分流", () =>
        formDialog(
          "域名分流",
          [
            {
              key: "domain_suffix",
              label: "域名后缀",
              type: "csv",
              required: true,
            },
            {
              key: "outbound",
              label: "目标出站",
              type: "select",
              options: [
                ...(a.outbounds || effective.outbounds || []),
                ...(a.endpoints || effective.endpoints || []),
              ].map((v) => v.tag),
              required: true,
            },
            {
              key: "strategy",
              label: "IP 策略",
              type: "select",
              options: [
                ["", "默认"],
                ["prefer_ipv4", "优先 IPv4"],
                ["prefer_ipv6", "优先 IPv6"],
              ],
            },
          ],
          {
            domain_suffix: [],
            outbound: a.route.final || "direct",
            strategy: "",
          },
          (v) => {
            if (!v.domain_suffix.length)
              throw new Error("至少填写一个域名后缀");
            const domains = [
              ...new Set(
                v.domain_suffix.map((d) =>
                  d.trim().replace(/^\./, "").toLowerCase(),
                ),
              ),
            ];
            if (!a.route.rules.some((rule) => rule.action === "sniff"))
              a.route.rules.push({ action: "sniff" });
            if (v.strategy)
              a.route.rules.push({
                domain_suffix: domains,
                action: "resolve",
                strategy: v.strategy,
              });
            a.route.rules.push({
              domain_suffix: domains,
              action: "route",
              outbound: v.outbound,
            });
            changed();
          },
        ),
      ),
    ),
  ];
}
