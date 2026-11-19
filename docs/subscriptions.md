# 订阅与分享

[使用指南](usage.md) · [架构](architecture.md)

管理与下载共用业务模型，权限独立，端口可共用或分开。管理员通过 Cookie/CSRF 使用 `/api/*`；账户下载使用 `/u/<token>/<format>`，用户自助页为 `/user#<token>`。第三方/自定义编译输出使用 `/s/<token>/<output-id>`，可共用面板 HTTPS 或选择独立订阅服务。旧 bearer 管理接口和菜单不受支持。

## 账户分享

先在「系统设置 → 公网访问」配置 HTTPS 地址，再在「用户与分享 → 权益与用量」创建权益、绑定入站，在权益行点击「分享」。未绑定启用入站时主操作为「绑定」，后端拒绝创建不可下载的分享；分享表单仅提供该权益已绑定的运行范围。链接 ID 自动生成，不设置第二个期限或下载次数；代理时间与流量由权益统一控制。创建后展示完整用户地址与复制按钮；只保存 token 哈希，完整地址仅展示一次。在「用户与分享 → 分享链接」可以查看记录和撤销。客户端格式与 `/api/clients` 目录一致；导出仍会检查节点是否支持所选格式。

既有链接的入站解绑或停用后，用量接口返回 `download_denial=no_bound_inbound`，用户页说明需要联系管理员，下载返回 403；重新绑定后同一链接可恢复。首次使用权益不会因分享、查看用量或下载而激活。用户页显示合计、上传/下载与实际开始时间，未激活时说明首次成功代理连接后起算。

用户分组收纳在权益页的可选区域，展开时才读取。普通单权益用户无需分组。下载令牌是私有链接与撤销的内部机制，不能删除后改成任何人可下载；它不增加代理额度，也不要求日常设置单独下载策略。

账户下载次数 `null` 不限、0 禁止；HEAD/状态查询不消费，成功正文下载使用数据库原子预留；并发请求不会超发。下载过期只阻止重新获取；代理是否能用由权益控制。撤销下载 token 不替代代理凭据轮换。授权检查在缓存/304 前执行。

## 来源、集合、模板、输出

「输出」标签里的分享选择器只列出已应用的输出。先配置可分享的 HTTPS 面板地址或独立订阅地址，再创建输出授权。草稿中的新增输出会在应用成功后出现。额度或下载次数用尽时，用户页仍显示用量和原因，并隐藏复制与下载按钮。

首次安装没有共用入口输出。自定义流程：

「高级功能 → 订阅编排」包含「订阅来源」「节点集合」「模板与规则」「输出与授权」「分发与检测」「订阅运行状态」标签，首次选中才创建内容；状态读取失败可重试。普通账户分享不需要进入这里。当前订阅配置版本为 **3**，只接受 account、remote、inline。旧配置直接拒绝，不迁移旧数据；模板 version=1、节点文档 schema=2 与该版本独立。

1. 来源选择 account（填写权益 ID 和 scope）、remote（URL/格式）或 inline（正文/格式）。
2. 集合引用来源，可过滤、排序、重命名与去重。
3. 模板选择客户端与 mode。原生配置使用 sing-box/mihomo；OpenClash 使用 Mihomo provider；URI/base64 仅节点列表。
4. 输出关联集合与模板；预览查看能力诊断；不兼容节点需明确允许跳过。
5. 保存应用；新增或过期远端来源会在候选准备阶段获取，缓存随任务持久保存。随后可创建输出授权、设置自动刷新与分发。

来源格式为 auto、uri、base64、sing-box、mihomo、nodes、surge、loon、quantumult-x；auto 根据正文识别格式。文本输入提取代理段或纯节点列表，忽略注释，不导入脚本、规则和本地文件。遇到存在歧义的文本，请显式选择格式。节点支持 TCP/WS/gRPC/HTTPUpgrade、TLS/Reality，以及 Hysteria2 salamander 混淆；目标客户端无法表达时拒绝或按输出策略跳过。

来源的 skip_invalid 默认 false，面板新来源默认开启：逐节点跳过无效或不支持的记录，保留原因。语法损坏、结构错误、超过节点预算或全部无效仍失败，远端保留上次成功缓存并执行陈旧期限。它与输出的 skip_unsupported 分别控制输入解析和客户端能力筛选。集合表单提供正则筛选、重命名、排序和连接参数去重；重名或保留名称按来源顺序追加稳定后缀“ · 2”。直接调用渲染器仍拒绝未经集合整理的重名。

服务端拒绝重复引用、过量展开和无法保留的连接、安全字段。模板里的路由和 DNS 作用于客户端，与「路由与 DNS」的服务端配置相互独立。

账户来源用账户专属凭据构建；禁用/到期/耗尽账户的缓存输出在下载与分发时再次拒绝，避免旧快照被当作授权。第三方直连节点不经过本机认证入口，本项目不能承诺代理流量额度。

