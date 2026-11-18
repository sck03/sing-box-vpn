// Navigation, search, titles and service shortcuts share one page catalog.
const lazy = (module, name) => async (context) =>
  (await import(module))[name](context);
export const groups = [
  { id: "overview", label: "运行概览", pages: ["overview"] },
  { id: "nodes", label: "节点管理", pages: ["inbounds", "certificates"] },
  { id: "users", label: "用户与分享", pages: ["accounts", "grants"] },
  {
    id: "settings",
    label: "系统设置",
    pages: ["access", "maintenance", "admins"],
  },
  {
    id: "advanced",
    label: "高级功能",
    advanced: true,
    pages: ["vpngate", "routing", "outbounds", "subscriptions"],
  },
];
export const pages = [
  [
    "overview",
    "运行概览",
    "查看服务健康、配置同步与最近任务。",
    "状态 服务 启动 停止 重启 日志",
    "./overview.js",
    "overview",
    ["admin", "operator", "viewer"],
  ],
  [
    "inbounds",
    "入站与发布",
    "配置用户连接的入口和对外发布地址。",
    "节点 新增节点 配置节点 协议 vless vmess hysteria2 hy2 tuic anytls reality 端口",
    "./network.js",
    "inbounds",
  ],
  [
    "certificates",
    "证书管理",
    "申请、导入与续期节点和面板证书。",
    "申请证书 续期证书 导入证书 TLS PEM ACME SSL https",
    "./network.js",
    "certificates",
  ],
  [
    "accounts",
    "权益与用量",
    "创建用户权益，绑定节点后分享；额度与有效期统一管理。",
    "账户 添加用户 创建用户 新建权益 发订阅 分享 额度 查流量 有效期 到期 绑定 凭据 历史 分组 订阅者",
    "./accounts.js",
    "accounts",
    ["admin", "operator", "viewer"],
  ],
  [
    "grants",
    "分享链接",
    "查看与撤销分享，普通分享继承权益的时间和用量限制。",
    "订阅 下载 授权 分享 取消分享 撤销链接 二维码",
    "./grants.js",
    "grants",
  ],
  [
    "access",
    "公网访问",
    "配置 HTTPS 面板入口与对外订阅地址。",
    "域名 IP 127.0.0.1 反代 nginx caddy 下载监听 subscription",
    "./access.js",
    "access",
  ],
  [
    "maintenance",
    "维护与任务",
    "查看任务结果、运行日志、备份与程序更新。",
    "升级 更新 回滚 恢复 备份 日志 任务 BBR FQ",
    "./maintenance.js",
    "maintenance",
    ["admin", "operator"],
  ],
  [
    "admins",
    "管理员安全",
    "管理控制台访问权限与账户安全。",
    "密码 登录 TOTP 二步验证 角色 会话",
    "./admins.js",
    "admins",
  ],
  [
    "vpngate",
    "VPNGate",
    "选择出口地区，查看隧道网关连接状态。",
    "国家 OpenVPN VPN IP 网关 出口",
    "./vpngate.js",
    "vpngate",
    ["admin", "operator"],
  ],
  [
    "routing",
    "路由与 DNS",
    "按需设置流量去向与域名解析。",
    "分流 规则 DNS 路由 域名解析",
    "./routing.js",
    "routing",
  ],
  [
    "outbounds",
    "出站与隧道",
    "管理外部代理、网络检测与 Argo 隧道。",
    "Argo Cloudflare WARP SOCKS 出站 检测 延迟 连通性 缓存",
    "./outbounds.js",
    "outbounds",
  ],
  [
    "subscriptions",
    "订阅编排",
    "高级：组合外部来源与模板，编译并分发客户端配置。",
    "来源 集合 模板 规则集 输出 编译 分发 webhook 通知",
    "./subscriptions.js",
    "subscriptions",
  ],
].map(
  ([id, title, description, keywords, module, name, roles = ["admin"]]) => ({
    id,
    title,
    description,
    keywords,
    roles,
    load: lazy(module, name),
  }),
);
const byID = new Map(pages.map((page) => [page.id, page]));
export const sections = {
  outbounds: [
    ["proxy", "代理出站", "SOCKS 外部代理 出站组 检测 连通性"],
    ["warp", "WARP / WireGuard", "WARP WireGuard 端点"],
    ["argo", "Argo 隧道", "Argo Cloudflare VMess 隧道"],
    ["services", "核心服务", "resolved 核心服务"],
    ["logging", "日志与缓存", "日志 缓存 运行参数"],
  ],
  maintenance: [
    ["logs", "任务与日志", "日志 任务 结果 进度", ["admin", "operator"]],
    ["backup", "备份与恢复", "备份 恢复"],
    ["upgrade", "程序更新", "升级 更新 回滚"],
    ["network", "网络调优", "BBR FQ 网络调优"],
  ],
  subscriptions: [
    ["sources", "订阅来源", "来源 外部订阅"],
    ["collections", "节点集合", "集合 去重 过滤"],
    ["templates", "模板与规则", "模板 规则集"],
    ["outputs", "输出与授权", "输出 编译 下载授权"],
    ["deliveries", "分发与检测", "分发 检测"],
    ["status", "订阅运行状态", "同步 Webhook 通知 状态"],
  ],
};
export const canView = (id, role) =>
  byID.get(id)?.roles.includes(role) || false;
export function pageFor(id, role) {
  const page = byID.get(canView(id, role) ? id : "overview");
  if (page.id === "accounts" && role !== "admin")
    return {
      ...page,
      description: "查看用户用量、额度、有效期和历史账期。",
      keywords: "账户 用户 查流量 到期 历史",
    };
  return page.id === "maintenance" && role === "operator"
    ? {
        ...page,
        description: "查看操作结果、任务进度与运行日志。",
        keywords: "日志 任务 结果 进度",
      }
    : page;
}
export const groupFor = (id) =>
  groups.find((group) => group.pages.includes(id));
export const groupLabel = (group, role) => {
  const visible = group.pages.filter((id) => canView(id, role));
  return !group.advanced && visible.length === 1
    ? pageFor(visible[0], role).title
    : group.label;
};
export const sectionsFor = (id, role) =>
  canView(id, role)
    ? (sections[id] || []).filter(([, , , roles = ["admin"]]) =>
        roles.includes(role),
      )
    : [];
export function routeFor(value, role) {
  const [id, requested = ""] = value.split("/");
  const page = pageFor(id, role);
  const section =
    page.id === id && sectionsFor(id, role).some(([key]) => key === requested)
      ? requested
      : "";
  return {
    page: page.id,
    section,
    hash: page.id + (section ? "/" + section : ""),
  };
}
export function searchPages(query, role) {
  const terms = query.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean);
  if (!terms.length) return [];
  return pages
    .filter((page) => canView(page.id, role))
    .map((page) => pageFor(page.id, role))
    .flatMap((page) => {
      const parent =
        `${groupLabel(groupFor(page.id), role)} ${page.title}`.toLocaleLowerCase();
      const matches = (text) => terms.every((term) => text.includes(term));
      const children = sectionsFor(page.id, role)
        .filter(([, title, keywords]) => {
          const field = `${title} ${keywords}`.toLocaleLowerCase();
          return (
            terms.some((term) => field.includes(term)) &&
            matches(parent + " " + field)
          );
        })
        .map(([key, title]) => ({
          ...page,
          title,
          route: page.id + "/" + key,
        }));
      return children.length
        ? children
        : matches(
              `${parent} ${page.description} ${page.keywords}`.toLocaleLowerCase(),
            )
          ? [{ ...page, route: page.id }]
          : [];
    });
}
