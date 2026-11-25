# 订阅、客户端模板与分享

[使用指南](usage.md) · [架构](architecture.md) · [兼容性](compatibility.md)

当前订阅配置为 **schema=6**，CompilerVersion=7、ParserVersion=3。只接受当前配置，不迁移旧数据或恢复旧管理接口。平台配置 schema=7、节点文档 schema=5、模板 version=1、数据库 schema=17 和备份容器格式 8 分别维护，不能一起机械递增。

## 普通账户分享

先在「系统设置 → 公网访问」配置 HTTPS 地址，再创建权益、绑定入站并点击「分享」。普通账户直接使用内置输出，不需要创建来源、集合或模板。默认优先提供仅节点订阅，完整配置由用户明确选择。

### 选择格式与用途

| 输出标识                                              | 内容与使用边界                                                                             |
| ----------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| uri / base64                                          | 同一节点 URI 列表的明文或 Base64 编码；客户端须支持节点链接及对应协议。Base64 不是加密。   |
| mihomo-provider / openclash-provider / stash-provider | 只有 proxies，需要在客户端配置中引用 Provider。                                            |
| sing-box-nodes                                        | 只有 outbounds；需要支持节点导入的客户端或与基础配置合并，不能当作独立代理程序的完整配置。 |
| 客户端 ID 加 -config                                  | 完整配置，包含模板中的策略、DNS、规则等设备设置。                                          |
| 其他客户端 ID 加 -nodes                               | 客户端专属节点语法；具体可用标识以输出目录为准。                                           |

仅节点仍保留认证、TLS、SNI、Reality 和传输等必要连接信息。通用表示节点链接格式可被多个客户端识别，不表示任何协议都能被任何客户端运行。Provider 与完整配置拥有不同地址，可以同时授权；不根据 User-Agent 静默切换语义。

### 现行接口

- GET /api/output-profiles：稳定输出目录，包含 id、client、mode、label、description。
- GET /api/accounts/outputs?id=...&scope=...：按账户及实例分析可用节点，不启动核心、不渲染全部格式。
- POST /api/accounts/export：请求 {id, scope, profile, tuic_port?}，返回 content、mime、extension、output 和 validation。
- POST /api/grants：账户授权通过 profiles: ["uri", "mihomo-provider", "mihomo-config"] 精确选择用途；高级输出仍通过 outputs 选择。
- 用户页 /user#TOKEN，用量 /u/TOKEN/usage，下载 /u/TOKEN/PROFILE。旧 clients 字段及账户客户端列表接口不再接受。

用户用量返回 profiles 对象数组，包含 profile、client、mode、description、可用数量、跳过原因与验证状态。反向代理前缀通过相对 URL 保留。令牌仅保存哈希，完整链接只在创建时展示；为同一账户增加分享不会增加流量额度。

列表的 pending 表示兼容性已分析、实际配置待下载时检查；unavailable 只表示 sing-box 配置检查不可用。passed 结合 scope 区分 sing-box 核心检查与内置节点格式检查。parser 不代表目标应用已接受完整配置；Mihomo/OpenClash 不依赖服务器安装客户端核心。

下载时重新检查授权、权益、绑定及模板，并且只生成与校验所选格式。HEAD 不计数，GET 在生成和校验后原子计数；下载次数 null 不限、0 禁止。账户状态与用量不进入编译缓存。创建分享或下载不会激活首次使用权益。

## 账户下载模板

「订阅编排 → 模板与规则 → 账户下载模板与单独覆盖」按输出用途管理模板：

- account_defaults 是输出 profile ID 到 {template, skip_unsupported} 的映射。
- account_overrides 是 {account_id, profile, template, skip_unsupported} 数组，优先于全局默认。
- 模板的 client 和 mode 必须同时匹配输出目录；节点链接不能通过换模板变成完整配置。
- 设置中选择「内置默认」会删除该覆盖；账户单独覆盖可在列表删除。
- 无覆盖时直接使用内置默认，不复制整套默认项进用户配置。默认允许跳过不兼容节点，并明确显示数量和原因。
- 远程模板只使用已发布快照，下载不访问远端。尚未发布的远程模板只影响引用它的输出，内置节点订阅继续可用。

