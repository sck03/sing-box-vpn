# 任务提交、页面生命周期与历史清理重构

## 2026-11-27 补充复核

补充基线 `71a52e0f8728d8e183730bba7ea425f7b7f73682`。上一阶段重构已经推送到 main，本次在其基础上补齐四个已复现边界；数据库仍为 schema 18，公开 HTTP 契约和前端布局不变。

- 并发重试计算摘要时，前一次成功响应可能先清除回执，导致重试生成新编号。请求现在保留开始时的回执视图，确认时替换有界 Map；之后明确发起的新操作仍使用新编号，旧响应也不能清除新回执。
- sessionStorage 数组中一条损坏记录曾导致其他有效回执全部丢失。恢复逻辑改为逐条校验并只复制允许字段，保留合法请求编号。
- 任务历史保留策略复用 refreshAfterSave，旧保存回调不能刷新新页面或关闭新加载弹窗。
- TOTP 密钥生成检查页面和编辑器归属；原窗口已经关闭时仅提示重新生成，不向新弹窗写入秘密。

新增纯 Node 回执回归，扩展现有 DOM 回归。四组定向脚本 task-requests、submissions、lifecycle、workflows 通过，三套类型检查和生成契约检查通过；未重新执行 Go 测试、性能基准、完整前端套件或真实浏览器验收。以下上一阶段的 64 项 Go 结果不计入本次复测，详见 [本次验证摘要](validation/2026-11-27-retry-editor-followup.json)。

本次验证临时恢复的 node_modules 已再次删除，回收 49,782,122 字节；这部分不是额外清理出的既有残留。常规 git gc 将 Git 文件体积从 4,296,370 减至 3,804,105 字节，减少 492,265 字节，历史与标签保留。清理时工作区为 7,203,573 字节（约 6.87 MiB），后续文档与提交元数据会增加少量体积。外部审查目录仍受策略限制，最新合计体积见下方清单。

## 上一阶段实施

更新：2026-11-27；实施基线 `1bc4800bf3664af466b5a3142af6e2cf60322818`。保持 Go 模块化单体、原生 ES modules、共享表单与现有窄屏布局，没有增加生产依赖。本轮数据库由 schema 17 更新到 18；开发与首次部署使用全新数据目录，旧数据库及其备份直接拒绝，不提供迁移层。平台配置、节点、订阅和备份容器的独立格式版本不变。

## 修复与边界

| 问题 | 当前实现 |
| --- | --- |
| 用户名为 scheduler 时被误判为自动任务，可能收到 task_id=0 | domain.TaskSubmission 分离操作者与可信来源；HTTP、CLI、调度器显式赋值。自动合并返回已有任务的真实编号 |
| 响应丢失后重试可能重复执行副作用或错误地报告配置冲突 | 三个 JSON 任务入口采用可选 Idempotency-Key；前端自动携带，任务/配置意图/回执同事务提交。配置准备前及修订号检查时识别重试 |
| 原任务历史被清理后重试可能重新执行 | 独立回执在保留期内返回 410 并提示核对结果；不会随任务历史立即删除。不同正文复用 key 返回 409 |
| 订阅者/管理员旧保存回调刷新新页面、关闭新加载弹窗 | 共享 refreshAfterSave 校验编辑器和页面归属；过期 pageContext 的 render 不再操作新页面 |
| 清理任务超时后审计也使用过期上下文，失败不易察觉 | application.HistoryRetention 使用窄仓储接口，各域独立 30 秒预算；失败先更新脱敏健康摘要，审计另给两秒预算 |
| 持久预留批次重复取得 SQLite writer | 每批在读取额度/租约和行级 SAVEPOINT 前取得一次 writer；移除逐请求重复 SQL，保留单项失败隔离和提交后通知 |

回执只覆盖 `POST /api/tasks`、`POST /api/config`、`POST /api/panel/restart`；文件上传恢复和普通 CRUD 沿用现有协议。前端未确认回执最多 128 项、最长重试窗口 24 小时，sessionStorage 只保存摘要、随机编号和时间，退出清除。服务端回执最多 10,000 项、至少保留 24 小时，运行中任务不按时间回收；超限时任务和 desired 变更一起回滚。

任务准入与回执存储分别从控制仓储主文件拆出；应用层只负责清理编排，具体 SQL、证书材料和 HTTP 留在所属模块。域类型不反向依赖 application。WAL/FULL、64 KiB 持久预留、额度与授权边界保持原语义。没有扩大预留额或放宽持久性来换取吞吐。

