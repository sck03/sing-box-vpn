# 性能与稳定性验证

[返回项目首页](../README.md) · [验证命令](development.md) · [模块边界](architecture.md)

本文记录当前实现和验证边界；以往的分配测量与修复记录见
[c0d528c 时的优化日志](https://github.com/sck03/sing-box-vpn/blob/c0d528c/docs/optimization.md)。

## 本轮修复 · 2026-11-09

基线提交 `c0d528c`。核心检查、服务重启、防火墙和网关系统命令原先使用无界
`CombinedOutput`。异常命令持续输出会累积内存，继承输出管道的子进程也可能在命令退出
或超时后拖住等待。命令被取消时只返回进程退出错误，调用方无法识别 context 超时。

现在由独立的 `internal/command` 模块统一执行这些短期命令：最多保留 1 MiB 合并输出，
多余内容继续消费并返回超限错误；执行期限沿用调用方 context，进程退出或取消后最多再
等待输出管道 1 秒。保留原退出码以及取消/超时原因，不接受截断的配置检查结果，也不将
超限防火墙诊断当作规则缺失。OpenVPN 的长期日志仍使用自身的有界流式接收器。

新增管理层回归在修改前复现：1 MiB + 1 字节和 4 MiB 输出均被完整保留且误报成功，
200 ms 超时不能通过 `errors.Is` 识别；修改后全部通过。公共模块另使用真实进程覆盖
标准输入、双输出流、超限伴随失败退出码、启动失败，以及主进程退出后仍被继承的输出管道。

同机 Windows amd64、Go 1.27.3、i5-9400F、默认并行度 6，`-benchmem -count=3`
的输出接收器对照测量中位数：

| 输入 | 修改前 | 修改后 |
| --- | --- | --- |
| 4 KiB 输出 | 4,144 B / 2 次分配 | 4,128 B / 2 次分配 |
| 8 MiB 输出，32 KiB 分块 | 16,744,508 B / 10 次分配 | 2,064,428 B / 7 次分配 |

异常大输出的累计分配下降约 87.7%，最终持有缓冲最多 1 MiB；小输出分配基本相同。
基准对照旧的 `bytes.Buffer` 与当前接收器，不包含进程创建、管道或系统命令耗时；后者
达到预算后丢弃后续字节，因此不能将此结果解释为代理吞吐、整机 RSS 或正常命令速度提升。
测试、竞态检查和构建与测量分开执行。复现命令：

```bash
go test ./internal/command -run '^$' -bench '^BenchmarkOutputCapture$' -benchmem -count=3
```

本地全量 `go test -race -count=1 ./...`（启用持续单向 UDP）、52 条真实协议链路、
`go vet`、gofmt、Bash 语法及 ShellCheck 均通过。Linux amd64/arm64 发行包构建通过，
Linux netns、防火墙和 systemd 专项由推送后的 GitHub CI 执行。

## 持续维护的资源边界

| 流程 | 约束与可用基准 |
| --- | --- |
| 短期外部命令 | 合并输出最多 1 MiB，退出/取消后管道等待最多 1 秒；`BenchmarkOutputCapture`、`TestOutputInheritedPipe` |
| OpenVPN 日志 | 持续消费、固定标记缓冲、严格 2 MiB 文件预算、退出等待；`BenchmarkOpenVPNOutput` |
| 连接接入 | 临时错误有限退避，保留已有会话；`TestAcceptRetryBackoffAndCancellation` |
| 连接准入 | 已接入连接与待拨号共用容量，满载直接拒绝；`BenchmarkManagerOpenAtCapacity` |
| TCP 中继 | 正常 EOF 保留响应，取消关闭双端并等待回收；`TestProxyShutdownClosesHalfClosedRelay` |
| UDP 转发 | 独立目标队列、缓冲复用、有界字节预算；`BenchmarkSOCKSUDPRoundTrip`、`BenchmarkSOCKSUDPTargetBatch` |
| 节点源 | 流式读取、记录/列数/候选上限、解码缓冲复用；`BenchmarkNodeSource/Streaming`、`BenchmarkNodeSourceExcessColumns` |
| Geo 响应 | 1 MiB 响应上限、按需字段解析、整数精确校验；`BenchmarkGeoResponse` |
| 控制客户端 | 1 MiB 完整响应上限、拒绝截断成功和重定向；`TestControlResponseBoundaries` |
| profile 校验 | 固定字段数，过量参数及时拒绝；`BenchmarkProfileValidation` |
| 配置发布 | 单份事务快照、相同内容不重写；`BenchmarkPublishChanged`、`BenchmarkPublishUnchanged` |
| 文件复制 | 输入与输出共用 32 KiB 池化缓冲；`BenchmarkCopyFileContent` |
| 远端推送 | 同批快照、单文件 16 MiB、响应 4 MiB、HEAD 存在性检查；`BenchmarkRemoteAcknowledgement` |
| 核心更新 | 流式摘要，不持有完整配置副本；`BenchmarkCoreConfigValidation` |
| 客户端配置 | 只创建当前范围需要的规则和选择器；`BenchmarkClientConfig` |
| VMess URI | 直接编码 JSON，省去无用的 URI 字段构造；`BenchmarkVMessURI` |

测量时固定输入、工具链、机器及并行度；性能测试与 race 分开执行。并发与网络变更须保留
TCP 半关闭、UDP 双向续期、源端口稳定、退出回收及未经验证不转发的行为。
完整测试、Linux netns/防火墙/systemd 专项和发行包构建命令统一维护在
[开发文档](development.md)，不在此复制。

## 本地文件整理

临时源码、日志和基准产物统一放在 `.test/`；工具放在 `.tools/`，最新发行包放在
`.build/`。验收完成后可清理 `.test/` 中的可再生缓存和临时文件，以及发行包已包含的
`.build/release/.bin/` 中间程序；开发脚本和构建命令会在下次运行时重建所需目录。
避免把临时上游源码放在普通目录，防止被 `go test ./...` 扫描。自动删除若受执行策略限制，统一保留在
`.test/obsolete-artifacts/` 供手动删除；其中内容不参与构建、测试或发行打包。