编译缓存每服务最多 128 条、估算 16 MiB、有效期 5 分钟，并合并同输入的并发编译。键含账户及版本、实例范围、实际节点摘要、模板、跳过策略及编译/解析版本。权限、撤销、额度和文件相关的核心验证仍按请求检查。

订阅转换与客户端核心安装解耦。Mihomo/OpenClash 使用内置节点解析器检查导出格式，模板结构、引用和能力在编译阶段检查；服务器不寻找或启动 Mihomo。MIHOMO_BINARY 仅供开发和 CI 客户端验收使用，测试版本与摘要位于 scripts/client-test-versions.json。

## 快速创建与高级编排

「快速创建订阅」提供来源、客户端和模板用途选择。预览成功后点击「加入草稿」，再统一保存应用。预览期间其他草稿修改会使本次加入操作失效，避免覆盖用户的新修改。

高级工作区保留来源 → 集合 → 模板 → 输出 → 分发流程：

1. 来源支持 account、remote、inline。账户来源填写账户 ID 与实例范围；远端填写 URL；内联填写节点内容。
2. 集合选择来源，设置协议/标签/名称正则筛选、重命名、去重和排序。
3. 模板选择客户端与模式，配置策略组、规则和 DNS。
4. 输出引用集合与模板，明确选择是否跳过不支持节点。
5. 预览节点数量、增删变化、实际验证结果与脱敏配置，保存应用后创建输出授权。

来源、集合、输出和分发列表的编辑局部刷新；其他已加载标签按依赖失效，下次打开读取新选项。页面保留搜索、排序及分页状态。状态页展示已配置的同步资源和投递目标；任务变化、切回标签和浏览器重新可见时刷新，合并并发请求，隐藏页不发新请求。账户和分享列表会取消被搜索、排序或筛选替代的旧请求。

## 输入与字段保真

来源格式：auto、uri、base64、sing-box、mihomo、nodes、surge、loon、quantumult-x。auto 识别正文；文本配置只提取代理段或节点列表，不执行脚本。

当前节点协议为 VLESS、VMess、Hysteria2、TUIC、AnyTLS、Trojan、Shadowsocks。客户端支持范围按协议和传输分别登记，不能只凭协议名称推断全部字段可用。

- Hysteria2 支持默认 443、完整认证字符串、mport 和地址内端口列表；不会截断含冒号的认证信息。
- 节点字段 `hop_interval_seconds` 为 5–86400 的整数，必须同时提供 Hysteria2 跳跃端口；支持 sing-box、Mihomo/OpenClash、Egern 输出和 sing-box/Mihomo 输入。通用 URI 及其他未适配目标明确报告不能保留间隔，不静默丢弃。
- Shadowsocks 支持常见 SIP002 URI，以及受控 SIP003 obfs / v2ray-plugin 参数。插件只表示连接参数，不允许指定外部可执行程序。
- 支持 TCP、WS、gRPC、HTTPUpgrade；VLESS XHTTP 仅输出给能表达该传输的客户端。
- WS early-data、gRPC 模式/authority、Hysteria2 salamander、TLS/Reality、端口跳跃和证书固定均经过能力检查。
- XHTTP 支持受控头、padding、POST 大小/间隔等选项；未建模的 downloadSettings、xmux 等额外字段明确拒绝。
- 未识别或不能保留的连接字段拒绝转换；`skip_invalid` 控制逐节点输入跳过，`skip_unsupported` 控制目标筛选，两者独立。

来源可设置 `user_agent` 和 `headers`。头数量最多 16、总大小最多 8 KiB，禁止 Host、连接控制及长度等头；携带自定义头时仅允许同源跳转，避免认证信息转发给其他站点。状态接口不返回 URL、请求头、正文或凭据。

