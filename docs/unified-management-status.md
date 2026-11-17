# 部署前重构与验收记录

更新：2026-11-17。本轮基于 `cb100e4`，面向首次部署：数据库 schema **9**、备份容器格式 **7**、平台配置 schema **4**、订阅配置 schema **1**。旧数据库及包含旧数据库的备份明确拒绝，不迁移旧数据或接口。

## 实施结果

- 配置一致性：账户导出只读取一次完整已应用配置，候选订阅编译显式注入候选快照；不再混合 JSON 投影中的地址、端口与数据库配置。
- 订阅恢复：记录账户来源的 revision/可用性，禁用解除、额度增加、订阅者恢复和周期重置后自动重建输出；纯账户来源也会触发，无变化不重复排队。读取配置从空结构解码，修复默认模板 TUN 残留到 URI 模板的问题。
- 主机稳定性：Argo 按 InvocationID 与隧道设置保存域名，日志滚动不再清空；BBR 先恢复内核值再删除项目文件，部分失败可重试；取消时使用独立有界上下文恢复观察状态。
- 性能：账户策略改为独立数据库字段，新增订阅者索引、有界时区缓存和计量 Tick 容器复用；移除包初始化 bcrypt 计算。WAL/FULL、64 KiB 预留和崩溃保守计费保持不变。
- 模块边界：主机观察/恢复移入 hostservice，任务详情访问规则位于 application，订阅预览返回结构化结果，分享和任务 UI 独立成模块。旧订阅文件写入、计划及归档路径只保留为回归测试夹具，不编入生产。内部 Profile/Inbounds 仍用于初始化和核心适配，持久化节点格式仍为 ManagedNode。
- 前端：统一主题、间距、表格工具与空状态；次要动作收纳为“更多”，小屏表格使用带字段名的卡片。增加创建权益→绑定→分享引导和可复制分享结果，用户页显示额度进度。日期支持秒且保留未编辑的原值，修复分页末页按钮被异步完成重新启用；操作员可查看自己的 apply 详情。
- 文档：同步 schema、接口行为、恢复与性能说明；将旧根目录 VPNGate 方案整理到 [VPNGate 实现](vpngate.md)，移除失效菜单、脚本路径与 app.json 权威配置说明，发行清单同步更新。

## 验证

Windows amd64：Go 1.27.3、Node 24.18、官方 sing-box 1.14.4、Mihomo 1.19.33。Linux：独立 Ubuntu amd64 VM，QEMU TCG，2 vCPU / 2 GiB。

| 检查 | 结果 |
| --- | --- |
| `go test -count=1 ./...`、`go vet ./...` | 通过；含配置、权限、周期、订阅、备份恢复、协议链路等回归 |
| race | accounting、adminauth、controlstore、domain、hostservice、application、manage、gateway 通过；传入本次构建的受管核心；时区缓存调整后补充检查通过 |
| `npm test` | 三组通过；新增末页禁用/重复请求、日期精度、任务详情能力、分享结果窗口回归 |
| gofmt / Prettier / diff whitespace | 通过 |
| Windows 管理器与受管核心 | 构建成功，本地独立 no-system 面板启动成功 |
| Linux schema 9 新安装 | 登录、权益创建/绑定、导出、QR、首次使用及下载次数限制通过 |
| Linux systemd / TLS | 停止状态保留、端口冲突回滚 active/enabled 和配置、导入证书指纹到达真实 TLS 监听器通过 |
| Linux 配置/订阅回归 | 破损/未确认投影导出、四种纯账户订阅恢复、真实受管核心凭据发布重试通过 |
| Linux 服务启动恢复 | 通过；首次调用漏设 GATEWAY_BINARY，补齐环境后重跑通过 |
| Linux ENOSPC / SIGKILL | 通过：不交付未落盘预留、空间恢复后可写、崩溃不错误退款 |
| 数据库 | 新安装 schema=9，integrity_check=ok |
| Linux amd64 / arm64 | 管理器和受管核心双架构发行包构建通过；arm64 未做实体运行测试 |

100 活跃授权 Tick 由约 1.12 ms 降至约 0.674 ms，每轮分配由约 260 KB 降至 190 KB。持久预留复测约 57.5–57.7 MB/s，位于基线范围；首次较慢样本同时保留。不将微基准等同于代理网络吞吐或整机资源改善。详见 [性能记录](optimization.md#schema-9-本机对照) 与 [原始数据](validation/2026-11-17-schema9-performance.json)。

## 验证边界

浏览器工具在本地页面绑定时报 constraints 内部错误，原生浏览器路径因无法可靠确认当前 URL 而自动停止。因此本轮未完成真实桌面/移动端截图与视觉验收；前端 DOM 回归不能替代实际视觉确认。

未重新执行公网 ACME、第三方 VPNGate 志愿线路、物理断电、整机重启及长期满载；闭源客户端和实体 OpenClash 路由器仍需对应环境验证。本轮不是零 BUG 保证。更早的 Linux 网络隔离、升级及资源实验仅见 [前轮记录](https://github.com/sck03/sing-box-vpn/blob/cb100e4/docs/unified-management-status.md)，不冒充本轮结果。

## 清理与交付

旧订阅生产文件已改为仅测试夹具；失效根目录方案已归入 docs，必要回归夹具、许可证及开发工具保留。本轮本地递归删除被自动审批拒绝（blocked by policy），已按授权把临时面板数据和测试二进制集中移至 `待手动删除/schema9-refactor/`，可整体手动删除；目录被 Git 忽略。当前发行包在 `.build/release/`。Linux 验收使用可丢弃目录，脚本退出时停止测试服务、删除单位和临时配置并恢复原管理器。

分支推送不等于 GitHub Actions 通过，也不等于发布 Release 或部署生产。交付使用 `codex/platform-refactor` 分支。