权益授权和输出授权统一通过 `/api/grants` 创建、查询，通过 `/api/grants/revoke` 撤销。token 由服务端生成，只保存哈希；授权创建及撤销立即生效，无需应用配置。所有授权的 `max_downloads=null` 表示不限，`0` 表示禁止下载。没有默认共享令牌，配置中不再保存 grants。一个授权选择权益及客户端格式，或选择输出列表，不能同时选择两者。HEAD/304 也先检查授权、次数与订阅者状态，但不消耗次数。

在「公网访问」启用独立下载监听后，分享使用必填的 HTTPS `public_base_url`。选择内置 TLS 时绑定有效证书与私钥，无需 Nginx/Caddy；选择 HTTPS 反向代理时自行配置代理、可信 CIDR 和 Host/协议头，URL 可含被代理剥离的路径前缀。关闭独立监听时，账户与高级输出分享均使用当前生效的 HTTPS 面板地址。明文公网 URL、`127.0.0.1`、`localhost`、`::1` 和未指定 IP 会在配置或创建分享前拒绝，不产生无效令牌记录。用户分组停用会阻止所属权益下载、导出与关联分发。部署示例见[公网访问](public-access.md)。

用户页资源、用量接口和下载链接使用相对路径，支持 `/sub/user#token` 等反向代理前缀。fragment 保留以支持刷新，不随 HTTP 请求或 Referrer 发送；持有完整分享地址即持有下载权限。

## 客户端与验证范围

| 客户端 ID                                       | 模式                    | 验证范围                           |
| ----------------------------------------------- | ----------------------- | ---------------------------------- |
| sing-box                                        | nodes、config           | 1.14.4 核心配置检查与真实协议链路  |
| mihomo                                          | nodes、provider、config | 1.19.33 核心配置检查与真实协议链路 |
| openclash                                       | provider、config        | 复用 Mihomo；实体路由器/插件未验收 |
| surge、surge-mac、loon、quantumult-x、surfboard | nodes、config           | 实验编码器，未完成应用导入验收     |
| stash                                           | provider、config        | 实验编码器，未完成应用导入验收     |
| egern                                           | nodes、config           | 实验编码器，未完成应用导入验收     |
| shadowrocket                                    | nodes、rules            | URI 编解码；应用导入未验收         |
| uri、base64                                     | nodes                   | 编解码往返；最终表现取决于导入器   |

协议和特性以 `internal/clientprofile/catalog.go` 为唯一登记源。表单只提供该客户端支持的模式，按模式隐藏不适用的 DNS/TUN/测速字段。Loon 不输出 TUIC；Quantumult X 不输出 Hysteria2/TUIC；Surfboard 不输出 VLESS。所有列表只是当前编码器边界，不能视为各应用最新版本的兼容认证。

不支持的证书固定、端口跳跃、显式 uTLS 指纹、Egern 非 TUIC ALPN、部分文本客户端的显式 TUIC 参数会拒绝生成；`skip_unsupported` 只允许跳过节点并附诊断，不会篡改节点安全参数。`AUTO/PROXY/DIRECT/REJECT/VPNGate` 为保留名称，大小写变体同样拒绝。URI 的自签证书导入存在额外限制，应优先选择原生格式。

## 调度、检测与分发

立即检测和分发使用管理员 GET /api/subscriptions/actions 的已应用 ID 清单，不使用尚未应用的草稿。运行状态增加已发布输出、实际筛选节点数量、核心/编码验证及截断诊断。已发布只表示产物存在，下载与分发仍逐请求检查权益、令牌和次数。

后台调度使用专用的小型 schedule.json；编译根据选定节点与有效模板摘要复用未变化产物，核心验证另核对核心身份。配置及运行元数据仍以原子发布日志提交，缓存不替代业务校验或授权。

### 声明式模板

sing-box、Mihomo、OpenClash、Stash 的完整配置模板提供策略组、远程规则 Provider 和 DNS 策略。策略组支持 select、urltest，Mihomo 系还支持 fallback；可按普通/VPNGate 范围及名称正则选择节点。members 非空时只引用其他自定义组，禁止循环、空组及节点同名；VPNGate 专属组加入默认 VPNGate 选择器。规则目标可引用这些组。

本地规则保持数组顺序，其后依次为引用的本地规则集、自定义 Provider、国内分流、最终路由。sing-box 使用域名与 IP SRS，Mihomo/OpenClash 使用域名与 IP MRS，不依赖 Country.mmdb。远端规则由客户端在运行时获取，核心配置检查不会证明远端文件可达或规则内容正确；需在目标网络验证。Stash 仍属实验适配。

DNS 策略分为 local、remote、bootstrap，bootstrap 只允许 IP，其他角色可用 IP 或 udp/tcp/tls/https URL。sing-box 每个角色限一个解析器，域名形式解析器显式使用 bootstrap，DNS 最终使用代理出口，国内域名走 local。Mihomo 系可选择 Fake-IP、排除域名和 IPv6。省略 dns_policy 使用内置默认；显式关闭 Fake-IP 时应填写至少一个角色解析器。服务器 DNS 的 DoH 路径、引导解析器和客户端 DNS 策略是独立设置。

