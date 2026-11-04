# sing-box-vpn

可在线更新的 sing-box 稳定版五协议普通节点，加一套可临时选择的 VPNGate 家宽候选出口。
管理、配置生成、证书、订阅、核心更新与网关统一使用 Go，Bash 保留安装和菜单入口。
构建工具链锁定 Go 1.27.3。支持 Ubuntu/Debian、systemd、Linux amd64/arm64。

```bash
curl -fsSL https://raw.githubusercontent.com/sck03/sing-box-vpn/main/install.sh | sudo bash
```

安装普通节点后运行 `sb`，菜单 **17** 选择国家并启动 VPNGate，菜单 **9** 分别选择
“普通五协议”或“VPNGate 五协议”，获取各自的 URI、二维码、Mihomo 和 sing-box 配置。
本地订阅在菜单 **3 → 8** 配置；两套内容使用同一个订阅服务的不同路径，无需增加常驻进程。
安装包包含静态管理/网关二进制；VPS 无需安装 Go 或 Python。

菜单采用单列分组与彩色状态区，直接显示核心版本、服务状态和五协议端口。
主菜单和 VPNGate 菜单均显示当前国家、实时隧道状态、出口 IP 与连接模式；
网关停止或状态不可用时不会将缓存的出口显示为已连接。
各级子菜单输入 **0** 返回上一级；填写参数时输入 **0** 取消，操作完成后按回车继续。
卸载输入 **y** 确认（不区分大小写），回车取消。普通安装仍一键生成五协议节点。
终端不支持颜色或设置 `NO_COLOR=1` 时使用纯文本。`sb status` 保留 JSON 输出，
菜单使用中文摘要；最新稳定核心在菜单 **8 → 1** 查询。

| 操作 | 菜单 |
| --- | --- |
| 整套卸载（输入 y 确认，回车取消） | 2 |
| 普通五协议凭据、Reality SNI、VMess WS/CDN、Argo 等 | 3 |
| 普通五协议端口、Hy2 跳跃 | 4 |
| 普通节点域名分流 / 证书 | 5 / 12 |
| 普通与 VPNGate 分别生成、查看、推送 | 9 |
| 仅卸载 VPNGate，保留普通节点 | 17 → 12 |
| VPNGate 节点与订阅 | 17 → 13 |
| VPNGate 五协议端口、凭据、SNI、WS、统一 TLS 证书 | 17 → 14 |

两套导出不混合，VPNGate 只包含当前国家的五个节点：

| 内容 | 普通节点 | VPNGate 节点 |
| --- | --- | --- |
| 文件目录 | `/etc/s-box/` | `/etc/s-box/vpngate/` |
| URI / 聚合 | `jhsub.txt` / `jhsub-base64.txt` | 同名文件，位于 VPNGate 目录 |
| 客户端配置 | `sbox.json` / `clmi.yaml` | 同名文件，位于 VPNGate 目录 |
| HTTP 订阅路径 | `/<令牌>/sbox.json` 等 | `/<令牌>/vpngate/sbox.json` 等 |

两套 HTTP 路径使用同一个订阅令牌。原有订阅地址继续提供普通节点；VPNGate 请使用新地址。
也可运行 `sb links normal`、`sb links vpngate`、`sb export normal`、`sb export vpngate`。
Telegram 按所选类型推送；GitLab 的 VPNGate 文件放入 `vpngate/`，不会覆盖普通订阅。

安装默认获取官方最新稳定核心。菜单 **8** 可查询版本、更新最新稳定版、指定稳定版本
和回滚上一核心，也可以使用：

```bash
sb core-check
sb core-update
sb core-update 1.14.4
sb core-rollback
```

最低支持 1.14.0，1.14.4 是当前测试基线，不是版本锁定。更新先校验官方 SHA256，
再用候选核心检查普通节点、VPNGate 和客户端配置，最后替换并重启原先运行的实例。
配置不兼容时不替换，启动失败时自动恢复原核心。端口、UUID、密码和分享链接不改变。

| 客户端选择 | 出口 |
| --- | --- |
| VLESS / VMess / Hy2 / TUIC / AnyTLS 普通节点 | 原有 VPS 出口与域名分流 |
| 对应的 VPNGate 节点 | VPS → 本机认证 SOCKS5 → 独立 netns → OpenVPN → 指定国家 |

普通节点与 VPNGate 节点使用独立端口和凭据。五种 VPNGate 协议共用一个出口；
换 VPNGate IP 时分享链接不变。普通订阅保留自动测速与原有分流；VPNGate 订阅手动选择协议，
公网流量均经过已验证的 VPNGate 出口，不包含普通节点或公网直连回退。
当前只监听一个国家的五个 VPNGate 入口。再次启用以前的国家会恢复它的端口和凭据；
切换国家后，旧国家入口关闭，旧链接暂时不可用。

