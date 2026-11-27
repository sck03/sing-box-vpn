# 表单、队列通知与模块依赖重构

更新：2026-11-27；实施基线 `6919b6c`。保留 Go 模块化单体、原生 ES 模块、共享表单和现有窄屏布局；没有增加生产依赖，也没有增加旧版本兼容接口。数据库与配置格式版本不变。

## 已完成

| 问题或边界 | 当前实现 |
| --- | --- |
| 新增 DNS 从 UDP 切到 TLS/HTTPS 后仍保存 53 端口 | 新增时省略端口，使用核心协议默认值；显式自定义端口保留。DNS 字段与规范化集中到独立模块 |
| 高级网络字段缺少常用表单入口 | DNS 增加 TLS 服务器名、自定义 CA；WireGuard 增加 MTU。其他高级字段继续保留 JSON 编辑 |
| 访问方式切换把已有 token 列表误判为空 | 通过表单控制器读写完整可信代理值，不访问控件内部 input |
| 弹窗被替换后兼容性复查仍继续请求 | 合并编辑器会话信号与单次请求信号；移除只监听 close 的清理逻辑。JSON 弹窗也归 dialogSession 管理 |
| 三个队列竞争同一个唤醒通知 | control/subscription/probe 独立通知，入队及取消通知对应队列；保留跨进程轮询兜底 |
| 仓储依赖应用层的分页、身份和任务类型 | 共享值对象移到 domain，accounting/controlstore 生产代码不再依赖 application；删除旧定义，不保留别名 |
| 未使用的 VPNGate 档案无法清理 | 未启用档案提供删除操作；预览、排队、执行前校验全部保留档案的凭据身份，绑定未解除时拒绝删除。切换国家继续保留绑定 |
| 删除档案后可能残留秘密 | 编辑器删除对应条目，配置规范化同时移除无档案归属的秘密 |
| quality 测试与页面可见性不匹配 | 可见页面夹具启用 pretendToBeVisual，已有隐藏页回归继续保留 |
| Windows socket 回归因临时路径过长失败 | 使用短名称临时目录并显式回收，正常默认 TEMP 即可执行 |

应用用例继续通过窄仓储接口执行；HTTP 与主机操作由管理器适配。配置发布、激活、确认和恢复仍保持原有串行一致性。未取得锁等待与吞吐基准前，不把复杂状态依赖的构建过程直接移出锁。WAL/FULL、64 KiB 持久预留、授权窗口和计费规则未变。

## 本轮必要验证

- `npm run typecheck`：三套配置通过。
- 相关前端脚本通过：quality、selection、contracts、state-sync、refactor。新增回归使用原生表单校验/提交，覆盖协议默认端口、自定义 TLS 保留、已有及空可信代理列表、弹窗替换取消、未启用档案删除。
- Go 定向回归通过：领域任务权限、命令队列选择、控制存储任务、分页/绑定、维护执行基线，以及新增的按队列通知和国家档案引用校验。socket 关闭回归在默认 Windows TEMP 下通过。
- 定向 Go 命令使用发布标签 `with_quic,with_utls,with_wireguard,with_gvisor`，执行范围为 domain/application/accounting/controlstore/manage/vpngate 的相关测试；没有执行完整 Go 测试集。
- `go run ./cmd/panel-contract -check` 通过；内部类型移动没有改变公开 JSON 契约。
- `go build -tags with_quic,with_utls,with_wireguard,with_gvisor ./cmd/...` 通过，覆盖全部命令入口的编译检查。
- 官方 sing-box 1.14.4 对省略端口的 UDP/TLS/HTTPS DNS 配置检查通过，TLS/HTTPS 同时包含显式服务器名。
- `git diff --check` 通过。

本轮没有重新执行完整前端套件、race、真实浏览器、Linux systemd/netns/nftables、真实代理链路、存储故障、长期负载或性能基准。前一轮审查的测试结果不计入本轮验证。不宣称未经测量的运行内存或吞吐提升。

## 清理与交付

- 使用仓库清理脚本删除 node_modules，释放 **49,782,122 字节（47.48 MiB）**。清理后工作区约 **7 MiB**，包含 `.git`；提交元数据可能使体积略有变化。
- 工作区扫描没有旧数据库、可执行文件、压缩工具包、测试日志或虚拟机磁盘。保留被文档引用的示例图、必要回归、紧凑历史验证摘要及许可证。
- 原 application 中分页与任务类型的定义及测试已迁至 domain，旧文件删除；页面内重复 DNS 字段和手动弹窗清理代码已移除。
- 外部临时目录 `C:\Users\bridge\AppData\Local\Temp\sbvpn-audit-20261127` 与 `E:\sbvpn-audit-tmp` 合计 **1,165,612,070 字节（约 1.09 GiB）**。整目录及逐文件清理均被自动审批以 `blocked by policy` 拒绝，仍保留，未计入释放量。没有清理全局 Go 模块缓存或外部部署数据。
- 开始实施时本地和 origin 均只有 main，不存在需合并或删除的其他分支。最终提交与远端状态以 Git 为准；推送不等于生产部署或全量 CI 验收。

清理后可用 `npm ci` 恢复前端开发依赖；Go 工具版本与开发环境说明见 [开发文档](development.md)。
