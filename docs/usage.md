# 使用指南

[返回项目首页](../README.md) · [兼容性说明](compatibility.md)

候选节点源的 16 MiB 上限只限制一次下载的节点列表与配置文件，不限制代理连接的累计流量、
下载文件大小或视频传输。TCP/UDP 数据转发与节点列表刷新使用独立模块。
VPNGate 的单个 UDP 回复负载最多 65,497 字节，为 SOCKS 地址头保留 10 字节；更大的回复
只丢弃该包，后续包继续使用原目标会话。这是单包封装边界，不是累计流量限制。

## 凭据与入口设置

普通节点在菜单 **3** 设置，VPNGate 在 **17 → 14** 设置，两类节点的修改与订阅独立。

自定义凭据分别位于 **3 → 2 → 1** 和 **17 → 14 → 2 → 1**：

| 协议 | 自定义内容 |
| --- | --- |
| VLESS Reality / VMess WS | 各自的标准 UUID |
| Hysteria-2 / AnyTLS | 各自的密码 |
| TUIC-v5 | UUID 和密码，任一项留空保留该项，两项留空取消 |
| 全部协议 | VLESS、VMess、TUIC 的统一 UUID，以及 Hy2、TUIC、AnyTLS 的统一密码；密码留空使用该 UUID |

UUID 使用 `xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx` 格式；密码为 1–1024 个 UTF-8 字节，支持
中文、空格和特殊符号，不接受 NUL 或换行。输入隐藏，通过标准输入传给管理程序，不放入进程参数。
VLESS、VMess、Hy2、AnyTLS 的自定义值留空时取消；需要随机生成时选择 **3 → 2 → 2**。
填写参数时 `0` 表示取消。修改后从菜单 9 获取新链接，服务端、原生客户端配置和订阅同步更新。

普通 VMess 支持 WS/CDN 和 Argo；VPNGate VMess 使用独立 WS 入口。
WS 路径以 `/` 开头，最多 255 个 UTF-8 字节，例如 `/ws-01`、`/节点/连接`。
填写原始路径，不填写完整 URL、`?ed=2048` 等查询参数、`#` 片段或 `%2F` 等转义；
空白、控制字符和反斜杠也会被拒绝。错误输入不会覆盖现有配置或订阅。

## 端口、跳跃与复用

| 操作 | 普通节点 | VPNGate |
| --- | --- | --- |
| 修改主端口 | 4 → 1 | 17 → 14 → 1 |
| Hy2 跳跃 / TUIC 多端口 | 4 | 17 → 14 → 7 |
| 设置 / 清除 TUIC 多端口 | 4 → 4 / 4 → 5 | 17 → 14 → 7 → 4 / 5 |
| 生成指定 TUIC 端口链接 | 4 → 6 | 17 → 14 → 7 → 6 |

范围输入支持 `31000:31100 31200`，本次输入替换旧设置；程序合并重复或相邻范围，并拒绝
与其他节点、历史国家、代理、订阅及系统占用冲突。TUIC 的额外 UDP 端口转发到主端口，
使用同一组凭据；主端口变更保留复用范围。修改复用范围不重启服务。
这是同一个 sing-box 进程、一个 TUIC 监听入口的 UDP 转发，不为每个端口新增入口或进程。
TUIC 客户端使用其中一个端口；它没有 Hy2 的自动端口跳跃参数。云平台安全组也需放行这些 UDP 端口。
只改 TUIC 复用范围时，程序只更新端口元数据和防火墙，不重新生成未变化的订阅或调用核心校验。

菜单仍缺少这些选项时，先通过菜单 **7** 更新正式包至 **v2.1.0 或更新版本**，退出后重新运行 `sb`。
源码分支与正式 Release 分开发布，单独复制 `sb.sh` 会造成菜单与管理程序不匹配，请使用完整安装包。

自动分配使用 20000–59999，跳过已有节点和预留范围；若提示 200 个未预留端口均不可用，
请检查系统占用或手动指定端口。系统端口清单无法完整读取时会报错并保留原配置。

默认订阅包含五协议主端口，Hy2 导出跳跃参数；TUIC 可指定主端口或已配置的复用端口：

```bash
sb tuic-link 31200
sb tuic-link 31200 vpngate
```

## 分流与证书

菜单 **5** 配置普通节点的 SOCKS5、WARP 或域名解析优先级。IPv4/IPv6 是域名解析优先级，
保留可用地址回退；客户端直接传来的 IP 保持原值。现代规则支持情况见[兼容性说明](compatibility.md)。

菜单 **12** 导入 PEM 证书/私钥或使用 Certbot；VPNGate 统一 TLS 证书位于 **17 → 14 → 5**。
导入文件必须存在、密钥匹配，证书须覆盖填写的域名且在有效期内。VLESS 继续使用 Reality。
自签证书的 sing-box 配置使用公钥指纹，Mihomo 配置明确启用 `skip-cert-verify`。
Hysteria2 分享链接同时携带 `insecure=1` 和叶证书 SHA256 指纹；CA 证书使用系统信任校验，
不固定叶证书，避免正常续期后旧链接失效。更换自签证书后须重新获取订阅。

