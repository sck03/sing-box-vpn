**1.14.4 兼容性说明**

测试基线为官方 sing-box 1.14.4，工具链为 Go 1.27.3；基线 SHA256 记录在 versions.json。
安装与菜单更新从官方 API 获取最新稳定核心，1.14.4 不构成版本锁定。
研究阶段检查了 sing-box-yg 的新版服务端、四种客户端分支和 Hy2 跳跃格式。
实现统一使用现代 DNS、WireGuard endpoint、HTTP client、route action 和 SRS 规则集。

规则中的 network 应使用独立名称，不能把 udp,tcp 当作一个匹配字符串。
Hy2 server_ports 的单端口写成 31200:31200。Mihomo 使用自身格式和版本校验。
SRS 中国规则仍支持；它不是被移除的旧 geosite/geoip 数据库选项。

验证脚本包含配置/迁移、认证代理、真实五协议 TCP/UDP，以及受控 Linux OpenVPN
与命名空间测试。公共节点的可用性和特定 VPS 网络仍取决于实际运行环境。
许可证与代码来源见 NOTICE.md。