重名使用稳定后缀，计数器按原名推进，避免大批同名节点反复从 2 开始扫描。去重依据连接参数；节点名称、来源标签不代替连接身份。

## 模板预设与策略

`GET /api/subscription-presets` 提供全局代理、国内直连、移动端 TUN、自动选择、地区选择、路由器 Provider 和 VPNGate 专用预设。返回每个用途对应的客户端模板，界面直接消费同一目录。

自动选择默认每 900 秒检测，容差 80 ms。地区预设按名称匹配香港、日本、新加坡和美国，空地区省略。预设创建后可编辑，不依赖上游脚本运行时。

模板及策略组分离稳定 `id` 与显示 `name`。名称可用中文；与节点显示名冲突时生成稳定后缀并报告。规则和组引用稳定 ID，由渲染器统一转换为目标名称。

策略组支持 select、urltest，Mihomo 系支持 fallback；`members` 只引用其他组，并检查循环。显式 normal/vpngate 范围同时约束嵌套成员，VPNGate 组不能间接引用普通出口。`optional=true` 可省略空组，但最终策略、规则或其他组不能引用被省略的组。

`final` 指定默认策略组；省略时按节点范围使用 PROXY 或 VPNGate。`private_network=true` 添加局域网路由，VPNGate-only 配置拒绝此项、国内直连和非受管 native 覆盖。

本地规则、引用规则集、自定义 Provider、局域网/国内规则及最终策略按顺序执行。sing-box 使用 SRS，Mihomo/OpenClash 使用 MRS。远端规则的可达性和内容不由核心语法检查担保，客户端仍需在目标网络获取。

DNS 分为 local、remote、bootstrap；bootstrap 使用 IP，其余支持 IP 或 udp/tcp/tls/https URL。sing-box 每角色一个解析器；Mihomo 系支持 Fake-IP、排除域名和 IPv6。`fake_ip` 省略或 null 表示继承（Mihomo 系默认启用），true 显式启用，false 显式关闭。表单提供对应三态；仅修改代理 DNS 不影响 Fake-IP。`fake_ip_filter` 省略继承默认排除域名，显式 `[]` 清空默认列表；关闭 Fake-IP 时不能设置非空排除列表。sing-box 使用自身地址处理，不接受 Fake-IP 启用或排除列表。

`native` 补充未受管的根字段，可覆盖日志、混合端口等基础默认值。受管理字段及禁止字段在保存/预览时报出 `native.<字段>` 错误，不再静默覆盖。规则同时由 `/api/clients.native_fields` 提供给前端。模板不是任意脚本入口。

| 客户端                 | 可覆盖默认值                           | 编译器管理                                             | 禁止额外注入                                            |
| ---------------------- | -------------------------------------- | ------------------------------------------------------ | ------------------------------------------------------- |
| sing-box               | log                                    | inbounds、outbounds、route、dns                        | endpoints、services                                     |
| Mihomo/OpenClash/Stash | mixed-port、allow-lan、mode、log-level | proxies、proxy-groups、rules、rule-providers、dns、tun | proxy-providers                                         |
| Egern                  | 其他非受管根字段直接保留               | proxies、policy_groups、rules                          | proxy_providers、policy_group_providers、rule_providers |

表中以外的原生字段由目标客户端校验；实验适配器未通过实体应用验收，不能把“已编码”理解为客户端已接受。

示例：[Mihomo](examples/template-mihomo.json)、[sing-box](examples/template-sing-box.json)。客户端模板与服务端「路由与 DNS」独立。

## 接口与预览

任务统一通过 `POST /api/tasks` 提交：外层 `operation` 为 `subscription`，内层 `subscription` 必须符合下列形态。