## 节点与订阅

菜单 **9** 先选择普通节点或 VPNGate，再查看 URI、二维码、Mihomo 和 sing-box 配置，
或主动推送到 Telegram / 私有 GitLab。VPNGate 只导出当前国家的五个节点。

| 内容 | 普通节点 | VPNGate |
| --- | --- | --- |
| 导出目录 | `/etc/s-box/` | `/etc/s-box/vpngate/` |
| 客户端配置 | `sbox.json` / `clmi.yaml` | 同名文件 |
| OpenClash/Mihomo 节点 provider | `proxies.yaml` | 同名文件，只含当前国家节点 |
| URI / Base64 聚合 | `jhsub.txt` / `jhsub-base64.txt` | 同名文件 |
| HTTP 订阅 | `/<令牌>/sbox.json` 等 | `/<令牌>/vpngate/sbox.json` 等 |

本地订阅在 **3 → 8** 启用或重置，两套地址使用同一令牌；重置令牌后需更新客户端订阅地址。
HTTP 的 `jhsub.txt` 路径提供 Base64 聚合，`links.txt` 提供原始 URI。公网加密传输需 HTTPS 反向代理。
默认通配监听在系统支持时同时接受 IPv4/IPv6；生成的 IPv6 URL 自动使用方括号。
文件缺失、为空或超过 16 MiB 时返回 503，避免把无效内容当作成功订阅；此上限不限制代理流量。
GitLab 的 VPNGate 文件放在 `vpngate/`，普通订阅继续使用原路径。
推送使用开始时的一份订阅快照；期间修改配置不会混入本次内容，需再次推送才能更新。
Telegram 保留完整链接，按每条消息最多 3500 字节分段；空订阅或单个链接过长时会在发送前
报错。若发送多条消息途中网络失败，已发送部分会保留，重试可能产生重复消息。

推荐使用客户端原生配置：sing-box 导入 `sbox.json`，Mihomo 导入 `clmi.yaml`。
聚合 URI 不是完整客户端配置，导入器对证书选项的支持存在差异：VMess JSON 使用当前
v2rayN 的 `insecure` 字段，TUIC 使用 `allow_insecure`。Mihomo 1.19.33 的 URI 导入器
忽略这两个字段，因此自签 TLS 的 VMess/TUIC 请直接使用 `clmi.yaml`；不要仅因链接能
导入就判定能连接。默认未启用 TLS 的 VMess 不受此证书限制，Argo 使用公网 CA 证书。

```bash
sb links normal
sb links vpngate
sb export normal
sb export vpngate
```

### OpenClash

使用当前 OpenClash 的 **Mihomo（Meta）核心**。菜单 **9 → 选择节点类型 → 2** 显示文件与
HTTP 地址，普通和 VPNGate 订阅分开使用。两种导入方式：

- 新建完整客户端配置：在 OpenClash 配置订阅中添加 `clmi.yaml` 地址，或上传这个文件。
  使用原生配置，无需第三方订阅转换；OpenClash 负责路由器监听、DNS 和透明代理设置。
- 保留现有路由器配置：将 `proxies.yaml` 地址配置为 `proxy-providers`，再由现有策略组引用。
  文件只包含 `proxies`，不会带入 DNS、TUN、端口或分流规则；它不能单独作为完整配置启动。

例如将 VPNGate 的节点 provider 合并到现有 Mihomo 配置中，替换下方 URL 为菜单显示的实际地址：

```yaml
proxy-providers:
  VPNGate:
    type: http
    url: "http://服务器:订阅端口/令牌/vpngate/proxies.yaml"
    path: ./proxy_provider/vpngate.yaml
    interval: 3600
    proxy: DIRECT
    health-check:
      enable: false
proxy-groups:
  - name: VPNGate出口
    type: select
    use: [VPNGate]
```

`proxy: DIRECT` 只指定订阅下载路径，避免依赖尚未加载的节点。业务分流由路由器配置决定：
将指定设备或域名指向 `VPNGate出口`；如需全部公网流量使用该出口，将最终规则设为
`MATCH,VPNGate出口`，并检查之前的 DIRECT 规则及 OpenClash 覆写。此组不要加入 DIRECT、
普通节点或自动回退组。暂停 VPNGate 时节点文件仍保留，但公网转发被服务端阻断。

普通节点使用不带 `/vpngate/` 的 `proxies.yaml` 地址；两种 provider 使用不同的名称和缓存路径。
这两个 YAML 文件采用 JSON 语法（属于 YAML 可接受的格式），Mihomo 可直接读取，无需改格式。
自签 VMess/TUIC 请使用原生文件，避免聚合 URI 丢失证书设置。当前验收覆盖 Mihomo HTTP provider
导入五协议的真实 TCP/UDP；OpenWrt 设备和 LuCI 覆写界面需在实际路由器上验证。

## 核心更新与配置恢复