程序不会为了节省资源定时断开 TCP 或轮换出口。UDP 收发任一方向有数据都会续期，
仅在双方均空闲约 60 秒后回收。健康检查复用现有隧道；修改 VPNGate 入口设置只重启对应
sing-box 服务，导出订阅不会重启服务。真实隧道故障或出口校验失败仍会阻断并尝试恢复。

VPNGate 是志愿者网络。“家宽候选”采用已知自营机房排除和出口运营商信息筛选，
并非住宅网络认证。节点可能掉线、满员或变更 IP；固定节点模式也无法保证永久固定 IP。

VPNGate 要求 root、可创建 network namespace、可用 `/dev/net/tun` 和 IPv4 外网。
创建隧道前安装断线阻断规则，业务流量只能经过 `tun0`。仅 OpenVPN 的标记连接
可经 veth 访问已经校验的远端 IP/端口。目标域名由命名空间中的工作进程解析，
IPv6 目标拒绝转发。出口国家未知、不匹配或校验失败时不会转发业务流量。
Geo/ASN 结果按数据源和 IP 缓存 15 分钟，最多保存 128 项。健康探测默认每 10 秒进行。
探测连接按隧道复用，切换时关闭；节点列表缓存一分钟，手动刷新可立即重新下载。

```bash
yg-vpngate ctl status
yg-vpngate ctl nodes
yg-vpngate ctl swap
yg-vpngate ctl mode --mode fixed
yg-vpngate ctl mode --mode sticky
yg-vpngate ctl stop
yg-vpngate ctl start
```

进程控制接口为 `/run/yg-vpngate/control.sock`，仅 root 可读写；SOCKS5 TCP 与 UDP
中继只监听回环地址。高级设置在 `/etc/s-box/vpngate/gateway.json`，修改后执行
`yg-vpngate ctl reload`。镜像源可通过菜单 17 配置；默认使用 VPNGate 官方 HTTPS API。
默认排除已知 hosting；可开启 `require_isp` 或配置 `allow_asns`，信息不满足要求时阻断。
默认地理信息源为 `api.ipapi.is`，可配置其他兼容的 JSON 数据源。

```bash
yg-vpngate ctl policy --require-isp=true --exclude-hosting=true
yg-vpngate ctl policy --asns 12345,67890
```

配置、密钥、令牌和节点缓存均保存在服务器本地，权限 0600。节点源提供的 OpenVPN
配置必须通过白名单校验，脚本、插件、管理端点、任意文件引用和不同 remote 均拒绝。
后台服务为 `sing-box`、`yg-vpngate`、`sing-box-vpngate`，日志通过 `journalctl -u` 查看。

已有 sing-box-yg 的 `/etc/s-box/sb.json` 会先备份为 `migration-backup.json`，再迁移到
单一现代模板；保留已有入口的端口、用户凭据、Reality 密钥与证书路径。
旧内核、旧 geosite/geoip 数据库分支、旧配置修改方式不再维护。
证书可通过菜单 12 导入或使用 Certbot；普通 VMess 支持 Argo/CDN，VPNGate VMess
首版使用独立 WS 入口。Telegram 和 GitLab 只在明确选择推送时使用，GitLab 仅允许私有项目。

本地订阅使用 Go HTTP 服务，限制 32 个连接并设置请求超时，流式读取文件。
令牌相当于访问密码。需要加密传输时，在订阅端口前配置 HTTPS 反向代理。
自签证书的原生 URI/Mihomo 使用对应客户端的自签模式，sing-box JSON 使用公钥指纹。
其他客户端的 UDP 与协议支持应按其版本确认。

开发与验证：

```bash
SING_BOX_BINARY=/path/to/sing-box RUN_UDP_IDLE_TESTS=1 go test -race -count=1 ./...
go vet ./...
go build -trimpath -o .build/yg-vpngate ./addons/vpngate/gateway
sudo env GATEWAY_BINARY="$PWD/.build/yg-vpngate" RUN_NETNS_TESTS=1 \
  "$(command -v go)" test -run '^TestLinuxNamespaceIsolation$' -count=1 -v ./addons/vpngate/gateway
go run ./cmd/release
```

CI 验证两套配置生成、旧参数迁移、稳定链接、认证、五协议真实 TCP/UDP、65 秒单向 UDP
传输不被空闲回收，以及隔离环境的
OpenVPN 握手、隧道 DNS 和断线行为。带 `v` 的发行标签在全部检查通过后发布源码与
静态二进制安装包及 SHA256。详见 [实施方案](VPNGate-实施方案.md) 和
[Go 迁移与性能分析](docs/optimization.md)。源码本地安装须先构建并解压发行包，再运行包内
`sudo bash install.sh --local`。v1.2.0 起全部项目程序与验收均使用 Go；升级会替换旧订阅服务入口。

基于 [sing-box-yg](https://github.com/yonggekkk/sing-box-yg)、
[fanout](https://github.com/byJoey/fanout) 与
[ConduitVPN](https://github.com/sarices/conduitvpn) 的代码与设计改造。
许可证 GPL-3.0，第三方来源与 MIT 声明见 [NOTICE.md](NOTICE.md)。