| 操作 | 内层请求 | 作用范围 |
| --- | --- | --- |
| 刷新 | `{ "operation": "refresh" }` | 全局刷新；拒绝 ID 和 probe_url |
| 检测 | `{ "operation": "check", "id": "output-id", "probe_url": "https://example.com" }` | 必须指定已应用输出和有效 HTTP(S) probe_url |
| 投递 | `{ "operation": "deliver", "id": "delivery-id" }` | 必须指定一个分发目的地；省略/无效 ID 或携带 probe_url 在入队前拒绝 |

自动调度策略由服务端维护，不接受客户端提交 `automatic`。无效命令在入队前返回 400，不会悄悄扩大执行范围。

| 接口                                   | 用途                                          |
| -------------------------------------- | --------------------------------------------- |
| GET /api/output-profiles               | 账户输出身份：客户端、用途、名称与使用说明    |
| GET /api/clients                       | 客户端、模式、扩展名及协议/传输/加密/插件范围 |
| GET /api/subscription-capabilities     | 当前 schema、输入格式和协议                   |
| GET /api/subscription-presets          | 可编辑的用途预设                              |
| GET /api/accounts/outputs?id=…&scope=… | 当前账户与实例的实际可下载范围                |
| POST /api/accounts/export              | 内容、MIME、扩展名、节点数量、模板与验证结果  |
| POST /api/config/preview               | 完整草稿校验、节点数量和增删变化              |
| POST /api/config/preview-output        | `{id, config}`，返回单个输出的脱敏预览        |
| GET /api/subscriptions/actions         | 已应用输出/分发 ID，草稿不能用于运行操作      |
| GET /api/outbound-options              | 已应用服务端出站选择器，供实际连接探测        |
| GET /api/subscriptions/status          | 输出验证及来源/模板/分发运行状态              |

写入和敏感管理接口继续要求管理员会话及 CSRF。输出预览最多显示 64 KiB，Base64 列表可解码展示，凭据和私有 URL 被隐藏；预览不能直接代替真实下载文件。预览弹窗显示加载、可发布输出、核心校验和待关注数量。关闭、离页、切换标签、登出或打开其他编辑弹窗会取消其在途请求，迟到结果不会替换新窗口。

验证对象包含 `state`、`engine`、`scope`、`cached`、`application_verified`，前端不解析英文消息推断状态。核心配置检查、解析往返和客户端应用导入分开表达。

## 刷新、检测和分发

刷新在发布锁外抓取与编译，捕获输入及提交前都检查数据库已确认配置、发布版本、账户凭据和核心身份。配置正在激活且未确认时拒绝后台覆盖。新的模板/规则未通过输出验证时保留旧资源，并继续更新无关输出及已失效账户状态。账户本地模板所引用的远程规则复用已发布有效模板，避免不同下载入口产生不同规则。记录与分发元数据在提交时合并。

产物缓存按订阅 schema、CompilerVersion、所选节点与有效模板摘要失效；正文摘要防止复用损坏产物。来源摘要包含 ParserVersion，解析语义升级时清空旧节点和 ETag，重新获取正文。sing-box 核心校验缓存包括二进制身份；其他格式的纯解析检查与编译产物一起缓存。调度只读取 schedule.json。

来源有刷新间隔、最大陈旧时间和失败退避；304 合并有效元信息。必需来源不可用时撤下相关输出，可选来源过期时移除。每分钟核对账户状态，恢复额度、账户或新周期后可重新编译。

立即检测只编译目标输出及其来源，不重复校验同一个产物。最多 64 节点、2 个 sing-box 探测进程并行，每节点 12 秒，总预算 8 分 24 秒。检测能力由节点连接参数决定，与导出给哪个客户端无关。XHTTP 等超出 sing-box 能力的节点标记 unsupported 并计入 skipped，全部无法检测时 outcome=not_run；部分完成为 partial。不会因此安装客户端核心，也不把无法检测当成节点故障。节点连接通过不等于目标应用已导入。

分发支持 HTTP PUT、WebDAV、secret Gist、私有 GitLab、Telegram。自动分发需启用；未变化产物不重复发送，失败退避。Webhook 记录最后一次发送结果，不表示目标服务实时健康。

