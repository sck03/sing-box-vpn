# sing-box-vpn

基于 sing-box 的五协议节点管理程序，支持 VLESS Reality、VMess WS、Hysteria-2、TUIC-v5 和 AnyTLS。
普通节点使用 VPS 出口，可选 VPNGate 提供指定国家的家宽候选出口。

支持五协议自定义凭据、域名分流、Argo/CDN、TLS 证书、Hy2 跳跃、TUIC 多端口和订阅导出。
支持使用 Mihomo 核心的 OpenClash：可导入完整配置，或只更新节点、保留路由器自己的分流。
订阅支持本机、手工及远端来源、集合处理、客户端模板、独立分享和加密备份。
可按需启动轻量 Web 管理和协议连接检测，支持定时刷新、规则资源、诊断通知与多目的地分发。

## 安装与使用

支持 **Ubuntu/Debian + systemd、Linux amd64/arm64**，安装需要 root 权限。
使用预编译程序，VPS 无需安装 Go 或 Python；VPNGate 另外需要 IPv4 外网、TUN 和 network namespace。

```bash
curl -fsSL https://raw.githubusercontent.com/sck03/sing-box-vpn/main/install.sh | sudo bash
```

安装后运行 `sb` 打开菜单：

1. 菜单 **9** 查看普通五协议节点、二维码和客户端配置。
2. 需要 VPNGate 时，菜单 **17** 选择国家并启动出口，再从菜单 **9** 选择 VPNGate 节点。
3. 菜单 **3 → 8** 管理订阅、来源、集合、模板、独立分享、刷新记录、备份和回收站。
4. 菜单 **3 → 2 → 1** 自定义五协议 UUID / 密码，TUIC 的 UUID 和密码可分别修改。
5. 菜单 **4 → 4** 设置 TUIC 多端口，**4 → 6** 获取指定端口链接。

**v3 使用新的管理数据结构和订阅接口，面向空目录的新部署，不迁移 v2 或更早的数据。**
安装器下载最新正式 Release；源码与安装包分别发布，部署本分支请先构建完整安装包。

推荐导入对应核心的 `sbox.json` 或 `clmi.yaml`。聚合 URI 的证书选项支持因客户端而异，
Mihomo 的 VMess/TUIC 自签 TLS 节点请使用 `clmi.yaml`，详见[订阅说明](docs/usage.md#节点与订阅)。
OpenClash 的 `proxy-providers` 使用 `proxies.yaml`，见[OpenClash 设置](docs/usage.md#openclash)。

## 常用菜单

| 操作 | 菜单 |
| --- | --- |
| 安装；整套卸载 | 1；2 |
| 凭据、Reality SNI、VMess WS/CDN、Argo、订阅 | 3 |
| 主端口、Hy2 跳跃、TUIC 多端口 | 4 |
| 普通节点域名分流；TLS 证书 | 5；12 |
| 服务启动 / 停止 / 重启；核心更新 / 回滚 | 6；8 |
| 更新项目程序；查看节点 / 订阅；日志 | 7；9；10 |
| VPNGate 国家、出口、独立入口设置 | 17 |

子菜单输入 **0** 返回，填写参数时输入 **0** 取消；卸载需输入 **y** 确认。

## 使用须知

- 普通节点与 VPNGate 使用独立端口、凭据和订阅。VPNGate 同时激活一个国家，切回已保存国家可恢复原链接。
- VPNGate 是志愿者网络，家宽候选属于筛选结果；节点在线和固定 IP 无法保证。出口校验失败时阻断转发。
- 已启用服务支持开机恢复，连通时间取决于网络、握手和出口校验。主动停止与 VPNGate 暂停设置会保留。
- 本地订阅令牌相当于访问密码，公网加密传输可配置 HTTPS 反向代理。

安装与核心更新默认获取官方最新稳定版，最低支持 **1.14.0**。具体兼容性见[兼容性说明](docs/compatibility.md)。

## 文档

- [使用指南](docs/usage.md)：凭据、端口、证书、两套订阅、出口控制与故障排查。
- [开发与发布](docs/development.md)：构建、测试、GitHub Actions 和 Release。
- [订阅配置与接口](docs/subscriptions.md)：配置示例、客户端能力、授权、调度与恢复。
- [v3 验收记录](docs/refactor-status.md)：交付范围、回归结果、持续测试和验证边界。
- [架构与维护](docs/architecture.md)：模块边界、资源限制和扩展约定。
- [上游技术与协议](docs/upstream.md)：sing-box 新协议、OpenClash 和 VPNGate 更新及项目接入范围。
- [实施方案](VPNGate-实施方案.md) · [性能与优化记录](docs/optimization.md)。

许可证 GPL-3.0，第三方来源与授权见 [NOTICE.md](NOTICE.md)。
