# 性能与稳定性验证

[返回项目首页](../README.md) · [验证命令](development.md) · [模块边界](architecture.md)

本文记录当前实现和验证边界；以往的分配测量与修复记录见
[a13e12e 时的优化日志](https://github.com/sck03/sing-box-vpn/blob/a13e12e/docs/optimization.md)。

## 本轮修复 · 2026-11-07

基线提交 `a13e12e`。共享连接循环把所有 `Accept` 错误作为永久故障，描述符、内存或
网络缓冲暂时不足时会退出 SOCKS/worker 服务，关闭已有会话，并触发网关或隧道重建。
对基线注入 `EMFILE`、`ENFILE`、`ENOBUFS`、`ENOMEM`，四类回归均复现会话被关闭。

`server.go` 统一处理可重试错误，SOCKS 和 worker 无需分别维护恢复分支：

- 已接入会话继续通信，新连接等待资源恢复。
- 重试间隔从 5 ms 倍增至最多 1 s，持续失败时不忙等；接入成功立即重置间隔。
- 停止服务立即打断退避，永久错误继续上报，并沿用现有连接回收流程。
- 定时器仅在错误路径创建，没有新增常驻协程、连接表或旧版本兼容分支。

回归使用虚拟时间验证间隔、上限、重置和取消；用真实的双向管道验证重试期间已有会话
仍能收发。它们验证错误处理行为，不等同于实际耗尽 Linux 系统资源的压力测试。
本轮不据此宣称正常转发吞吐、延迟或整机 RSS 提升。

本地全量 `go test -race -count=1 ./...`（启用持续单向 UDP）、52 条真实协议链路、
`go vet`、gofmt、Bash 语法及 ShellCheck 均通过。Linux amd64/arm64 发行包构建通过，
Linux netns、防火墙和 systemd 专项由推送后的 GitHub CI 执行。

## 持续维护的资源边界

| 流程 | 约束与可用基准 |
| --- | --- |
| 连接接入 | 临时错误有限退避，保留已有会话；`TestAcceptRetryBackoffAndCancellation` |
| 连接准入 | 已接入连接与待拨号共用容量，满载直接拒绝；`BenchmarkManagerOpenAtCapacity` |
| UDP 转发 | 独立目标队列、缓冲复用、有界字节预算；`BenchmarkSOCKSUDPRoundTrip`、`BenchmarkSOCKSUDPTargetBatch` |
| 节点源 | 流式读取、候选数量限制、解码缓冲复用；`BenchmarkNodeSource/Streaming` |
| profile 校验 | 固定字段数，过量参数及时拒绝；`BenchmarkProfileValidation` |
| 配置发布 | 单份事务快照、相同内容不重写；`BenchmarkPublishChanged`、`BenchmarkPublishUnchanged` |
| 核心更新 | 流式摘要，不持有完整配置副本；`BenchmarkCoreConfigValidation` |
| 客户端配置 | 只创建当前范围需要的规则和选择器；`BenchmarkClientConfig` |
| VMess URI | 直接编码 JSON，省去无用的 URI 字段构造；`BenchmarkVMessURI` |

测量时固定输入、工具链、机器及并行度；性能测试与 race 分开执行。并发与网络变更须保留
TCP 半关闭、UDP 双向续期、源端口稳定、退出回收及未经验证不转发的行为。
完整测试、Linux netns/防火墙/systemd 专项和发行包构建命令统一维护在
[开发文档](development.md)，不在此复制。

## 本地文件整理

临时源码、日志和基准产物统一放在 `.test/`；工具放在 `.tools/`，最新发行包放在
`.build/`。移除旧审计和上游源码顶层目录的忽略规则，避免无用源码被 Git 隐藏却仍被
`go test ./...` 扫描。无效文件的自动删除若受执行策略限制，统一保留在
`.test/obsolete-artifacts/` 供手动删除；其中内容不参与构建、测试或发行打包。
