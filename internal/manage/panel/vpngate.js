import { workspace } from "./workspace.js";
import { api, getGatewayStatus } from "./api.js";
import { observeStatus } from "./status-observer.js";
import { boundCertificate, certificateChoices } from "./certificate-fields.js";
import { capabilities } from "./capabilities.js";
import { showFacts, loadRecords } from "./details.js";
import { dialogSession } from "./dialog-session.js";
import { vpnProfileFields, gatewayFields } from "./network-fields.js";
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
export async function vpngate(ctx) {
  const admin = ctx.session.role === "admin";
  const c = admin ? await ctx.config() : null;
  const metadata = await capabilities();
  const active = c?.vpn.active_country || (admin ? "尚未选择" : "运行控制");
  const runtime = el("div", { role: "status" }, help("正在读取网关运行状态…"));
  const gateway = async () => {
    const { refresh } = observeStatus(
      ctx,
      async () => {
        try {
          const current = await getGatewayStatus(
            ctx.signal,
            ctx.request || api,
          );
          if (ctx.signal?.aborted) return;
          const state =
            {
              ready: "已就绪",
              stopped: "已停止",
              starting: "正在启动",
              switching: "正在切换",
              failed: "失败",
              degraded: "异常",
            }[current.state] || current.state;
          runtime.replaceChildren(
            row(
              badge(
                state || "暂无运行状态",
                current.state === "ready" ? "good" : "warn",
              ),
              help("运行国家：" + (current.country || "尚未运行")),
            ),
            ...(current.reason ? [help(current.reason)] : []),
          );
        } catch (error) {
          if (error.name === "AbortError" || ctx.signal?.aborted) return;
          runtime.replaceChildren(
            help("运行状态暂不可读取：" + error.message),
            button("重试", refresh),
          );
        }
      },
      { section: "gateway" },
    );
    await refresh();
    return status;
  };
  const status = card(
    "VPNGate 运行状态",
    runtime,
    row(
      button("查询出口验证", async () => {
        const session = dialogSession(ctx.signal);
        session.replace(help("正在加载出口验证…"));
        let status;
        try {
          status = await getGatewayStatus(session.signal, ctx.request || api);
        } catch (error) {
          session.fail(error);
          return;
        }
        if (!session.active()) return;
        showFacts(
          "出口验证",
          [
            ["状态", status.state],
            ["国家", status.country],
            ["节点", status.node],
            ["出口 IP", status.exit?.ip],
            ["ISP", status.exit?.isp],
            ["ASN", status.exit?.asn],
            ["最近更新", status.updated],
            [
              "候选刷新",
              {
                passed: "刷新成功",
                partial: "部分完成",
                cached: "刷新失败，使用旧缓存",
                failed: "刷新失败",
                running: "正在刷新",
                not_run: "尚未刷新",
              }[status.source_refresh?.outcome] || "尚未刷新",
            ],
            ["候选数量", status.source_refresh?.nodes],
            [
              "最近抓取成功",
              status.source_refresh?.last_success || "尚无成功记录",
            ],
            ["刷新说明", status.source_refresh?.reason],
          ],
          status.reason,
        );
      }),
      button("候选节点", async () =>
        loadRecords(
          "候选节点",
          [
            { key: "host", label: "节点" },
            { key: "ip", label: "IP" },
            { key: "country", label: "国家" },
            { key: "network", label: "传输" },
            { label: "延迟", value: (n) => n.ping + " ms" },
            {
              label: "报告带宽",
              value: (n) => (n.speed / 1000000).toFixed(1) + " Mbps",
            },
          ],
          "/api/gateway?resource=nodes",
          ctx.signal,
        ),
      ),
    ),
    help(
      "国家验证、ISP / hosting 筛选和无普通出口回退由独立网关执行。「家宽候选」不是住宅出口保证。刷新候选任务会等待抓取完成；失败时明确显示使用旧缓存或刷新失败。",
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
  if (!admin)
    return [
      workspace(
        "vpngate",
        {
          access: () =>
            card(
              "VPNGate 接入核心",
              help(
                "sb-vpngate-core 负责用户接入、认证和计量；入口配置由管理员管理。",
              ),
            ),
          gateway,
        },
        ctx,
      ),
    ];
  const controls = card(
    "国家与独立入口",
    help("草稿国家：" + active + "；配置应用成功后生效。"),
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
              options: Object.entries(metadata.countries).map(([id, name]) => [
                id,
                `${name} · ${id}`,
              ]),
            },
          ],
          { country: active },
          async (v, editor) => {
            await ctx.selectCountry(v.country, editor.signal, editor.active);
          },
          false,
          ctx.signal,
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
        button("绑定证书", () =>
          formDialog(
            "VPNGate 证书",
            [
              {
                key: "id",
                label: "证书资产",
                type: "select",
                required: true,
                options: certificateChoices(c.certificates),
              },
            ],
            { id: boundCertificate(p, c.certificates) },
            (v) => {
              const asset = c.certificates.find((a) => a.id === v.id);
              if (!asset) throw new Error("请先导入或申请证书。");
              Object.assign(c.vpn.profiles[p.country], {
                certificate: asset.certificate,
                key: asset.key,
                tls_sni: asset.domain,
                vmess_tls: true,
              });
              ctx.changed();
              ctx.render();
            },
          ),
        ),
        button("编辑端口 / TLS", () =>
          formDialog(
            "VPNGate " + p.country,
            vpnProfileFields(metadata.protocols),
            {
              ...c.vpn.profiles[p.country],
              handshake_port: c.vpn.profiles[p.country].handshake_port || 443,
            },
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
  const policy = card(
    "运行策略",
    button("本地网关端口", () =>
      formDialog(
        "本地网关端口",
        [
          {
            key: "proxy_port",
            label: "SOCKS 监听端口",
            type: "number",
            required: true,
            min: 1,
            max: 65535,
            help: "仅监听 127.0.0.1，服务端会检查端口冲突。",
          },
        ],
        c.vpn,
        (value) => {
          c.vpn = value;
          if (c.vpn.active_country)
            c.gateway.listen = `127.0.0.1:${value.proxy_port}`;
          ctx.changed();
          ctx.render();
        },
      ),
    ),
    button("编辑来源与筛选", () =>
      formDialog(
        "VPNGate 来源与策略",
        gatewayFields(metadata.gateway.limits),
        { ...metadata.gateway.defaults, ...c.gateway },
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
  );
  return [
    workspace(
      "vpngate",
      {
        access: () => [
          help("sb-vpngate-core 负责用户接入、认证和计量。"),
          controls,
        ],
        gateway: async () => [
          await gateway(),
          help("sb-vpngate-gateway 负责 OpenVPN 隧道、出口验证与节点切换。"),
          policy,
        ],
      },
      ctx,
    ),
  ];
}
