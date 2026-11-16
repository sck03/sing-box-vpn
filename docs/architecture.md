# v4 架构与维护

`control.db` 是配置、管理员、权益、凭据、账本、下载授权和任务的权威状态。SQLite WAL + FULL 同步 + 外键。schema=4，面向空目录，不提供旧数据迁移。配置派生 JSON、订阅缓存和证书材料是不同职责，不允许旧 CLI 修改配置投影。

## 模块边界

| 模块 | 职责 |
| --- | --- |
| internal/domain | 稳定身份、权益/周期规则，不依赖 HTTP 或 SQLite |
| internal/application | HTTP/CLI 共享类型化命令 |
| internal/database | 连接策略、schema 版本与文件权限 |
| internal/adminauth | bcrypt、Cookie 会话、CSRF、角色、限速、TOTP、审计 |
| internal/accounting | 权益激活、日历账期、凭据代次、预留/检查点、下载计数 |
| internal/controlstore | desired/applied revision、持久任务、运行心跳、观测状态 |
| internal/manage/platform_* | 应用编排、配置图校验、备份、证书、主机服务、HTTP/CLI |
| internal/manage/panel | 原生 ES 模块 SPA，共享表单/列表/草稿，静态嵌入二进制 |
| internal/subscription | 纯配置处理、解析、集合预算、能力表和客户端渲染 |
| internal/runtime | 受管 sing-box 注册、认证后计量守卫 |
| addons/vpngate/gateway | 独立 VPNGate 后端、netns/OpenVPN、出口验证和 SOCKS 转发 |
| internal/command | 子进程期限、取消、有界输出 |

面板、普通核心、VPNGate 核心、VPNGate 网关和可选下载/Argo 服务分进程运行。一个权益不创建一个进程或端口。普通与 VPNGate 同库共享额度池，网关后端不重复计费。

## 配置应用

HTTP/CLI → Validate → 校验资源引用/核心配置 → revision CAS 保存 desired+任务 → 发布日志 → 主机应用 → 新核心心跳 → applied。只有成功改变 applied revision。失败重建旧投影、恢复原 active/enabled，任务记录回滚结果。重启面板恢复已应用投影并重放幂等 apply；非幂等中断任务标失败供核对。

单 worker 文件锁防双管理器。排队最多 32，任务展示/保留 200 条，结果最多 256 KiB，每任务期限 5 分钟。提交后的任务不依赖浏览器连接。新的运行实例心跳与配置 revision 分开，未重启的无关实例可继续持旧启动 revision。

凭据撤销和账本变更不参加配置回滚，不能复活已撤销用户。新增/轮换凭据重启对应实例；现有代次守卫阻止旧认证会话继续转发。账户独立到期不依赖面板或下载监听。

## 计量与恢复

每权益每实例共享最多 64 KiB 持久预留池，批量累计检查点幂等结算。预留先落盘再交付转发，崩溃保守占用剩余额度。关闭连接时回收无引用池；无图表观看时不影响计量。周期以 IANA 时区与日历计算，短月截断，时钟回拨不回退 period ID。旧周期检查点只结算旧账期。

备份使用 VACUUM INTO 取得一致快照，再 AES-GCM 加密。恢复校验数据库结构等于本发行 schema，拒绝额外 trigger/view 和路径逃逸。同机恢复保留安全和账本；灾难恢复临时目录完成后发布，禁用所有权益并撤销 token。

## 安全与资源

只允许固定类型的系统动作；没有网页任意 shell。公开面板强制 TLS/可信代理；会话存哈希，CSRF、同源、Host、CSP 与服务端角色检查。SSE 每轮复核会话，状态缓存 4 秒、刷新 5 秒、慢写 10 秒截止；页面隐藏断开，无无限消息队列。

VPNGate 转发必须通过独立 namespace 的已验证出口，DNS 和公网 IPv4 拨号在隧道 worker 内执行；出口验证失败或身份改变立即撤销准入。防火墙/网卡/namespace 仅处理完整所有权匹配资源，不清空用户系统规则。OpenVPN 输出流有 2 MiB 日志预算和持续消费，cloudflared 使用 journald。

## 扩展方式

新增协议先加入能力登记、领域映射与运行时认证适配，再通过 TCP/UDP、两个账户、禁用/到期/轮换验收。新增客户端扩展 subscription renderer；新增通知实现目的地适配；新增节点应提供 RuntimeID 和独立额度分配策略。当前只支持单机共享 SQLite，不把该模型误称分布式全局严格额度。

高级核心 JSON 局限于适配边界；字段保留、引用和循环检查必须通过。VPNGate 保护路由不被普通高级对象覆盖。未来拆分 manage 时以业务模块为界，不再增加平行配置真源或重复表单业务。