native 是完整配置的补充根字段，例如本地控制地址。编译器生成的每个根字段整体覆盖 native 同名字段，包括节点、组、路由、DNS、日志与监听；它不用于替换受管路由或拼接任意原生节点。VPNGate-only 输出拒绝 native、国内直连、DIRECT 规则/Provider。高级字段仍需对应核心检查，实验客户端需要应用导入验收。

示例模板见 [Mihomo](examples/template-mihomo.json) 与 [sing-box](examples/template-sing-box.json)，远程模板使用相同 version=1 对象。设计参考 [MiniSubConvert](https://github.com/bestruirui/MiniSubConvert) 与 [Sub-Store](https://github.com/sub-store-org/Sub-Store)，固定参考提交及范围见 [上游说明](upstream.md#订阅转换设计参考2026-11-19)。本项目独立实现 Go 编译器，不运行上游脚本。

面板 worker 管理后台刷新和分发，独立下载进程只处理下载。远端来源有刷新间隔、最大陈旧时间；304 也合并合法用量/到期元数据。可选过期源在重新编译时移除；必需来源失败仅撤下依赖它的输出，不阻塞无关本地配置。

远程模板和规则的已验证正文、ETag 一起持久缓存，面板重启/重建后继续使用；URL 更改会使两者同时失效。资源刷新不改写用户配置或其 revision。没有待刷新来源/资源、没有到期自动分发时不生成空任务。

模板表单提供远程模板 URL；通知设置提供 Webhook。完整草稿只通过 `POST /api/config/preview` 校验，原 `/api/subscriptions/preview` 已移除。订阅任务请求为 `{"operation":"subscription","subscription":{"operation":"check","id":"output-id","probe_url":"https://example.com"}}`；命令不再包含旧授权/归档字段。

「运行状态」显示远端来源、模板和规则最近检查、最后成功、下次尝试及错误，并显示 Webhook 最后发送结果、连续失败次数与事件对象。状态来自已发布配置，草稿不改变运行记录；可手动刷新。通知记录只表示最后一次事件发送结果，不等于远端服务的实时健康检测。修改 Webhook 地址后不复用原地址的成功状态。

`GET /api/subscriptions/status` 仅管理员可读，不返回远端 URL、Webhook URL、缓存正文、节点或凭据。只读配置接口也隐藏 Webhook URL。通知活动保存在独立的小型 `subscriptions/notification.json` 中，不因发送通知重写整个节点缓存；该文件为可重建运行数据，不属于账户账本。

账户来源的 revision 和可用状态单独保存在运行缓存；每分钟调度核对一次。解除禁用、增加额度、恢复订阅者或进入新账期后，即使没有远端来源也会重新编译先前撤下的输出；执行时间仍受任务队列影响。状态不变不生成重复刷新。一次编译复用同一账户快照，读取模板从空结构解码，避免默认模板的 TUN 等省略字段残留。

按需检测最多 64 节点、2 个探测核心并行，每节点期限 12 秒，结果在任务详情展示。先检查最终输出，再对通过能力筛选的节点发起真实协议 HTTP 请求。报告单列 `artifact_validation` 和 `connectivity_engine`；后者为 sing-box，不代表 Surge 等应用实际导入成功。Mihomo 检查使用 `MIHOMO_BINARY` 或 PATH 中的 mihomo；分流使用 MRS，不依赖 Country.mmdb。开发/CI 可设置 `SBVPN_REQUIRE_CLIENT_CHECK=1`，缺少 Mihomo 时直接失败，避免静默降级。

探测任务总预算为 8 分 24 秒，涵盖 64 个节点的完整最坏耗时及准备时间；普通控制任务仍为 5 分钟。中断时保留已完成结果，节点分别显示通过、失败、中断或未执行。

只读探测使用独立后台任务通道；配置变更、订阅刷新与外部分发仍串行执行，避免过期快照发布。必需账户停用/过期/删除，以及单个输出渲染或核心校验失败，在恢复/刷新时撤下相应输出并保留诊断。候选预览/应用拒绝格式及协议不兼容；暂不可用的运行数据会在输出清单中单独说明。

支持 HTTP PUT、WebDAV、secret Gist、私有 GitLab 和 Telegram。自动分发需显式启用；目标未变化时不重复发送，失败有退避，通知失败不会撤销已应用配置。访问 token、下载 URL、节点凭据不要放进公开日志或公开仓库。外部平台可能有自身费用/限额；程序及依赖不要求付费许可证。

## OpenClash 与证书

为账户来源创建 `client=mihomo, mode=provider` 模板和输出，在 OpenClash 的 proxy-providers 引用该授权 URL。保留路由器自己的分流。完整配置则使用 `mode=config`。尚未代表实体 OpenWrt/LuCI 验收。

Mihomo URI 导入忽略 VMess insecure 与 TUIC allow_insecure，自签 TLS 使用原生 Mihomo 配置/provider。CA 证书使用系统信任，自签节点导出固定指纹；更换自签证书需重新下载。

数据/对象预算以 `internal/subscription` 常量为准：来源/集合等每类最多 128，单订阅响应 8 MiB，远程模板与规则正文合计 4 MiB，生成 JSON 文件 16 MiB，任务结果 256 KiB。不得通过删除安全校验扩大容量。
