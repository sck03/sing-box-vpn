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

VPNGate 网关源码在本项目的 `addons/vpngate/gateway/`，管理源码在 `internal/manage/`，
安装包也包含这些源码，并直接运行本项目构建的 `yg-vpngate`。安装流程从本项目的
GitHub Release 获取程序；fanout 和 conduitvpn 是代码设计的参考来源，见 [NOTICE.md](NOTICE.md)，
安装时不会克隆或安装这两个项目。OpenVPN 由 Ubuntu/Debian 的 apt 包安装，节点的
OpenVPN 配置从 VPNGate API 或配置的镜像下载。

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
| 普通五协议端口、Hy2 跳跃、TUIC 多端口 | 4 |
| 普通节点域名分流 / 证书 | 5 / 12 |
| 普通与 VPNGate 分别生成、查看、推送 | 9 |
| 仅卸载 VPNGate，保留普通节点 | 17 → 12 |
| VPNGate 节点与订阅 | 17 → 13 |
| VPNGate 五协议端口、凭据、SNI、WS、统一 TLS 证书 | 17 → 14 |
| VPNGate 的 Hy2 跳跃 / TUIC 多端口 | 17 → 14 → 7 |

**域名分流继续支持 sing-box 1.14。** 菜单 5 使用现代 `domain_suffix` 和路由动作，
客户端的中国规则使用 SRS 规则集。被移除的旧 DNS 格式和 geosite/geoip 数据库不影响这套功能。
IPv4 / IPv6 选项表示目标域名的**解析优先级**，保留可用地址回退；客户端直接传来的 IP 保持原值。
SOCKS5、WARP 和后缀域名匹配继续保留，域名输入会规范大小写、去重并校验每个 DNS 标签。

自定义凭据在 **3 → 2 → 1**，VPNGate 使用 **17 → 14 → 2 → 1**。
选择“全部协议”后填写一个标准 UUID，会同时用于 VLESS、VMess、TUIC 的 UUID 和
Hy2、TUIC、AnyTLS 的密码；也可以单独修改一个协议，TUIC 可另填独立密码。
输入回车或选择“随机重置”仍可自动生成凭据。修改只作用于所选的普通节点或当前 VPNGate 国家，
随后同步服务配置和订阅；Reality 密钥、WS 路径及网关内部认证保持原值。

TUIC 多端口在普通菜单 **4 → 4**，或 VPNGate 菜单 **17 → 14 → 7 → 4**。
支持多个单端口、范围及其组合，例如 `31000:31100 31200`；本次输入替换旧设置，菜单 **5** 清空。
与原版相同，额外 UDP 端口转发到主端口，各端口可同时使用同一组凭据；IPv4/IPv6 入口分别配置规则。
程序合并重复、重叠和连续范围，拒绝与其他节点、历史国家、订阅及已占用端口冲突的设置。
更换主端口会保留复用范围并更新转发目标；修改复用范围无需重启服务。

默认订阅仍保留五种协议的主端口。TUIC 客户端使用一个指定端口，菜单 **4 → 6** 可生成
某个主端口或复用端口的专用 URI，也可运行 `sb tuic-link 31200` 或
`sb tuic-link 31200 vpngate`。Hy2 的自动端口跳跃参数继续正常导出。

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
核心更新只在校验与替换文件时占用配置锁，服务启动恢复可以正常执行；自动回滚会等待
并发的启动准备结束。菜单和后台控制修改网关设置时共同加锁，按最新保存的配置合并修改。

配置提交前保存私有恢复记录。进程在多文件替换期间中断后，服务启动前或下一次管理操作
会恢复提交前的配置与订阅；也可运行 `sb recover`。并发服务启动会等待正在进行的配置操作，
损坏的恢复记录会保留并报错。明确确认的整套卸载仍可清理损坏的配置。

**服务器重启后会自动恢复已启用的服务，但不能保证立即连通。** 普通节点、VPNGate、
本地订阅和 Argo 在启动前共同恢复未完成配置及 IPv4/IPv6 防火墙规则，包括 Hy2 跳跃和
TUIC 多端口转发。意外退出（含正常退出码）后间隔 3 秒重试，不因启动次数限制停止恢复；
主动 `systemctl stop` 仍保持停止，`yg-vpngate ctl stop` 的暂停设置也会保存。
安装升级会同步更新已有 VPNGate 服务单元，并为已有 Argo 单元补上恢复策略。

网关先尝试一个本地保存的同国候选，同时刷新节点源；单次刷新含等待最多 30 秒。
保存节点仍须通过实际出口校验。启动时临时探测故障也会在同一隧道上重试两次。
恢复耗时取决于开机网络准备、OpenVPN 握手（默认每个候选最多 60 秒）和出口校验；
候选故障时可能重试数分钟，固定节点离线时持续等待。服务器重启前的连接需由客户端重新建立。

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
重复启动、重载或提交相同网关设置会保持当前隧道。健康探测短暂失败时先阻断转发，
在原隧道上间隔 2 秒重试两次，校验成功后恢复接收新请求；错误国家或违反筛选策略直接拒绝。
固定 / sticky 模式重连时优先使用节点源更新后的同名节点配置，保持选定节点身份。
两套客户端配置的 AnyTLS 在空闲超时后保留一个会话，继续按需建立并发会话，减少空闲连接占用。

