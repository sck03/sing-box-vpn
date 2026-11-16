# v4 订阅与下载授权

[使用指南](usage.md) · [架构](architecture.md)

管理与下载共用业务模型，但权限和端口独立。管理员通过 Cookie/CSRF 使用 `/api/*`；账户下载使用 `/u/<token>/<format>`，用户自助页为 `/user#<token>`。第三方/自定义编译输出使用独立下载监听的 `/s/<token>/<output-id>`。旧 bearer 管理接口和菜单不受支持。

## 账户分享

先创建权益、绑定入站，再在「下载授权」创建授权。只存 token 的哈希，创建时展示一次完整用户地址。授权指定权益、normal/vpngate/全部、允许客户端、到期和下载次数。客户端选项与 `/api/clients` 的目录一致；每次导出仍校验该权益所有节点能否表达，不保证选择任意格式都能下载成功。

账户下载次数 `null` 不限、0 禁止；HEAD/状态查询不消费，成功正文下载使用数据库原子预留；并发请求不会超发。下载过期只阻止重新获取；代理是否能用由权益控制。撤销下载 token 不替代代理凭据轮换。授权检查在缓存/304 前执行。

## 来源、集合、模板、输出

v4 初始无共用入口输出。自定义流程：

1. 来源选择 account（填写权益 ID 和 scope）、remote（URL/格式）或 inline（正文/格式）。
2. 集合引用来源，可过滤、排序、重命名与去重。
3. 模板选择客户端与 mode。原生配置使用 sing-box/mihomo；OpenClash 使用 Mihomo provider；URI/base64 仅节点列表。
4. 输出关联集合与模板；预览查看能力诊断；不兼容节点需明确允许跳过。
5. 保存应用后刷新远端来源；创建输出下载授权或分发目的地。

来源支持 URI/Base64、sing-box、Mihomo 和节点 JSON。服务端拒绝重复引用、过量展开和不支持的安全字段。模板里的路由和 DNS 作用于客户端，与「路由与 DNS」的服务端配置相互独立。

账户来源用账户专属凭据构建；禁用/到期/耗尽账户的缓存输出在下载与分发时再次拒绝，避免旧快照被当作授权。第三方直连节点不经过本机认证入口，本项目不能承诺代理流量额度。

权益授权和输出授权统一通过 `/api/grants` 创建、查询，通过 `/api/grants/revoke` 撤销。token 由服务端生成，只保存哈希；授权创建及撤销立即生效，无需应用配置。所有授权的 `max_downloads=null` 表示不限，`0` 表示禁止下载。没有默认共享令牌，配置中不再保存 grants。一个授权选择权益及客户端格式，或选择输出列表，不能同时选择两者。HEAD/304 也先检查授权、次数与订阅者状态，但不消耗次数。

启用下载监听后，分享地址使用它的 `public_base_url`（可带反向代理路径前缀）；未填写则使用发布地址和下载端口。关闭下载监听时，权益用户页使用面板地址；自定义输出授权需要启用下载监听。禁用订阅者会同时阻止其所有权益下载、导出与关联输出分发。

用户页资源、用量接口和下载链接使用相对路径，支持 `/sub/user#token` 等反向代理前缀。fragment 保留以支持刷新，不随 HTTP 请求或 Referrer 发送；持有完整分享地址即持有下载权限。

## 客户端与验证范围

| 客户端 ID | 模式 | 验证范围 |
| --- | --- | --- |
| sing-box | nodes、config | 1.14.4 核心配置检查与真实协议链路 |
| mihomo | nodes、provider、config | 1.19.33 核心配置检查与真实协议链路 |
| openclash | provider、config | 复用 Mihomo；实体路由器/插件未验收 |
| surge、surge-mac、loon、quantumult-x、surfboard | nodes、config | 实验编码器，未完成应用导入验收 |
| stash | provider、config | 实验编码器，未完成应用导入验收 |
| egern | nodes、config | 实验编码器，未完成应用导入验收 |
| shadowrocket | nodes、rules | URI 编解码；应用导入未验收 |
| uri、base64 | nodes | 编解码往返；最终表现取决于导入器 |

协议和特性以 `internal/clientprofile/catalog.go` 为唯一登记源。表单只提供该客户端支持的模式，按模式隐藏不适用的 DNS/TUN/测速字段。Loon 不输出 TUIC；Quantumult X 不输出 Hysteria2/TUIC；Surfboard 不输出 VLESS。所有列表只是当前编码器边界，不能视为各应用最新版本的兼容认证。

不支持的证书固定、端口跳跃、显式 uTLS 指纹、Egern 非 TUIC ALPN、部分文本客户端的显式 TUIC 参数会拒绝生成；`skip_unsupported` 只允许跳过节点并附诊断，不会篡改节点安全参数。`AUTO/PROXY/DIRECT/REJECT/VPNGate` 为保留名称，大小写变体同样拒绝。URI 的自签证书导入存在额外限制，应优先选择原生格式。

## 调度、检测与分发

面板 worker 管理后台刷新和分发，独立下载进程只处理下载。远端来源有刷新间隔、最大陈旧时间；304 也合并合法用量/到期元数据。可选过期源在重新编译时移除；必需来源失败仅撤下依赖它的输出，不阻塞无关本地配置。

远程模板和规则的已验证正文、ETag 一起持久缓存，面板重启/重建后继续使用；URL 更改会使两者同时失效。资源刷新不改写用户配置或其 revision。没有待刷新来源/资源、没有到期自动分发时不生成空任务。

按需检测最多 64 节点、2 个探测核心并行，每节点期限 12 秒，结果在任务详情展示。先检查最终输出，再对通过能力筛选的节点发起真实协议 HTTP 请求。报告单列 `artifact_validation` 和 `connectivity_engine`；后者为 sing-box，不代表 Surge 等应用实际导入成功。Mihomo 检查使用 `MIHOMO_BINARY` 或 PATH 中的 mihomo；分流模式还要求 `MIHOMO_HOME/Country.mmdb`，缺失会明确标注只完成编码检查。检查不会为了补齐验证静默下载地理数据库。

只读探测使用独立后台任务通道；配置变更、订阅刷新与外部分发仍串行执行，避免过期快照发布。必需账户停用/过期/删除，以及单个输出渲染或核心校验失败，在恢复/刷新时撤下相应输出并保留诊断，面板仍可启动修复；严格预览/应用会报告错误。

支持 HTTP PUT、WebDAV、secret Gist、私有 GitLab 和 Telegram。自动分发需显式启用；目标未变化时不重复发送，失败有退避，通知失败不会撤销已应用配置。访问 token、下载 URL、节点凭据不要放进公开日志或公开仓库。外部平台可能有自身费用/限额；程序及依赖不要求付费许可证。

## OpenClash 与证书

为账户来源创建 `client=mihomo, mode=provider` 模板和输出，在 OpenClash 的 proxy-providers 引用该授权 URL。保留路由器自己的分流。完整配置则使用 `mode=config`。尚未代表实体 OpenWrt/LuCI 验收。

Mihomo URI 导入忽略 VMess insecure 与 TUIC allow_insecure，自签 TLS 使用原生 Mihomo 配置/provider。CA 证书使用系统信任，自签节点导出固定指纹；更换自签证书需重新下载。

数据/对象预算以 `internal/subscription` 常量为准：来源/集合等每类最多 128，单订阅响应 8 MiB，远程模板与规则正文合计 4 MiB，生成 JSON 文件 16 MiB，任务结果 256 KiB。不得通过删除安全校验扩大容量。
