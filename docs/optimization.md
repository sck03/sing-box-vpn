# 性能与稳定性验证

[返回项目首页](../README.md) · [验证命令](development.md) · [模块边界](architecture.md)

本文记录当前实现和验证边界；以往的分配测量与修复记录见
[5c272d4 时的优化日志](https://github.com/sck03/sing-box-vpn/blob/5c272d4/docs/optimization.md)。

## 本轮修复 · 2026-11-10

基线提交 `5c272d4`。核心下载原先找到二进制后立即结束解包，未检查后续条目与 gzip 尾部。
回归在修改前复现了截断 gzip、错误 CRC、重复核心仍被接受，以及版本检查中取消后仍覆盖
候选文件。现在完成整份归档检查后才执行核心版本检查，发布前再次检查取消状态。

解包不再只约束选中的二进制：全部展开数据最多 512 MiB，包含跳过条目、tar 元数据和尾部
gzip 成员；单个核心最多 256 MiB。超大条目声明提前拒绝，读取过程按块检查预算和取消。
失败保留原候选并清理暂存目录。归档处理独立于版本选择、服务更新和回滚，便于单独维护。

核心与 cloudflared 共用下载大小、哈希校验和取消检查，已知超过 256 MiB 的响应不开始读取。
下载和解包复用存储层 32 KiB 缓冲，删除重复校验实现与逐次复制分配。私有暂存文件直接写入，
不引入多余的原子重写；不增加后台协程、兼容层或第三方 Go 依赖。

Windows amd64、Go 1.27.3、i5-9400F、默认并行度 6，4 MiB 核心样本经内存 HTTP 传输、
解包和文件落盘，前后三轮测量如下。样本包含核心前后的其他文件，版本检查使用替代实现。

| 指标 | 修改前 | 修改后 |
| --- | --- | --- |
| 每次分配字节 | 125,315–127,110 B | 57,911–58,971 B |
| 每次分配次数 | 122–123 | 143–144 |
| 每次耗时 | 5.37–7.72 ms | 4.70–5.45 ms |

分配字节减少约 54%；完整读取后续条目增加了一些小对象分配。磁盘耗时受缓存影响，
本测量不代表公网下载速度、代理吞吐或整机 RSS。复现命令：

```bash
go test ./internal/manage -run '^$' -bench '^BenchmarkCoreDownload$' -benchmem -count=3
```

测试、竞态检查和构建与测量分开执行；Linux netns、防火墙和 systemd 专项由 GitHub CI 执行。

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
| 核心更新 | 下载 256 MiB、完整展开 512 MiB、取消与校验后发布；`BenchmarkCoreDownload`、`BenchmarkCoreConfigValidation` |
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