网关连接域名时，在同一 20 秒预算内尝试可用的公网 IPv4，避免首个地址失效导致整个连接失败。
UDP 帧复用现有缓冲，头部与数据一次写入；收发均有超时保护，持续单向传输仍可续期。
同一目标的地址头按会话缓存，减少每包解析与分配；不同地址表示仍保持同一 IPv4 会话的源端口。
网关异常退出后会回收其命名空间中的残留进程；清理失败保留所有权记录，下次可继续恢复。
并发控制操作会保留各自修改的设置；核心回滚后的服务恢复失败和被重定向的远端推送均明确报错。

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
安装只复制本机架构的管理二进制。升级成功后，另一架构的旧二进制、旧 Python 管理模块
和 `sb10.json` / `sb11.json` 会移入
`/etc/s-box/待手动删除/迁移-XXXXXXXX/`，附原始路径清单，供你确认后手动删除。
证书可通过菜单 12 导入或使用 Certbot；普通 VMess 支持 Argo/CDN，VPNGate VMess
首版使用独立 WS 入口。Telegram 和 GitLab 只在明确选择推送时使用，GitLab 仅允许私有项目。

本地订阅使用 Go HTTP 服务，限制 32 个连接并设置请求超时，流式读取文件。
令牌相当于访问密码。需要加密传输时，在订阅端口前配置 HTTPS 反向代理。
自签证书的原生 URI/Mihomo 使用对应客户端的自签模式，sing-box JSON 使用公钥指纹。
导入证书须提供现有且匹配的 PEM 证书 / 私钥，证书应覆盖所填域名并在有效期内。
其他客户端的 UDP 与协议支持应按其版本确认。

开发与验证：

```bash
SING_BOX_BINARY=/path/to/sing-box RUN_UDP_IDLE_TESTS=1 go test -race -count=1 ./...
go vet ./...
go build -trimpath -o .build/yg-vpngate ./addons/vpngate/gateway
sudo env GATEWAY_BINARY="$PWD/.build/yg-vpngate" RUN_NETNS_TESTS=1 \
  "$(command -v go)" test -run '^TestLinuxNamespaceIsolation$' -count=1 -v ./addons/vpngate/gateway
sudo env RUN_FIREWALL_TESTS=1 "$(command -v go)" test \
  -run '^TestLinuxMultiportFirewall$' -count=1 -v -timeout=1m ./internal/manage
sudo env GATEWAY_BINARY="$PWD/.build/yg-vpngate" RUN_SERVICE_TESTS=1 "$(command -v go)" test \
  -run '^TestLinuxServiceStartupRecovery$' -count=1 -v -timeout=2m ./internal/manage
go run ./cmd/release
```

CI 验证两套配置生成、旧参数迁移、稳定链接、认证、五协议真实 TCP/UDP、65 秒单向 UDP
传输不被空闲回收，以及隔离环境的
OpenVPN 握手、隧道 DNS 和断线行为。普通分支提交和 PR 只验证；推送 `v主版本.次版本.补丁`
标签时，CI 使用标签版本构建，并在全部检查通过后发布源码与静态二进制安装包及 SHA256。

也可以在 GitHub 的 **Actions → CI → Run workflow** 中手动运行：

| 输入 | 用途 |
| --- | --- |
| `publish` | 默认关闭，只验证并上传构建产物；勾选后在验证通过时创建版本标签和 Release |
| `version` | 自定义稳定版本，例如 `v1.2.3` 或 `1.2.3`；留空自动递增 |
| `bump` | 自动递增 `patch`（默认）、`minor` 或 `major`；自定义版本时忽略 |
| `draft` | 勾选后将 Release 保存为草稿，之后可在 Releases 页面编辑并正式发布 |
| `notes` | 自定义发布说明；留空时由 GitHub 自动生成 |

自动版本取所有现有 `v主版本.次版本.补丁` 标签与 `versions.json.release` 中的最高版本，
再递增指定位置；首次发布没有版本标签时，以 `versions.json.release` 为基准。
例如基准 `v1.2.0`，三种递增结果分别为 `v1.2.1`、`v1.3.0` 和 `v2.0.0`。
手动运行时即使不勾选 `publish`，也会计算下一版本供预览，但不会创建标签或 Release。
自动版本会跳过预发布标签；自定义发布不允许使用已存在的标签，也不接受预发布版本号。
选定版本在构建前写入 CI 工作目录的 `versions.json`，保证程序、包内清单、包名和 Release
一致；不会自动提交或回写分支。手动发布的标签指向所选分支此次运行的提交。
发布任务串行运行，避免同时自动递增到同一版本；最终版本可在运行摘要中查看。

详见 [实施方案](VPNGate-实施方案.md) 和
[Go 迁移与性能分析](docs/optimization.md)。源码本地安装须先构建并解压发行包，再运行包内
`sudo bash install.sh --local`。v1.2.0 起全部项目程序与验收均使用 Go；升级会替换旧订阅服务入口。

基于 [sing-box-yg](https://github.com/yonggekkk/sing-box-yg)、
[fanout](https://github.com/byJoey/fanout) 与
[ConduitVPN](https://github.com/sarices/conduitvpn) 的代码与设计改造。
许可证 GPL-3.0，第三方来源与 MIT 声明见 [NOTICE.md](NOTICE.md)。
