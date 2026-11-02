# sing-box-vpn

可在线更新的 sing-box 稳定版五协议普通节点，加一套可临时选择的 VPNGate 家宽候选出口。
构建工具链锁定 Go 1.27.3。支持 Ubuntu/Debian、systemd、Linux amd64/arm64。

```bash
curl -fsSL https://raw.githubusercontent.com/sck03/sing-box-vpn/main/install.sh | sudo bash
```

安装普通节点后运行 `sb`，菜单 **17** 选择国家并启动 VPNGate，菜单 **9** 获取
URI、二维码、Mihomo 和 sing-box 配置。本地订阅在菜单 **3 → 8** 配置。
安装包包含静态网关二进制；VPS 无需安装 Go。

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
换 VPNGate IP 时分享链接不变。自动测速只包含普通节点，临时组只能手动选择。
当前同时激活一个国家。再次启用以前的国家会恢复它的端口和凭据；切换国家后，
旧国家凭据无法使用新国家出口，因此旧链接会不可用。

VPNGate 是志愿者网络。“家宽候选”采用已知自营机房排除和出口运营商信息筛选，
并非住宅网络认证。节点可能掉线、满员或变更 IP；固定节点模式也无法保证永久固定 IP。

VPNGate 要求 root、可创建 network namespace、可用 `/dev/net/tun` 和 IPv4 外网。
创建隧道前安装断线阻断规则，业务流量只能经过 `tun0`。仅 OpenVPN 的标记连接
可经 veth 访问已经校验的远端 IP/端口。目标域名由命名空间中的工作进程解析，
IPv6 目标拒绝转发。出口国家未知、不匹配或校验失败时不会转发业务流量。
Geo/ASN 结果按 IP 缓存 15 分钟，健康探测默认每 10 秒进行。

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

本地订阅令牌相当于访问密码。需要加密传输时，在订阅端口前配置 HTTPS 反向代理。
自签证书的原生 URI/Mihomo 使用对应客户端的自签模式，sing-box JSON 使用公钥指纹。
其他客户端的 UDP 与协议支持应按其版本确认。

开发与验证：

```bash
cd addons/vpngate/gateway
go test -race ./...
cd ../../..
SING_BOX_BINARY=/path/to/sing-box python3 -m unittest discover -s tests -v
SING_BOX_BINARY=/path/to/sing-box python3 tests/e2e_protocols.py
sudo GATEWAY_BINARY=/path/to/yg-vpngate python3 tests/integration_linux.py
python3 scripts/build_release.py
```

CI 验证配置生成、旧参数迁移、稳定链接、认证、五协议真实 TCP/UDP，以及隔离环境的
OpenVPN 握手、隧道 DNS 和断线行为。带 `v` 的发行标签在全部检查通过后发布源码与
静态二进制安装包及 SHA256。详见 [实施方案](VPNGate-实施方案.md)。

基于 [sing-box-yg](https://github.com/yonggekkk/sing-box-yg)、
[fanout](https://github.com/byJoey/fanout) 与
[ConduitVPN](https://github.com/sarices/conduitvpn) 的代码与设计改造。
许可证 GPL-3.0，第三方来源与 MIT 声明见 [NOTICE.md](NOTICE.md)。
