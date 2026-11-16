# 订阅配置与接口（v3）

[首页](../README.md) · [使用指南](usage.md) · [架构](architecture.md)

v3 面向新部署，使用 JSON schema 1，不读取旧数据库、不兼容旧下载 URL。
运行只需一个 Go 管理程序；不需要 Node.js、Python 或独立数据库。JSON 文件使用原子事务发布，
可直接备份和审查。普通五协议、Argo/CDN、Hy2 跳跃、TUIC 多端口、VPNGate 隔离继续保留。

## 菜单与命令

菜单 **3 → 8** 包含启停、默认地址、重置、配置预览/应用、远端刷新、分享、日志、备份、回收站、
客户端能力、Web 管理、连接检测和分发。菜单查看读取已发布内容，刷新必须明确选择或等待调度。

```bash
yg-vpngate subscriptions show
yg-vpngate subscriptions clients
yg-vpngate subscriptions preview --file /root/subscriptions.json
yg-vpngate subscriptions apply --file /root/subscriptions.json
yg-vpngate subscriptions refresh
yg-vpngate subscriptions logs
```

全局 `--root /etc/s-box --core /etc/s-box/sing-box` 放在操作名称之前。编辑从 `show` 导出的 JSON，
保留 `revision`，先预览再应用；版本冲突时重新载入。配置含上游 URL、节点凭据与分发密钥，应限制权限。
`grants` 由授权命令维护，应用配置文件不会修改它。新增的必需远端来源在提交前获取并验证，
失败不会覆盖现有快照。刷新期间网络请求不持有配置锁，提交前复核版本。

## 数据对象

| 对象 | 字段与用途 |
| --- | --- |
| `sources` | `id`、`name`、`kind`；本机 `local` 使用 `scope: normal/vpngate`；手工 `inline` 使用 `content`；远端 `remote` 使用 `url` |
| `collections` | `id`、`name`、`sources` 来源 ID 列表、`processor` 处理规则 |
| `templates` | `id`、`client`、`version: 1`、`mode`、`preset`，可选 DNS/TUN/测速与规则 |
| `rule_sets` | `id`、内置 `rules`，可选远端 `url` |
| `outputs` | `id`、`name`、`collection`、`template`，可选 `skip_unsupported` |
| `grants` | 独立分享的令牌摘要、输出范围、有效期、次数限制和撤销状态 |
| `deliveries` | 输出的 HTTP PUT、WebDAV 或私有 Gist 分发设置 |

默认有 `normal` 和 `vpngate` 两个本机来源、同名集合，以及每范围的五种输出。
保留默认输出 ID，可让菜单的本地文件导出继续对应；删除这些输出时，相应文件会被移除。
仅使用自定义输出时，监听仍可启用，使用 `grant` 明确授权输出；程序不会自动把自定义输出加入默认分享。

以下片段添加到从 `show` 导出的对应数组；不替换整份配置：

```json
{
  "source": {
    "id": "upstream", "name": "外部节点", "kind": "remote",
    "format": "mihomo", "url": "https://provider.example/subscription",
    "required": true, "interval_seconds": 3600, "max_stale_seconds": 86400
  },
  "collection": {
    "id": "phone", "name": "手机", "sources": ["normal", "upstream"],
    "processor": {"deduplicate": true, "sort": true, "exclude": "过期|剩余流量", "prefix": "手机-"}
  },
  "template": {
    "id": "phone-mihomo", "client": "mihomo", "version": 1,
    "mode": "config", "preset": "proxy", "dns": ["1.1.1.1", "8.8.8.8"],
    "probe_url": "https://www.gstatic.com/generate_204", "tun": false
  },
  "output": {"id": "phone", "name": "手机配置", "collection": "phone", "template": "phone-mihomo"}
}
```

导入格式为 `uri`、`base64`、`mihomo`（YAML/JSON）、`sing-box` 或类型化 `nodes` JSON。
不执行脚本，不接受 YAML alias、多文档或无法保留的连接字段。仅支持模型可表达的 TCP/WS、
TLS/Reality 和协议参数；例如混淆插件、MUX、链式拨号等会报错，不能静默丢弃。
远端不能将节点标为可信 VPNGate。本机节点 ID 固定；外部节点 ID 取决于来源及连接参数，改名不变，改凭据会变。

处理器还支持 `protocols`、`tags`、`include`、`rename_pattern`、`rename_replacement`。
预览显示输入/输出数量及筛选、重命名、去重、能力和 TLS 诊断。重名和策略组保留名称会阻止发布。