授权统一通过 /api/grants 创建、查询和撤销。输出分享为 `/s/<token>/<output-id>`，账户分享与输出分享互斥。自定义输出在 HEAD/304 前重新检查授权、账户和缓存期限。

## 目标平台与产物完整性

输入格式新增显式 Stash；输出仍由客户端与用途共同标识。`/api/accounts/outputs` 和公开用量的 profiles 同时提供 platforms、import_prefix。平台筛选不改变下载授权，服务端仍在每次下载时重新检查权益与兼容性。Stash 完整配置支持官方 `stash://install-config?url=`，Provider 和节点片段只提供复制/下载。其他客户端没有经过核实的导入协议时不生成猜测链接。

Stash 使用独立完整模板：DNS 路由使用 follow-rule，不输出 Mihomo 的 respect-rules、enhanced-mode 或 TUN 字段；保留 MRS domain/ipcidr 支持。Stash 默认 Fake-IP，可配置排除域名，显式关闭会返回错误。前端依据共享能力隐藏不适用开关，不把它标成 redir-host。

当前 ParserVersion=3、CompilerVersion=7。生成产物保存节点数和不含明文秘密的连接摘要；解析后比较数量、名称、凭据、TLS、传输与协议参数，拒绝节点丢失或关键字段变化。等价默认值、公开说明的自签证书固定值映射被规范化。文本客户端的注释前缀名称（#、;、//）明确拒绝；允许跳过不兼容节点时，节点数与诊断同时减少。节点语义校验不等于应用完整配置已导入。Egern 仍只报告编码完成。

## 验证范围与预算

Egern 独立渲染 IPv4 `ip_cidr` 与 IPv6 `ip_cidr6`，支持 UDP 中继、Hysteria2 跳跃端口/间隔及 salamander、VLESS/VMess TLS gRPC、WebSocket、Reality 和证书 SHA256 指纹。TUIC 未指定 ALPN 时输出 h3；显式 ALPN 仅在 TUIC 保留。TUIC 自定义拥塞控制/UDP 中继模式、gRPC 明文与未支持字段会拒绝或按输出策略跳过。该适配器仍为实验级，不能以其他客户端核心检查代替 Egern 应用导入。

开发/CI 的客户端测试基线为 sing-box 1.14.4、Mihomo 1.19.33。Surge、Surge Mac、Loon、Quantumult X、Surfboard、Stash、Egern、Shadowrocket 仍标注应用导入未验收；OpenClash 核心路径不代替实体路由器验收。Mihomo 对部分自签证书 URI 参数的处理存在限制，优先使用原生配置。

来源/集合/模板等每类最多 128，节点 4096，单响应与合计输出正文 8 MiB，远程模板/规则正文合计 4 MiB，单 JSON 投影 16 MiB，任务结果 256 KiB。账户覆盖最多 4096 条。模板组成员展开另有预算，不能用无限扩展或丢弃安全字段换取兼容。