## 上一阶段必要验证

本轮 Go 定向执行共覆盖 **30 个不同顶层测试、64 个含子测试的不同测试项**，最终所选范围没有失败或跳过。复测不重复计数。范围包括任务来源、跨仓储并发重试、容量满时重试、事务回滚、回执清理、权限、清理诊断、计量并发/崩溃重放，以及当前备份往返和灾难恢复。灾难恢复额外断言旧任务回执已经清除。

- 前端选定四组通过：submissions、workflows、lifecycle、state-sync；格式整理后复测 submissions/lifecycle。
- `npm run typecheck` 三套配置通过；全体 JS 为 checkJs，指定边界额外 strict。
- `go run ./cmd/panel-contract -check` 通过，JSON DTO 与生成契约一致。
- `go vet ./internal/domain ./internal/application ./internal/controlstore ./internal/accounting ./internal/manage` 通过。
- `go build -tags with_quic,with_utls,with_wireguard,with_gvisor ./cmd/...` 通过，覆盖全部命令入口。
- `gofmt -l manifest.go internal cmd` 无输出；修改的前端文件经 Prettier 格式化。
- 66 处本地 Markdown 链接通过检查；生成文件与工作区产物核对完成，`git diff --check` 通过。

没有运行完整 Go/前端套件、race、真实浏览器、Linux systemd/netns/防火墙联动、断电故障、长期负载或生产规模 RSS 测量。先前审查的全量结果不计入本轮。测试命令与环境见 [验证摘要](validation/2026-11-27-task-submission-retention.json)。

## 性能证据

Windows amd64、i5-9400F、GOMAXPROCS=4；相同基准在实施基线和修改版各跑三轮、每轮一秒。并发持久预留从 **199 allocs/op 降到 188–190**，分配字节从 **8,116–8,123 B/op 降到 7,741–7,796**。吞吐区间分别为 149.23–162.33 MB/s 与 137.08–176.56 MB/s，重叠且磁盘波动明显，不宣称稳定吞吐或 VPN 实际带宽提升。单池分配约 207–209 次，基本不变。复测规则见 [性能与资源](optimization.md)。

## 清理与交付记录

仓库清理脚本删除了 node_modules，实际释放 **49,782,122 字节（47.48 MiB）**。清理时工作区包含 `.git` 共 **7,453,779 字节（约 7.11 MiB）**；之后的文档和提交元数据会略微改变大小。工作区未发现旧数据库、可执行文件、压缩包、测试截图或日志；保留仍被文档引用的示例图、必要回归、许可证和紧凑历史验证摘要。删除了重复的预留锁 SQL 与旧的用户名调度判断；文档合并为当前边界说明，不保留重复实施日志。

以下审查产物已经核对绝对路径、链接、内容和运行进程，但其删除被自动审批以 `blocked by policy` 拒绝，**补充复核后合计 3,346,111,054 字节（约 3.12 GiB）仍保留，未计入释放量**：

| 目录 | 字节 | 内容 |
| --- | ---: | --- |
| `C:/Users/bridge/AppData/Local/Temp/sbvpn-review-6j4slqhl` | 2,180,236,840 | 本轮隔离审查副本、工具链、模块/编译缓存、校验器、日志 |
| `C:/Users/bridge/AppData/Local/Temp/sbvpn-audit-20261127` | 1,165,611,143 | 上轮审查工具链、缓存、旧校验程序与日志 |
| `E:/sbvpn-audit-tmp` | 927 | 三份 DNS 配置测试夹具 |
| `C:/Users/bridge/AppData/Local/Temp/scheduler-http-repro-176925508` | 192,512 | HTTP 复现数据库 |
| `C:/Users/bridge/AppData/Local/Temp/scheduler-repro-3468405295` | 69,632 | 调度复现数据库 |

补充复核时仅删除两份已确认的复现数据库也被自动审批以 `blocked by policy` 拒绝。没有改用其他工具绕过删除限制，也没有清理全局 Go/npm 缓存、真实部署数据或 Git 历史。前端开发依赖可通过 npm ci 重建。

经 fetch/prune 核对，本地与 origin 都只有 main，没有其他分支需要合并或删除。采用普通提交与推送，保留标签和历史，不 force push。GitHub Actions 的结果独立于本机定向测试；推送 main 不等于部署或发布 Release。