## 客户端、模板与规则

| `client` | `mode` |
| --- | --- |
| `sing-box` | `nodes`、`config` |
| `mihomo` | `nodes`、`provider`、`config` |
| `openclash`、`stash` | `provider`、`config` |
| `surge`、`surge-mac`、`loon`、`quantumult-x`、`surfboard`、`egern` | `nodes`、`config` |
| `shadowrocket` | `nodes`（Base64）、`rules`（独立规则，客户端另配策略组） |
| `uri`、`base64` | `nodes` |

协议子集与检查级别以 `clients` 为准。sing-box/Mihomo 有真实核心与 72 条协议 TCP/UDP 验收；
其余专有客户端目前完成生成结构测试与字段映射核对，未做真实设备导入/连接验收。
默认遇到不支持的节点阻止发布；`skip_unsupported: true` 才允许排除，并记录原因。
原生 sing-box/Mihomo 保留自签证书 pin。URI 和部分客户端存在 pin/证书参数限制，预览会提示。

`preset: proxy` 为全代理，`split` 为中国流量直连。VPNGate 集合拒绝 DIRECT 分流；
混合集合保留独立 VPNGate 策略组。`tun` 只用于 sing-box/Mihomo/OpenClash 完整配置；
`probe_url` 只用于 Mihomo/OpenClash/Stash 的普通节点自动测速。DNS 是解析器 IP，
sing-box 接受一个，Mihomo 可多个；节点/provider 模式拒绝 DNS、TUN 和测速参数。
`minimum_version` 是客户端版本说明元数据，服务端无法读取安装在用户设备上的版本。

规则格式如下，目标为 `PROXY`、`VPNGate`、`DIRECT`、`REJECT`，类型支持域名、域名后缀、IP CIDR：

```json
[{"type":"domain_suffix","value":"example.com","target":"PROXY"}]
```

规则集远端 URL 返回上述 JSON 数组；模板 URL 返回完整模板 JSON，必须保持原 ID/client。
它们每小时检查更新，使用 ETag、失败退避和最近有效版本；无效资源不会覆盖正在使用的配置。

## 分享与 HTTP 下载

```bash
yg-vpngate config subscription-init --listen 127.0.0.1 --port 2080 --public-base-url https://sub.example.com/path --apply-services
yg-vpngate subscriptions grant --id phone --outputs phone --expires 720h --max-downloads 100
yg-vpngate subscriptions revoke --id phone
yg-vpngate config subscription-reset
yg-vpngate config subscription-stop --apply-services
```

安装菜单负责创建 systemd 单元，直接命令使用 `--apply-services` 前须已有单元。
普通启停和地址修改保留令牌；`subscription-reset` 只重置默认分享，不影响独立分享。
`--public-base-url -` 清除公网地址，省略则保留。默认监听 `::`，支持 IPv4/IPv6，HTTPS 由反向代理提供。

新 URL 为 `公网基础地址/s/<令牌>/<output-id>`。默认输出是 `normal-sing-box`、`normal-mihomo`、
`normal-provider`、`normal-base64`、`normal-uri`；VPNGate 换为 `vpngate-` 前缀。
独立令牌仅在创建时显示，服务端保存 SHA256。改名称或模板不改变 URL。
鉴权在 ETag 判断之前；GET/HEAD 支持条件请求。HEAD/304 不计次数，实际 GET 在响应开始前扣一次，
中断传输不退回次数。次数跨重启保留。到期、撤销或次数耗尽返回 404；输出缺失/必需来源过期返回 503。
创建分享前须配置监听及可用公网地址。撤销同时清理该分享的计数文件；查看不再列出已撤销的默认链接。

上游流量按来源保存；只有输出恰好来自单一远端来源时发送 `subscription-userinfo`，不合计多个账户配额。
`logs` 提供最后成功/检查时间、失败与退避、流量、到期和分享次数。
Webhook 接收事件 JSON；已配置的 Telegram 接收失败/恢复、分发状态变化和 72 小时内到期提醒。
同一来源相同到期时间只提醒一次，通知发送失败记录在日志中，不撤销已完成发布。

## Web 管理与按需检测

```bash
yg-vpngate subscriptions serve --listen 127.0.0.1:9099
# 在自己的电脑运行：
ssh -L 9099:127.0.0.1:9099 root@服务器
```

