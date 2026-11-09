# 上游技术与协议

[返回项目首页](../README.md) · [OpenClash 使用](usage.md#openclash) · [兼容性与验收](compatibility.md)

核对日期：**2026-11-10**。以下来自官方发布记录、对应标签的文档/源码和 VPNGate 官网。
上游支持某项功能，不代表本项目已经接入其配置和出口验证。

## sing-box

最新稳定版仍为 [1.14.4](https://github.com/SagerNet/sing-box/releases/tag/v1.14.4)，
最新预发布为 [1.15.0-alpha.13](https://github.com/SagerNet/sing-box/releases/tag/v1.15.0-alpha.13)。
本项目安装与更新使用稳定渠道，当前验收基线不变，不为旧配置增加迁移层。

[1.14 官方变更记录](https://github.com/SagerNet/sing-box/blob/v1.14.4/docs/changelog.md)
中与本项目相关的新增能力：

| 能力 | 上游范围 | 本项目状态 |
| --- | --- | --- |
| Snell | 新增入站/出站；当前标签文档列出 v5/v6 入站、v4/v6 出站，不含 v5 QUIC 模式 | 尚未增加第六种菜单协议 |
| OpenVPN | 新增客户端/服务端 endpoint、推送 DNS、认证及隧道参数 | VPNGate 仍使用独立 OpenVPN 进程和 netns |
| OpenConnect | AnyConnect、GlobalProtect、Fortinet 等客户端 endpoint | 尚未提供企业 VPN 接入配置 |
| Hysteria2 | Chrome QUIC 指纹、BBR profile、随机跳跃间隔、gecko 混淆；Realm 服务用于 NAT 穿透 | 已有普通 Hy2 与端口跳跃；新增选项尚未开放到菜单 |
| 网络命名空间 / L3 | 声明式 netns、bridge、L3 转发 | 现有网关沿用经过隔离验收的独立 worker 后端 |

Snell 的版本需要分别匹配：
[sing-box 入站](https://github.com/SagerNet/sing-box/blob/v1.14.4/docs/configuration/inbound/snell.md)、
[出站](https://github.com/SagerNet/sing-box/blob/v1.14.4/docs/configuration/outbound/snell.md)与
[Mihomo 1.19.33 实现](https://github.com/MetaCubeX/mihomo/blob/v1.19.33/adapter/outbound/snell.go)
并非相同能力集合。该 Mihomo 实现将 v5 映射为 v4，未接受 v6；因此不能直接将新的 Snell v6
导出给 OpenClash。新增协议前须完成独立凭据、端口、防火墙、三类客户端格式和真实双向协议验收。

sing-box 的[内置 OpenVPN 客户端](https://github.com/SagerNet/sing-box/blob/v1.14.4/docs/configuration/endpoint/openvpn-client.md)
为简化后端提供了可评估的方向。切换需要实现 profile 到 endpoint 的受控映射，并重新验收
隧道 DNS、错误国家拒绝、故障后禁止直连和崩溃恢复；目前不将它标记为已支持的 VPNGate 后端。

## OpenClash / Mihomo

官方最新版本分别为 [OpenClash 0.48.002](https://github.com/vernesong/OpenClash/releases/tag/v0.48.002)
和 [Mihomo 1.19.33](https://github.com/MetaCubeX/mihomo/releases/tag/v1.19.33)。
OpenClash 更新包括 Ruby/UCI 加载优化、TUN 默认 GSO、更新 SHA256 校验与订阅引导改进；
Mihomo 更新包括 DNS 拨号收敛、代理循环检查性能及重载保留 fake-ip 分配等修复。
实际效果依赖 OpenWrt 固件和路由器硬件，不能直接换算为 VPS 的性能提升。

本项目提供完整 `clmi.yaml` 和仅含原生节点的 `proxies.yaml`。普通与 VPNGate 分开发布、
使用同一套协议转换，避免为 OpenClash 另维护一套节点字段。HTTP provider 已用 Mihomo 验收
五协议 TCP/UDP，涵盖自签 VMess/TUIC 与中文 WS 路径；尚未做实体 OpenWrt/LuCI 端到端验收。

## VPNGate 与相关项目

| 来源 | 本次核对结果 | 对本项目的意义 |
| --- | --- | --- |
| [VPNGate 官网](https://www.vpngate.net/en/) | 仍公布 SoftEther SSL-VPN、OpenVPN、L2TP/IPsec、MS-SSTP | 没有据此确认新增官方 WireGuard 服务；可用协议取决于志愿节点 |
| [OpenVPN 2.7.9](https://github.com/OpenVPN/openvpn/releases/tag/v2.7.9) | 2026-11-05 发布；Linux 可用时设置 IFF_BACKPRESSURE，另含内存、认证和平台修复 | 项目由 apt 安装 OpenVPN，实际版本与安全补丁由发行版提供；没有自动切换到上游二进制 |
| [SoftEther 开发分支 5.2.5189](https://github.com/SoftEtherVPN/SoftEtherVPN/releases/tag/5.2.5189) | 2026-11-05 发布；内存/并发修复、ARM64 支持、OQS 依赖与 OpenSSL 构建调整 | 不等同于 VPNGate 所有节点启用新技术，也不证明公共出口已支持后量子协商 |
| [SoftEther Stable](https://github.com/SoftEtherVPN/SoftEtherVPN_Stable/releases/tag/v4.41-9787-rtm) | 独立稳定仓库仍为 v4.41-9787-rtm | 与 5.x 开发仓库分开判断 |
| [fanout](https://github.com/byJoey/fanout/commit/d7ce5224caf3469b19876982abdf8d1be16c4a8b) | 最新提交仍与本项目来源记录一致，主要涉及 netns 线程污染修复 | 本项目通过独立进程进入 netns，并保留隔离验收 |
| [conduitvpn](https://github.com/sarices/conduitvpn/commit/4afc89bb9647fc1ccd8c240ab88e6e84e879398e) | 最新提交仍与来源记录一致，涉及 VPNGate 节点源和部署 | 继续采用受限 profile 校验与有界 CSV 解析 |

OpenVPN DCO 能否使用依赖内核、客户端/服务端和密码套件；只升级客户端版本不能保证 VPNGate
志愿节点支持。当前保持标准库 Go 管理器、独立 OpenVPN 后端与明确的 `tunnel`/`proxyAccess`
边界，后续后端替换无需改动订阅或 SOCKS 协议层。