菜单 **8** 查询、更新或回滚核心。最低支持 1.14.0，1.14.4 为测试基线；安装和更新默认采用
官方最新稳定版。更新校验 SHA256 和两套配置，启动失败时恢复原核心，节点凭据与链接保留。
校验期间配置新增、删除或内容变化会终止更新，待配置修改完成后重试即可。
无法查询 systemd 服务状态时也会中止更新，保留当前核心与上一核心；按错误信息修复总线或
权限问题后再重试。服务明确未运行或不存在时可正常更新，不会主动启动已停止的服务。
服务状态查询超时会报错；激活前取消不会替换当前核心和上一版本。
下载会完整检查压缩包和解压数据；损坏、超限或取消时保留已有候选文件。可在网络恢复后重试。

```bash
sb core-check
sb core-update
sb core-update 1.14.4
sb core-rollback
sb recover
```

`sb recover` 恢复未完成的配置提交。每次修改只保留一份修改前的临时快照，提交或恢复成功后
清理，不累积历史配置。恢复记录只存文件列表与校验值；备份缺失或损坏时保留快照并报错。
当前版本面向新部署，不导入旧 sing-box-yg/Python 配置。首次安装使用空配置目录；已有当前
版本的 `app.json` 可重复安装并保留设置。只有 `sb.json` 而缺少 `app.json` 时拒绝覆盖，请恢复
完整当前配置或另用空目录。安装后的另一架构二进制移入 `/etc/s-box/待手动删除/架构-XXXXXXXX/`，
附原始路径清单，供手动删除。

## VPNGate 控制与排查

菜单 **17** 管理国家、候选节点、固定/sticky 模式及暂停；**17 → 12** 仅卸载 VPNGate。
固定模式只重连选定节点，sticky 优先保持原节点；节点故障可能使恢复延长至数分钟。
换出口 IP 保持分享链接，切换国家会关闭旧国家入口，已有客户端连接需重新建立。

```bash
yg-vpngate ctl status
yg-vpngate ctl nodes
yg-vpngate ctl swap
yg-vpngate ctl mode --mode fixed
yg-vpngate ctl mode --mode sticky
yg-vpngate ctl stop
yg-vpngate ctl start
```

高级设置位于 `/etc/s-box/vpngate/gateway.json`，修改后执行 `yg-vpngate ctl reload`。
`max_connections` 默认 128，范围 1–1024，统计代理套接字及正在建立的目标连接，并非客户端人数。
达到上限会拒绝新连接或新 UDP 目标；降低并发或按 VPS 资源调整该值后重试。
UDP 关联空闲 60 秒后回收；合法单向上传或下载可续期，无效包、分片、零目标端口和其他
源端口的流量不延长空闲期限。退出会等待控制连接读取及目标转发完成回收。
菜单 17 可配置镜像源和 ISP/hosting 筛选；默认使用官方 HTTPS 节点 API。
ASN 允许列表填写 1–4294967295 的整数。自定义 Geo 服务的 ASN 也须返回此范围的 JSON
十进制整数，不接受小数、指数、字符串或 null；`hosting` / `is_datacenter` 使用布尔值。
已知字段类型错误时拒绝本次 Geo 结果，修正后重新验证。出口与 VPS 地址相同、ASN 无效或
不符合筛选策略时不会开放转发；IP 的等价文本写法不会改变出口比较结果。
修改镜像源后，旧来源候选、上次节点和失败冷却立即失效；新来源不可用时等待恢复。
重启也只加载来源一致的缓存。旧开发版未记录来源的缓存会重新获取，无需手动迁移。
同一来源返回断流、超限或引号结构损坏的 CSV 时，保留上次有效候选；缓存节点连接前仍校验出口。
更多隔离、校验与资源限制见[实施方案](../VPNGate-实施方案.md)和[架构说明](architecture.md)。
`ctl` 的完整响应上限为 1 MiB；超限或断流会报错，不输出截断内容。控制接口返回重定向或
其他非 2xx 状态也按失败处理，缺少具体错误说明时显示 HTTP 状态码。

启动恢复与 `yg-vpngate cleanup` 都会校验数据目录中的 `namespace-owner.json`。记录不可读、
缺字段或子网不合法时保留原文件并停止资源操作；检查文件权限及记录是否损坏后重试，
不要直接删除记录来跳过检查，以免遗失已有命名空间和防火墙规则的清理依据。

启动前还会查询系统命名空间、网卡和防火墙清单；权限不足、超时或输出超限会停止启动，
不会把查询失败视为资源不存在。同名资源没有本项目所有权时拒绝接管。清理后的查询失败
会保留所有权供重试；请先排查 `ip`、`iptables` 的报错，再重新执行清理或启动。

菜单 **10** 查看运行日志，也可执行：

```bash
sb status
yg-vpngate ctl status
journalctl -u sing-box -u yg-vpngate -u sing-box-vpngate -n 100 --no-pager
```

OpenVPN 握手日志位于 `/var/lib/yg-vpngate/openvpn.log`，每次连接尝试重建，最多保存前
2 MiB 原始输出。达到上限或日志写入失败后仍继续读取进程输出、检测就绪，不因日志停止而卡住连接。

已启用服务在开机启动时恢复配置和防火墙；主动停止或暂停会保留。VPNGate 为志愿者网络，
家宽筛选不代表住宅接入认证，节点在线和固定 IP 无法保证；未经出口校验不会转发。