访问 `http://127.0.0.1:9099`，输入终端显示的管理令牌。它与下载授权独立，存放在
`subscriptions/admin.json`，浏览器不把它写入 localStorage。Web 仅在命令运行期间存在，Ctrl+C 退出，
只能绑定回环 IP。页面支持预览/应用、输出下载、分享、日志、回收站、检测、分发、加密备份和恢复。
所有 API 要求 Bearer 管理令牌；公开下载 listener 没有管理 API。

```bash
yg-vpngate subscriptions check --id normal-sing-box --probe-url https://www.gstatic.com/generate_204
```

检测启动最多两个临时 sing-box 进程，通过节点的实际协议发出 HTTP 请求（包括 Hy2/TUIC 的 QUIC 握手），
每节点最多 12 秒、每次最多 64 节点。输出包含启动、握手和 HTTP 请求耗时；不代表纯 RTT、UDP 业务吞吐
或长期可用性。结束/取消后回收进程及暂存文件，不修改节点选择或 VPNGate 状态。

## 分发目的地

原 Telegram 和私有 GitLab 推送继续可用。`deliveries` 增加 WebDAV、普通 HTTP PUT 和既有私有 GitHub Gist。
每条选择一个已发布输出；默认手动执行，`automatic: true` 才在订阅守护进程中检查并分发。
内容和目的地配置均未变化时不重复上传，失败指数退避；不跟随重定向，避免凭据被转发到其他地址。

```json
[
  {"id":"nas","kind":"webdav","output":"phone","url":"https://nas.example/webdav/phone.json","username":"user","password":"replace-me","automatic":false},
  {"id":"device","kind":"http-put","output":"normal-provider","url":"https://device.example/config","token":"replace-me","automatic":false},
  {"id":"gist","kind":"github-gist","output":"phone","url":"https://api.github.com/gists/0123456789abcdef","filename":"phone.json","token":"replace-me","automatic":false}
]
```

```bash
yg-vpngate subscriptions deliver --id nas
yg-vpngate subscriptions export --id phone --file /root/phone.json
```

Gist 更新前读取可见性，只允许既有 secret Gist；secret 不等于访问鉴权，知道链接的人仍可访问。
服务端必须已支持目标 WebDAV/PUT 路径；程序不会创建远端目录或账户。

## 加密备份与回收站

```bash
read -rs -p '备份口令: ' password
printf '%s' "$password" | yg-vpngate subscriptions backup --file /root/config.sbvpn
printf '%s' "$password" | yg-vpngate subscriptions restore-backup --file /root/config.sbvpn
printf '%s' "$password" | yg-vpngate subscriptions restore-backup --file /root/config.sbvpn --apply
unset password
```

AES-256-GCM 加密，PBKDF2-SHA256 600000 次，随机盐和 nonce；口令 12–1024 字节，从 stdin 读取。
备份包括配置、节点凭据、证书、VPNGate 设置、上游缓存与远端设置，不含分享授权、管理令牌、运行程序和 systemd 单元。
恢复先暂存并验证路径、schema、证书和核心配置；必需远端缓存过期会阻止恢复，不能发布不完整结果。
`--apply` 仅用于 Linux：先停止原活动服务，让旧网关完成清理，再替换文件、应用防火墙并恢复原活动服务。
不在备份中的 VPNGate 服务停用；错误恢复原文件与状态。分享保持关闭，旧授权永不恢复。
新的机器应先安装同版程序与服务单元。Argo 的 systemd 启动参数及外部证书续期服务需另行配置。

```bash
yg-vpngate subscriptions archive --kind output --id phone
yg-vpngate subscriptions trash
yg-vpngate subscriptions restore --id 回收站条目ID
```

归档前检查依赖，应先撤销分享、移除分发引用，再归档输出、集合和来源。恢复不重新授予已撤销权限，
ID 冲突或陈旧必需来源会阻止恢复。最多保留 32 条、30 天、2 MiB；在下次归档/恢复时清理超限旧条目。

## 资源边界

每类配置对象最多 128（分发最多 16）；单次输入 8 MiB；所有来源最多 4096 节点；
远端缓存合计 8 MiB；规则合计 4096；全部输出合计 8 MiB；序列化状态文件最多 16 MiB。
来源下载并发 2、请求超时 20 秒、有限重定向，默认每小时刷新；失败从 60 秒退避至最多 6 小时。
订阅服务每分钟调度，停止它时自动刷新和自动分发也停止。手动命令仍可使用。
这些限制作用于管理数据，不限制代理累计流量。测试结果与实际测量见 [重构验收记录](refactor-status.md)。
