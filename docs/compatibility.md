# 兼容性与验证边界

本版只接受新数据库 schema 17 / 备份格式 8，不迁移旧账户或管理接口。平台配置 schema 8 与数据库版本独立。Linux Debian/Ubuntu + systemd，amd64/arm64；Windows 用于开发与本机测试。数据目录由 root 独占，面板默认回环。

账户绑定命令必须提供非空 `inbound_ids` 数组；省略或传空数组均返回参数错误，不再解释为“绑定全部”。面板默认选择当前启用入口并提交具体 ID，执行时再次校验；期间新建入口不会被隐式绑定。轮换、解绑不接受绑定专用字段。Go 校验、生成的任务联合类型和页面使用同一契约；不提供旧客户端兜底。此次只收紧任务输入并新增概览诊断字段，没有改变持久数据格式，因此不增加 schema 版本。

订阅刷新和分发必须具备 `runtime-revision.json`，并与数据库已应用配置一致。缺少标记时应重新应用配置以重建发布，不再根据目录标记接受未初始化状态；测试夹具也通过独立的测试初始化函数创建确认状态。

订阅配置 schema=6，不接受旧订阅配置。模板 version=1、平台 schema=8、数据库 schema=17 与备份容器 8 保持独立；同数据库版本中的旧订阅内容也会被拒绝。转换新增受控 SIP003、WS early-data、gRPC 参数、VLESS XHTTP 和 Hysteria2 地址规范化，按客户端/协议/传输/加密方式筛选。sing-box 1.14.4 不接受 XHTTP，相关输出或探测使用支持它的目标；未知 XHTTP extra 字段明确拒绝。

Release 标签使用 `vMAJOR.MINOR.PATCH`，主版本号与数据格式无绑定：v5.0.0 等正式标签可安装，但包内容必须满足当前平台约定。版本号变化不会自动迁移数据库。证书提供方接受 `acme` / `import` / `generated`（自签名测试证书），旧 `certbot` 标记不兼容。

受管核心固定 sing-box 1.14.4，适配版本 1.14.4+sbvpn.7。官方二进制用于生成配置验证；不能用任意官方核心替换受管运行时，否则丢失认证后账户计量。更新需管理程序、受管运行时和对应官方校验器一起验证并切换；lego 随管理程序更新。组件版本来自 versions.json，并由测试核对 Go 依赖。

稳定依赖核对已升级 YAML 3.0.6、x/crypto 0.59.0、SQLite 1.62.0 等。sing-box 的协议 fork 与未发布 tag 的依赖保持上游配套版本，精确版本见 go.mod/go.sum。所有链接模块的可用许可证声明位于 third_party/modules，免费开源不等于外部 VPS/域名/服务永久免费。

支持五协议入站；direct、SOCKS、五协议代理出站、selector/urltest、WireGuard endpoint、resolved DNS 服务和受管 DNS。未注册协议拒绝配置，不静默丢字段。账户只允许数据库显式绑定；入口预设凭据不是默认用户。

Mihomo 开发/CI 测试基线为 1.19.33；部署服务器无需安装它。双方协议、传输和 TLS 参数匹配时，Mihomo 客户端可连接 sing-box 服务端。原生 sing-box/Mihomo 配置和 Provider 是推荐输出。Mihomo URI 导入器忽略 VMess insecure 和 TUIC allow_insecure；这两类自签 TLS URI 不纳入真实链路通过项，其原生配置已覆盖。OpenClash 使用 Mihomo Provider 格式，尚不代表实体 OpenWrt/LuCI 验收。

客户端与模式统一登记在 `internal/clientprofile`，通过 `/api/clients` 提供详细字段范围。`/api/accounts/outputs` 按账户和实例计算实际可下载格式，公开用量的 `profiles` 返回对象数组，每项同时标明客户端语法和用途。Surge、Surge Mac、Loon、Quantumult X、Surfboard、Stash、Egern、Shadowrocket 标为实验适配；范围不代表所有应用版本和组合已通过。账户模板策略明确控制是否跳过不兼容节点，并展示数量与原因。Stash 独立 DNS 模板使用 follow-rule 与 Fake-IP 排除域名；不接受关闭 Fake-IP。该客户端仍未完成实体应用验收。节点验证比较产物数量和连接语义。详见[订阅验证范围](subscriptions.md#验证范围与预算)。

域名解析优先级使用 resolve/prefer_ipv4/prefer_ipv6，保留其他地址族回退；IP 请求不会被 SNI/HTTP Host 隐式替换。现代 DNS 与 SRS 规则集有效，旧 geosite/geoip 和 DNS 格式不兼容。

凭据绑定、轮换和解绑通过按入站重载载入新用户，保留核心进程和其他入站连接；同入站连接可能短暂中断。配置拓扑、路由和服务变更仍走完整配置应用流程。禁用/到期/旧代次在守卫执行，授权缓存最长 250 ms，另有数据库、调度及在途 I/O 延迟。协议内部认证会话对象可能存活，但守卫拒绝后不能继续转发。持久预留不保证断电零误差；每次崩溃最多保守占用每权益、每实例/账期活动池的未用预留 64 KiB，连续崩溃可以累积，方向计数可能落后最终数据。

实际已通过和未完成的验收以 [实施记录](unified-management-status.md) 为准。浏览器工具故障、未覆盖的长期负载或断电实验必须明确记录，不能等价为已验证生产稳定。

受管 DNS 当前支持 TCP、UDP、TLS、HTTPS、local、resolved；不支持 QUIC DNS。可选类型以 `/api/capabilities.runtime` 为准，运行环境和选项以实际受管核心校验为准。WARP/WireGuard 发布构建必须含 gVisor。配置预览通过只代表候选配置可加载，业务连通性仍需独立检测。

5.1 提供检查正式发行包和组件配套状态的入口；不承诺未来任意上游版本自动兼容。新发行包仍须通过本项目当前配置、原生核心、账户计量及网络测试。外部模板导入是有限、显式的规则转换，不能把其他项目的完整配置直接视为本项目模板。

2026-11-25：当前平台配置为 7、节点文档为 5；本轮任务队列与持久取消结构调整将数据库提升至 17。订阅为 6、备份容器为 8，各版本独立管理。旧 app.argo、routing、socks_port、country、Profile 自签标志与 Reality 公钥副本、profile_order 均移除；使用全新初始化的数据。固定名称的本地订阅文件不再生成，使用显式输出和下载接口。
