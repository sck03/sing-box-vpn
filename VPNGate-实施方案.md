**VPNGate 实施方案与实现说明**

目标是保留普通五协议节点，再提供一套可临时选择的 VPNGate 家宽候选节点。
sing-box 核心支持在线更新至官方最新稳定版，最低 1.14.0，测试基线 1.14.4。
Go 构建工具链为 1.27.3；发行包包含 Linux amd64/arm64 静态网关。

普通节点与 VPNGate 节点各有 VLESS Reality、VMess-WS、Hysteria-2、TUIC-v5、AnyTLS。
普通入口保留自己的端口、凭据和路由。VPNGate 入口使用独立端口、密钥和密码，同国家
五种协议共用一个 OpenVPN 出口。切换 VPNGate IP 不改变入口链接。

```text
客户端 → VPNGate 专用五协议 sing-box 入口
       → 回环地址上的认证 SOCKS5
       → Unix socket
       → netns 内的独立工作进程
       → OpenVPN tun0 → 指定国家的 VPNGate 节点
```

出口采用进程隔离，避免宿主机进程调用 setns 引起线程或子进程进入错误命名空间。
netns 先设置 OUTPUT/INPUT 默认 DROP，仅允许 tun0 业务流量、回环通信，以及带专用
mark、远端 IP/端口明确匹配的 OpenVPN 外层连接。宿主机默认路由不改变；转发/NAT
规则限定到本项目 veth/subnet，停止后按所有权逐项清理。

工作进程显式使用隧道内 IPv4 DNS；IPv6 目标不转发。SOCKS5 支持 CONNECT、UDP
ASSOCIATE，TCP/UDP 中继只监听回环地址，必须认证。业务 socket 不具备 OpenVPN
外层 mark，因此隧道消失后不能经 veth 回落到 VPS。

国家代码持久化。节点源缺少国家、国家不符、无效 IP，或 profile 含脚本、插件、
任意文件引用、其他 remote 时拒绝。连上后查询实际出口 IP 的国家和运营商；
未知或不匹配时阻断。默认排除已知自营机房与 hosting，支持 ISP 严格模式和 ASN
允许列表。Geo/ASN 缓存 15 分钟，默认健康间隔 10 秒。

当前同时激活一个国家。各国家入口和凭据保留：韩国激活后，日本凭据不能使用韩国
出口；重新激活日本会恢复原日本链接。停止出口保留入口配置，节点不可用；再次启动
沿用原链接。普通节点独立于 VPNGate 的启停和国家选择。

家宽筛选是候选识别，不是住宅接入认证。固定模式只重连选定节点；动态 IP、节点
满员或下线仍可能导致不可用，严格模式不会因此跨国或回落到 VPS。

| 文件/服务 | 职责 |
| --- | --- |
| sb.sh | 五协议菜单、安装迁移、核心、路由/端口/证书/订阅 |
| lib/config.py、render.py、store.py | 按字段修改、校验后写入、客户端/URI 输出 |
| addons/vpngate/vpngate.sh | 菜单 17、国家入口、服务启停及清理 |
| addons/vpngate/gateway/ | 节点/profile、国家/ISP、代理、worker、namespace |
| sing-box.service | 普通五协议 |
| yg-vpngate.service | 出口管理器与认证 SOCKS5 |
| sing-box-vpngate.service | 独立五协议 VPNGate 入口 |
| /etc/s-box/vpngate/ | 入口设置、密钥、网关及服务端配置 |
| /var/lib/yg-vpngate/ | 缓存、profile、状态、限量日志 |
| /run/yg-vpngate/control.sock | 仅 root 的控制接口 |

菜单 9 输出 URI、二维码、聚合、Mihomo 与 sing-box JSON；刷新不重复追加。
临时组是手动组，不含 DIRECT，也不进入自动测速组。全局模式选该组可让全部代理
流量经过此出口；规则模式仍遵守国内直连规则。

只维护现代模板，旧内核、双模板和旧 geosite/geoip 数据库不再生成。旧配置先私有
备份，再迁移五协议字段，保留端口、用户和 Reality 密钥。修改不依赖行号或数组
位置；Hy2 单端口转换为等值范围。新配置先通过对应核心 check。

验收随代码交付：

- Go：危险 profile、国家/固定模式、ASN、内核规则、过期认证、TCP/UDP 和碎片。
- Python：五协议、迁移、凭据隔离、链接稳定、非法配置保护、跳跃与手动组。
- 实际协议：普通/VPNGate 五协议分别验证 TCP/UDP，共 20 条链路。
- Linux：受控 OpenVPN TLS、隧道 DNS、TCP/UDP、杀隧道后的已有/新连接阻断、
  停用状态重启恢复、错误出口国家拒绝。
- CI 通过后才发布两种架构与 SHA256；测试不冒充公共节点或特定 VPS 的在线结果。

生产要求 Ubuntu/Debian + systemd、root、IPv4、TUN/netns。稳定版下载须校验。
配置不兼容时保留旧核心，服务启动失败时自动回滚。
仓库不包含运行密码、私钥、节点缓存、临时工具链和研究源码副本。
来源见 [NOTICE.md](NOTICE.md)，使用见 [README.md](README.md)。
