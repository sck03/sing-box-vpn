# 统一管理重构实施记录

更新：2026-11-16。对应 [实施前分析报告](unified-management-analysis.md)。

**完整统一平台尚未完成。** 当前交付包括 F1–F7 修复、订阅应用层解耦，以及可单独构建和验收的账户账本/受管核心。现有安装入口仍使用官方外部 sing-box；不能把新增账户页面理解成现有服务已经启用限额。

## 已落地

| 范围 | 实现与验证 |
| --- | --- |
| F1 构建预算 | 拒绝重复来源/规则集引用；展开集合、规则和合计输出均有预算；追加前验证 |
| F2 本地维护 | 本地维护按可用依赖发布，必需远端失效时撤下受影响输出，其他本地输出可继续更新 |
| F3 程序升级 | 独立升级流程保留 enabled/active 状态，校验失败回滚；不通过首次安装重新启用服务 |
| F4 304 元数据 | 更新并校验流量/到期响应头，缺失头保留现有信息；拒绝重复、非法和超大 usage 头 |
| F5 默认授权 | 精确匹配内置输出白名单，自建同前缀输出不会自动获得默认授权 |
| F6 可选来源过期 | 刷新后重新构建并剔除失效可选节点；仍有有效节点的输出继续发布 |
| F7 核心升级校验 | 纳入自定义 sing-box 配置产物与证书/本地规则文件，前后核对清单摘要 |
| 订阅应用层 | HTTP/CLI 调用 `SubscriptionService`，不再由 HTTP 拼装 CLI 参数或创建临时输入文件 |
| 并发与响应 | 订阅编译在配置锁外进行，提交复核配置/缓存/核心身份；冲突返回 409；预览返回摘要而非完整秘密产物 |
| 后台任务 | 刷新和分发拥有独立调度与期限，慢分发不阻塞刷新 |
| 账户应用层 | HTTP/CLI 共用 `AccountService`；创建、修改、绑定、轮换、独立导出；默认列表不返回代理秘密 |
| 账户账本 | SQLite WAL、FULL 同步、revision、持久额度预留与幂等累计检查点；普通/VPNGate 共用额度池 |
| 受管核心 | 固定 sing-box 1.14.4，在认证之后、路由之前包装五协议的 TCP/UDP；不在 Web 进程内运行 |

现有安装、服务、VPNGate 所有权和防火墙行为保持原边界。删除了 HTTP→CLI 中转代码、临时备份输入文件路径和重复内联脚本；前端拆为 API、订阅操作和账户模块。分析基线与历史验收文档保留，不能作为无效文件删除。

## 账户运行时的实际语义

- 当前一个账户对应一个额度池，普通和 VPNGate 实例可共享；ID 不随显示名变化。
- `quota_bytes: null` 表示不限；`0` 表示零额度。当前支持绝对到期、手动禁用和凭据代次。
- 每次预留额度在事务中先落盘，再允许转发。每账户每运行实例的未消耗额度与在途分配合计不超过 64 KiB；其他实例无法再次取得这份额度。
- 上/下行是认证后交给代理转发的有效载荷字节，不是网卡总量，不重复计算 VPNGate 后端隧道。
- `charged_bytes` 包含尚未消费的预留以及崩溃后保守保留的预留；`upload_bytes` / `download_bytes` 是已经结算的方向计数。二者不是同一个指标。
- 干净退出会回收未消费预留。强制退出最多可能保守多扣每账户每实例 64 KiB，不承诺断电零误差。方向计数在崩溃时可能落后最后的实际传输，但已预留额度不会重新发放。
- 配额在转发边界执行；到期和代次也会在 I/O 边界检查。集中调度每 250 ms 检查并关闭被禁用账户的路由连接；操作系统调度、阻塞 I/O 和在途数据不构成绝对的停用时延保证。
- 已有 QUIC/复用认证会话对象可能仍存活，但旧代次不能通过守卫转发新流。没有声称所有协议都能直接销毁认证会话对象。
- 新增绑定或轮换后，新凭据需要重启对应受管核心才能载入；这会影响该实例其他账户。停用旧代次不依赖这次重启。
- 目前运行时只登记经过测试的五协议入站、相关代理出站、direct/SOCKS、selector/urltest 和基础 DNS。尚未登记 WARP 等端点；遇到不支持的配置会报错，不静默丢弃。
- `accounts.db` 是账户/凭据/账本的唯一真源；`app.json` 等仍是原节点配置真源。节点统一模型和全配置数据库改造尚未完成。

