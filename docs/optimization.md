# 性能与稳定性验证

[返回项目首页](../README.md) · [验证命令](development.md) · [模块边界](architecture.md)

本文记录当前实现和验证边界；以往的分配测量与修复记录见
[17deecd 时的优化日志](https://github.com/sck03/sing-box-vpn/blob/17deecd/docs/optimization.md)。

## 本轮修复 · 2026-11-08

基线提交 `17deecd`。节点源已有 16 MiB 响应限制，但标准 CSV 解析器会按字段数量扩容。
一个有效节点后附加约 256 KiB 的逗号行，即可产生约 38.5 MB 临时分配；旧实现还会将该
异常来源当作刷新成功，替换原缓存。单条超长记录和前导行也会产生不必要的分配。

新增独立的 `csv_bounds.go`，在标准库解析之前限制记录最多 2 MiB、64 列；前导和表头
改用固定 4 KiB 缓冲读取。超限拒绝整份来源，沿用已有内存/磁盘缓存回退。引号转义、
多行字段、CRLF 和前导 Unicode 空白仍由标准库解析；边界模块仅跟踪结构，不重复实现语法。
扫描按引号、换行分段，并批量统计逗号，减少正常 Base64 profile 的逐字节处理开销。

同机 Windows amd64、Go 1.27.3、i5-9400F、默认并行度 6，`-benchmem -count=3` 中位数：

| 输入 | 修改前 | 修改后 |
| --- | --- | --- |
| 一个有效节点 + 262,144 个逗号 | 约 313.7 ms / 38,532,764 B | 约 7.32 µs / 10,795 B |
| 250 个节点，每个证书占位内容 8 KiB | 约 4.52 ms / 7,991,929 B | 约 4.62 ms / 7,992,037 B |

异常多列输入分配下降约 99.97%，正常列表耗时中位数增加约 2.1%，分配基本持平。
异常输入的旧实现完整解析后成功返回，新实现提前拒绝，因此耗时反映拒绝路径的改进；
这不是代理转发吞吐或整机 RSS 测量。复现命令：

```bash
go test ./addons/vpngate/gateway -run '^$' \
  -bench 'BenchmarkNodeSourceExcessColumns$|BenchmarkNodeSource$/Streaming$' -benchmem -count=3
```

本地全量 `go test -race -count=1 ./...`（启用持续单向 UDP）、52 条真实协议链路、
`go vet`、gofmt、Bash 语法及 ShellCheck 均通过。Linux amd64/arm64 发行包构建通过，
Linux netns、防火墙和 systemd 专项由推送后的 GitHub CI 执行。

## 持续维护的资源边界

| 流程 | 约束与可用基准 |
| --- | --- |
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
