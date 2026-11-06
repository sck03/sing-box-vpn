**1.14.4 兼容性说明**

测试基线为官方 sing-box 1.14.4，工具链为 Go 1.27.3；基线 SHA256 记录在 versions.json。
安装与菜单更新从官方 API 获取最新稳定核心，1.14.4 不构成版本锁定。
研究阶段检查了 sing-box-yg 的新版服务端、四种客户端分支和 Hy2 跳跃格式。
实现统一使用现代 DNS、WireGuard endpoint、HTTP client、route action 和 SRS 规则集。

规则中的 network 应使用独立名称，不能把 udp,tcp 当作一个匹配字符串。
Hy2 server_ports 的单端口写成 31200:31200。Mihomo 使用自身格式和版本校验。
SRS 中国规则仍支持；它不是被移除的旧 geosite/geoip 数据库选项。

域名分流保留。官方 1.14 的路由规则仍支持 `domain`、`domain_suffix`、`domain_keyword`、
`domain_regex` 和 `rule_set`，SOCKS5 / WARP 仍可作为匹配后的出口。
旧 geosite/geoip 数据库在 1.12 被移除，旧 DNS server 格式在 1.14 被移除；
旧字段的迁移不等于取消域名分流。

本项目的 IPv4 / IPv6 设置使用 `resolve` 的 `prefer_ipv4` / `prefer_ipv6`。
它们是解析优先级，保留另一地址族的可用回退。1.14.4 的 `resolve` 仅解析请求目标中的域名，
不会用 HTTP Host / TLS SNI 改写已有目标 IP；菜单与状态输出据此使用“优先解析”表述。
真实核心验收覆盖域名请求、IP 请求的 HTTP 域名嗅探以及 IPv6 优先设置对仅 A 记录目标的回退。

官方参考：[路由规则](https://sing-box.sagernet.org/configuration/route/rule/)、
[resolve 动作](https://sing-box.sagernet.org/configuration/route/rule_action/#resolve)、
[废弃功能清单](https://sing-box.sagernet.org/deprecated/)。

Go 验收包含当前配置初始化、认证代理、真实五协议 TCP/UDP，以及受控 Linux OpenVPN
与命名空间测试。公共节点的可用性和特定 VPS 网络仍取决于实际运行环境。
UDP 验收包含慢域名解析及阻塞写入期间其他端口继续转发、多个目标独立解析、队列上限和关联关闭取消。
8 个目标端口的并发传输校验每个端口的回复、1024 个数据包的内容及按目标的发送顺序。
SOCKS/worker 的帧格式与认证协议保持兼容，单向活跃 UDP 的约 60 秒空闲阈值保持原值。
Linux 验收同时模拟网关被强制结束后的重启，确认旧隧道进程被回收后再建立新隧道。
systemd 专项验收模拟内核防火墙规则丢失及配置提交中断，再触发正常退出码的意外退出，
验证自动重试、启动前恢复配置/IPv4/IPv6 规则、订阅沙箱兼容及主动停止后保持停止。
这模拟了重启后的恢复状态，实际 VPS 断电及文件系统恢复仍须在目标机器验证。
许可证与代码来源见 NOTICE.md。

当前版本仅支持新部署及当前 Go 配置，不提供旧 sing-box-yg/Python 配置导入、旧防火墙链迁移
或历史 URI 字节兼容。VMess 分享链接使用标准 JSON 和 Base64，中文与 emoji 按 UTF-8 编码。
当前配置重装和国家切换继续保留各自端口、凭据与 Reality 密钥。
配置与更新使用同一文件锁，客户端校验失败不会提前替换服务端配置。

普通与 VPNGate 改为独立导出。根目录原订阅地址只返回普通节点；当前国家的 VPNGate
客户端文件位于 `vpngate/`，订阅地址增加 `/vpngate/`。已保存国家的端口与凭据保留，
切回时恢复原链接。只为当前国家监听五个入口；升级核心同时校验两套客户端与服务端配置。
普通客户端维持原有自动测速与分流；VPNGate 客户端只手动选择，不回退到普通节点或公网直连。

控制动作要求最多 16 KiB 的单个完整 JSON 对象，尾随对象、垃圾或超限空白均在修改配置前拒绝。
核心版本号的三个部分均检查范围，再比较最低版本；未来稳定版本继续可用。