## 开发验收入口

在独立测试配置目录使用，不要直接替换现有服务命令：

```bash
go build -tags with_quic,with_utls -o .build/managed-core ./cmd/managed-core
MANAGED_CORE_BINARY="$PWD/.build/managed-core" \
  SING_BOX_BINARY=/path/to/sing-box MIHOMO_BINARY=/path/to/mihomo \
  go test -race -count=1 -run '^(TestManagedAccountIsolation|TestRealFiveProtocolsTCPUDP)$' ./addons/vpngate/gateway
```

账户 CLI 从 stdin 读取一个 JSON 命令，避免秘密进入命令行。HTTP `POST /api/accounts` 使用同一个命令模型，并继承管理令牌与同源检查。

```bash
printf '%s' '{"operation":"save","account":{"id":"alice","name":"Alice","quota_bytes":107374182400,"expires_at":null,"revision":0}}' |
  yg-vpngate accounts --root /path/to/test-root
printf '%s' '{"operation":"bind","id":"alice","scope":"normal"}' |
  yg-vpngate accounts --root /path/to/test-root
printf '%s' '{"operation":"export","id":"alice","scope":"normal","client":"sing-box"}' |
  yg-vpngate accounts --root /path/to/test-root
```

运行时单独启动：

```bash
.build/managed-core --database /path/to/test-root/accounts.db \
  --config /path/to/test-root/sb.json --instance normal
```

它只接受数据库中显式绑定的用户，生成配置中的原单用户凭据不会成为绕过计量的默认账户。普通与 VPNGate 应使用不同进程和不同配置，后者继续经现有隔离网关。

## 本次验收证据与缺口

- F1–F7、revision 冲突、锁外编译、调度隔离、账户导出不串号、默认列表不泄密均有回归。
- 两账户共享入口的五协议 TCP 测试通过：禁用 A 后现有流和缓存认证会话的新流不能继续转发，B 保持可用。
- 受管核心普通/VPNGate 的五协议 TCP/UDP、sing-box/Mihomo 原生输出、provider 和适用的 URI 路径通过现有真实链路矩阵。
- UDP 矩阵曾发现包装层丢失协议缓冲区 headroom 导致崩溃；已修复为传递缓冲区元数据，同时禁止可绕过计量的 unwrap 快路径。
- SQLite 两连接并发共享预留、重放、重新打开数据库、干净关闭退款、过期、禁用、旧代次、零额度、空 UDP 包、在途预算有回归。
- Linux VM 实测通过多端口 IPv4/IPv6 防火墙、UDP 冲突、systemd 启动恢复、netns/OpenVPN 隔离，以及程序升级停止态、活动态和回滚脚本。
- Windows 全量 `go test -race -count=1 ./...`（启用 UDP idle 与受管核心）、`go vet`、ShellCheck、Bash 语法检查和两项菜单/升级脚本测试通过；三个前端模块通过 Node.js 语法检查。
- Linux VM 的受管核心五协议双账户停用/隔离测试通过；受管核心 Linux amd64/arm64 均构建通过。现有程序的双架构发行包构建通过，但未发布 GitHub Release。
- 浏览器控制工具因不能可靠确定 Windows 当前浏览器 URL 而停止。**尚无实际点击与移动端布局验收结果。**
- 尚未完成每协议的真实到期/超额/轮换 TCP+UDP 全矩阵、真实断电/满盘注入、持续吞吐与账户规模测量。不能把已有通过项扩展为这些场景已经通过。

## 尚未完成的原始范围

| 阶段 | 剩余工作 |
| --- | --- |
| B 统一业务与状态 | 新节点/权益模型、单一全配置数据库、持久任务、desired/applied revision 和应用恢复；当前账户绑定仍依赖原五协议节点配置 |
| C 完整面板 | 原日常菜单逐项整合，通用入站/出站/路由/DNS/证书/隧道/服务管理；管理员密码散列、角色、可撤销会话、TOTP；浏览器验收 |
| D 完整权益闭环 | 独立订阅者/多权益、账户下载授权和用户页、首次使用与周期重置、运行时自动安装/激活/回滚、全量备份对账与协议完整故障矩阵 |
| E 性能与故障 | 大账户/高连接数基线、按需实时推送、持续资源回收、磁盘故障和恢复测试；收敛逐次 I/O 授权查询及批量结算成本 |

在上述缺口完成前，不删除仍承担日常功能的原菜单，不切换默认服务为受管核心，也不把这次提交标记为“完整重构完成”。