设计参考 [MiniSubConvert](https://github.com/bestruirui/MiniSubConvert) 与 [Sub-Store](https://github.com/sub-store-org/Sub-Store)。MiniSubConvert 复用 Sub-Store 核心，两者不是独立的兼容性证明；本项目独立实现 Go 转换器，固定提交与许可证范围见[上游说明](upstream.md#订阅转换设计参考2026-11-19)。

## 从现有模板和规则导入

在「订阅编排 → 模板 → 导入模板 / 路由规则」粘贴内容，选择目标客户端并预览，再加入草稿。支持：

| 输入                       | 保留内容                                         | 明确限制                                                             |
| -------------------------- | ------------------------------------------------ | -------------------------------------------------------------------- |
| 本项目声明式 JSON          | 已支持的模板字段                                 | 目标客户端必须一致；未知字段拒绝；远程 URL 不自动继承                |
| ACL4SSR 等规则项目的 .list | DOMAIN、DOMAIN-SUFFIX、IP-CIDR、IP-CIDR6         | 可选择默认目标；不直接导入 INI、脚本或动态策略名称                   |
| Mihomo YAML                | rules 中上述简单规则及末尾 MATCH,PROXY           | 不导入节点、组、DNS、监听或脚本；不能映射的规则明确报错              |
| sing-box JSON              | 单一匹配字段的 domain/domain_suffix/ip_cidr 路由 | 保留 route/reject 行为；多条件、逻辑规则和自定义 outbound 需手动配置 |

导入只允许 PROXY、DIRECT、REJECT（block 对应 REJECT）目标，不猜测其他项目策略组的含义。多条件的 AND 语义不会被错误拆成 OR。遇到不支持的规则会拒绝整次导入，不静默漏掉规则。输入限 512 KiB，继续执行客户端能力、规则数和原生字段限制。

来源 URL、版本/提交号、格式和内容 SHA256 保存到 origin，可从模板行内「来源与版本」查看。来源信息用于追溯，不表示上游官方认可或已完成实机验证。内置预设记录本项目发行版本，sing-box/Mihomo 预设附官方格式参考。模板与外部规则项目的许可证仍应分别遵守。

加入草稿之后须关联输出并预览，通过配置校验后再应用。现有远程模板刷新仍只接受本项目声明式 JSON，并保留既有缓存与发布校验机制。其他客户端的实验适配标签继续保留，不将核心格式检查等同于手机客户端实机连接测试。

## 刷新结果与链接限制

普通账户分享的高级选项可设置链接到期时间和最大下载次数；留空跟随权益。链接限制不能延长权益有效期、增加额度或扩大 profiles/scope，达到任一限制即停止下载。

刷新在后台队列执行，来源和规则资源按独立预算分批推进，未完成项目由后续自动调度继续。任务详情显示已检查/成功项数、是否发布与待处理数；失败任务同样有报告。published=true 表示已发布通过校验的输出，可能使用保留的有效资源；任务 state=failed 仍可能伴随 outcome=cached，不能仅按任务状态判断既有订阅不可用。

## 投递状态与取消

手动及自动投递统一返回 DeliveryResult：outcome、selected、items、interrupted；每项包含 id、status、persisted 与可选 error。状态页展示当前配置的投递目标、输出、触发方式、最近检查、最后确认成功、连续失败和自动重试时间；目标地址、密码、令牌、内容摘要不进入状态响应。

已确认成功的目标保存摘要，内容和目标配置未变时不重复发送。整体取消后，已完成结果使用独立 5 秒预算保存；无关配置变更不丢失记录，替换目标地址/凭据后不会继承旧确认。配置编译结束发布时合并最新记录，避免覆盖期间已完成的投递。

状态 delivered 表示远端确认送达，unchanged 表示此前已确认相同内容，failed 表示明确错误，interrupted 表示操作中断，uncertain 表示远端可能已接收但本地没有完整确认。Telegram 多段消息部分成功、写入请求超时或确认正文中断都可能出现 uncertain；重试仍可能产生重复消息或提交，不能保证恰好一次。重试退避从 60 秒增加，上限 6 小时；手动投递可立即触发。

投递与刷新共用后台任务队列，每次投递最多两个并发网络请求，不占用配置控制队列。配置候选激活但尚未确认时，不会从不一致的订阅快照启动投递。网络期间发生的配置变化不撤回已发出的远程写入。

## 显式输出与预览一致性（2026-11-25）

订阅发布只保存已配置输出的快照和必要运行投影，不再隐式创建 `sbox.json`、`clmi.yaml`、`proxies.yaml`、`jhsub.txt`、`jhsub-base64.txt` 或 `share.json`（包括 VPNGate 子目录）。通过账户下载或配置输出的授权 URL 获取内容；输出 ID 不再决定额外的本地文件路径。

固定 Argo 隧道在应用前的候选预览中就会包含对应节点；更换域名和关闭隧道同样影响预览、应用后快照与账户下载。临时隧道只复用与候选隧道设置一致的观察值。
