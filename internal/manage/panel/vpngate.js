import { api } from "./api.js";
import {
  vpnProfileFields,
  gatewayFields,
  gatewayDefaults,
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
  showResult,
  badge,
} from "./ui.js";
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
            await ctx.selectCountry(v.country);
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
          formDialog(
            "VPNGate " + p.country,
            vpnProfileFields,
            c.vpn.profiles[p.country],
            (v) => {
              c.vpn.profiles[p.country] = v;
              ctx.changed();
              ctx.render();
            },
          ),
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
        formDialog(
          "VPNGate 来源与策略",
          gatewayFields,
          { ...gatewayDefaults, ...c.gateway },
          (v) => {
            c.gateway = v;
            ctx.changed();
            ctx.render();
          },
        ),
      ),
      help(
        "支持 sticky / fixed、来源地址、ISP / hosting 策略与 ASN 筛选。更改国家请使用上方国家选择，认证与隔离字段由服务端维护。",
      ),
      el(
        "details",
        { class: "advanced" },
        el("summary", {}, "高级配置"),
        button("编辑完整策略 JSON", () =>
          jsonDialog("VPNGate 高级策略", c.gateway, (v) => {
            c.gateway = v;
            ctx.changed();
            ctx.render();
          }),
        ),
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
