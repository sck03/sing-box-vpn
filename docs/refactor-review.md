# 稳定性、发布一致性与资源优化实施报告

更新：2026-11-25。针对前次审查中确认的问题实施，面向首次部署。数据库 schema=17、平台=7、节点=5、订阅=6、备份容器=8；ParserVersion=3、CompilerVersion=7、受管核心 1.14.4+sbvpn.7。只接受新数据，不提供旧数据库、任务队列或旧接口迁移。

## 修复与模块边界

| 问题 | 最终实现 | 回归证据 |
| --- | --- | --- |
| 无效 CDN/SNI 预检与保存成功，账号导出失败 | domain 统一地址约束；平台与订阅复用；CDN/Argo 转换后再验证；API 返回具体字段 | publication_validation_test、address_test |
| Argo 临时域名观察读取默认日志空间 | 复用 servicecatalog.Journal 和当前 invocation，指定 journalctl 命名空间 | 参数断言、Linux 真实 journald 验收 |
| 无关路由或未启用国家修改触发重启 | 独立 deployment_plan 按服务实际输入比较规范 JSON，预览与执行共用 ServiceChange | deployment_plan_test、前端部署计划回归 |
| 慢速探测阻塞刷新/投递 | control/subscription/probe 三条有界队列，probe 最多占 8 个排队名额；认领事务内复核排他条件 | lanes_test、task_lifecycle_test |
| 运行中探测不能停止 | 持久取消请求、上下文注册与二次读取防竞态、释放资源后终结任务；前端读取服务端取消能力 | task_cancellation_test，包含真实核心、连接与暂存目录释放 |
| VPNGate 证书弹窗没有当前选择 | 复用 boundCertificate，识别路径分隔符并回选已有资产 | test-panel-deployment.mjs |
| 持久计量存在重复读取和批次分配 | 使用 guarded upsert 的 RowsAffected 验证归属，移除重复 SELECT；写协程复用结果数组 | reservation_ownership_test、既有账期/并发/故障测试与基准 |
| 无用声明与检查缺口 | 清理未使用导入/声明，下划线标明回调占位；类型检查启用 noUnusedLocals/noUnusedParameters | 类型检查、vet、生成契约检查 |

HTTP 保持传输和权限职责；application 定义调度策略；controlstore 负责队列、持久取消和恢复；manage 组合执行与主机操作。部署计划、取消上下文和存储生命周期各自独立，不引入新进程或框架。原有订阅缓存、活动投影、凭据版本校验与账户额度边界保留。

## 性能样本

同一 Windows amd64 / i5-9400F / Go 1.27.3 工作区，将修改前后编译为独立测试程序，串行执行三轮、每轮一秒。使用相同 schema=17，旧实现通过 Go overlay 编译。

| 基准 | 修改前中位数 | 修改后中位数 |
| --- | ---: | ---: |
| 单路持久预留耗时 | 1,161,257 ns | 1,153,417 ns |
| 单路分配次数 | 249 | 210 |
| 并发持久预留耗时 | 335,298 ns | 313,562 ns |
| 并发分配次数 | 238 | 199 |

并发耗时约降低 6.5%，单路耗时基本持平；分配次数降低约 16%。保留 64 KiB 预留、保存点隔离、WAL/FULL 和提交后才转发的约束。早期与其他验证重叠的耗时样本不用于性能结论。这些测量不代表整机 RSS 或 VPN 吞吐同比变化。

## 验证、版本与清理

准确命令、结果、跳过项和原始样本见[本轮验证记录](validation/2026-11-25-stability.json)。记录区分本地测试、Linux 交叉编译与真实 Linux CI。前端共 12 组回归；不把 Windows 无法执行的 systemd、namespace、防火墙和磁盘故障测试写成本地通过。

首次全量回归发现既有测试仍认领已删除的 background 队列；夹具已更新为 subscription，并重新验证。重启计划回归还覆盖 JSON 数值类型差异造成的误判。取消覆盖认领前后、完成同时到达及服务重启，已取消任务不会被迟到结果重新标记成功。

本轮源码中的无用声明已清理。工具链、许可证与可复用依赖/构建缓存保留；分析实例、测试二进制、overlay 和原始日志在提取证据后集中到 Git 忽略的 .cleanup-manual/2026-11-25-stability/。该目录可整体手动删除，仓库记录不包含凭据或临时数据库。

提交目标为 main，不发布 Release 或部署实例。历史缓存与历史验证记录保留其原始含义，不替代当前测试结果。
