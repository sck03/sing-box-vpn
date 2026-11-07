# 性能与稳定性验证

[返回项目首页](../README.md) · [验证命令](development.md) · [模块边界](architecture.md)

本文记录当前实现和验证边界；以往的分配测量与修复记录见
[3cfeb31 时的优化日志](https://github.com/sck03/sing-box-vpn/blob/3cfeb31/docs/optimization.md)。

## 本轮修复 · 2026-11-08

基线提交 `3cfeb31`。OpenVPN 日志使用最大 1 MiB 的逐行扫描器，遇到更长的单行后停止读取，
剩余输出会填满管道，导致握手或运行过程阻塞。扫描协程与 `Cmd.Wait` 分离，还可能在退出时
丢失尾部日志。原日志上限按整行判断，最后一行可能令文件超过 2 MiB。

新增独立的 `openvpn_output.go`，直接消费进程输出块，跨块检测就绪标记，仅保留固定长度尾部。
stdout/stderr 共用接收器，`os/exec` 等待输出消费后再返回；关闭日志后才通知进程退出。
日志严格截取前 2 MiB，保留原始换行和未换行尾部；磁盘错误、短写或达到上限后仍消费输出。
不再使用逐行扫描协程，也不逐行构造字符串和补换行。

同机 Windows amd64、Go 1.27.3、i5-9400F、默认并行度 6，`-benchmem -count=3` 中位数：

| 输入 | 修改前 | 修改后 |
| --- | --- | --- |
| 4,096 条诊断日志，最后附就绪标记 | 429.8 µs / 397,586 B / 8,198 次分配 | 5.61 µs / 112 B / 1 次分配 |

临时分配下降约 99.97%。基准写入 `io.Discard`，新实现按 32 KiB 块输入；旧实现按基线代码
使用 Scanner、字符串转换及逐行写入。测量不包括磁盘、进程启动或管道复制缓冲，不能用于
推断整机 RSS、实际磁盘吞吐或代理转发速度。生产中的进程管道仍使用 `os/exec` 的复制缓冲。
当前实现复现命令：

```bash
go test ./addons/vpngate/gateway -run '^$' -bench '^BenchmarkOpenVPNOutput$' -benchmem -count=3
```

本地全量 `go test -race -count=1 ./...`（启用持续单向 UDP）、52 条真实协议链路、
`go vet`、gofmt、Bash 语法及 ShellCheck 均通过。Linux amd64/arm64 发行包构建通过，
Linux netns、防火墙和 systemd 专项由推送后的 GitHub CI 执行。

## 持续维护的资源边界

| 流程 | 约束与可用基准 |
| --- | --- |
| OpenVPN 日志 | 持续消费、固定标记缓冲、严格 2 MiB 文件预算、退出等待；`BenchmarkOpenVPNOutput` |
| 连接接入 | 临时错误有限退避，保留已有会话；`TestAcceptRetryBackoffAndCancellation` |
| 连接准入 | 已接入连接与待拨号共用容量，满载直接拒绝；`BenchmarkManagerOpenAtCapacity` |
| UDP 转发 | 独立目标队列、缓冲复用、有界字节预算；`BenchmarkSOCKSUDPRoundTrip`、`BenchmarkSOCKSUDPTargetBatch` |
| 节点源 | 流式读取、记录/列数/候选上限、解码缓冲复用；`BenchmarkNodeSource/Streaming`、`BenchmarkNodeSourceExcessColumns` |
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
