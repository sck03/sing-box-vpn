# v4 兼容性与验证边界

本版只接受新数据库 schema 9 / 备份格式 7，不迁移旧账户或管理接口。平台配置 schema 4 与数据库版本独立。Linux Debian/Ubuntu + systemd，amd64/arm64；Windows 用于开发与本机测试。数据目录由 root 独占，面板默认回环。

受管核心固定 sing-box 1.14.4，适配版本 1.14.4+sbvpn.2；截至 2026-11-17 查询仍是上游最新稳定版。官方二进制用于生成配置验证；不能用任意官方核心替换受管运行时，否则丢失认证后账户计量。更新需双程序一起验证。

稳定依赖核对已升级 YAML 3.0.6、x/crypto 0.59.0、SQLite 1.62.0 等。sing-box 的协议 fork 与未发布 tag 的依赖保持上游配套版本，精确版本见 go.mod/go.sum。所有链接模块的可用许可证声明位于 third_party/modules，免费开源不等于外部 VPS/域名/服务永久免费。

支持五协议入站；direct、SOCKS、五协议代理出站、selector/urltest、WireGuard endpoint、resolved DNS 服务和受管 DNS。未注册协议拒绝配置，不静默丢字段。账户只允许数据库显式绑定；入口预设凭据不是默认用户。

Mihomo 测试基线 1.19.33。原生 sing-box/Mihomo 配置和 provider 是推荐导出方式。Mihomo URI 导入器忽略 VMess insecure 和 TUIC allow_insecure；这两类自签 TLS URI 不纳入真实链路通过项，其原生配置已覆盖。OpenClash 使用 Mihomo provider 的结构与核心路径，尚不代表实体 OpenWrt/LuCI 验收。

客户端与模式统一登记在 `internal/clientprofile`，通过 `/api/clients` 提供给面板和授权流程。Surge、Surge Mac、Loon、Quantumult X、Surfboard、Stash、Egern、Shadowrocket 标为实验适配；协议列表表示编码器覆盖范围，不表示所有应用版本及组合已通过。不能表达的显式参数拒绝生成，只有输出明确允许时才跳过节点并记录诊断。详见[订阅模板验证](subscriptions.md#客户端与验证范围)。

域名解析优先级使用 resolve/prefer_ipv4/prefer_ipv6，保留其他地址族回退；IP 请求不会被 SNI/HTTP Host 隐式替换。现代 DNS 与 SRS 规则集有效，旧 geosite/geoip 和 DNS 格式不兼容。

凭据绑定和轮换需要重启对应实例载入新用户，影响该实例其他连接。禁用/到期/旧代次在守卫执行，授权缓存最长 250 ms，另有数据库、调度及在途 I/O 延迟。协议内部认证会话对象可能存活，但守卫拒绝后不能继续转发。持久预留不保证断电零误差；每次崩溃最多保守占用每权益、每实例/账期活动池的未用预留 64 KiB，连续崩溃可以累积，方向计数可能落后最终数据。

实际已通过和未完成的验收以 [实施记录](unified-management-status.md) 为准。浏览器工具故障、未覆盖的长期负载或断电实验必须明确记录，不能等价为已验证生产稳定。
