# 性能与稳定性验证

[返回项目首页](../README.md) · [验证命令](development.md) · [模块边界](architecture.md)

本文记录当前实现和验证边界；以往的分配测量与修复记录见
[4012897 时的优化日志](https://github.com/sck03/sing-box-vpn/blob/4012897/docs/optimization.md)。

## 本轮修复 · 2026-11-10

基线提交 `4012897`。UDP 关联原先在每次读取前刷新空闲期限，短包、分片以及其他源端口的
数据也能让空闲关联持续占用连接、套接字和接收缓冲。新增真实网络回归在修改前复现四类
流量持续 65 秒后连接仍未退出；修复后只由合法请求或回复续期，空闲 60 秒后正常回收。
零目标端口提前拒绝，不再为这类包反复创建队列、协程或 worker 连接。丢弃明显无效的包
发生在认证锁之前，也不再为它们刷新套接字计时器。退出等待控制连接读取与全部目标转发结束。

合法单向上传和下载仍分别持续 65 秒验证，保留稳定源端口。测试没有缩短生产空闲期限。
本轮不增加流量统计协程、全局缓存或第三方 Go 依赖。

HTTP 订阅原先把空文件和超过存储层 16 MiB 边界的文件返回为成功；回归覆盖两类节点、
GET/HEAD 和四种订阅文件。现在返回 503，避免空订阅被客户端当作有效更新。
文件仍以流式方式发送，检查大小不读入完整内容。

OpenClash 新增原生 `proxies.yaml`，与完整 Mihomo 配置共享节点转换，只输出 `proxies`。
两套文件沿用现有原子发布、访问令牌和卸载流程，避免引入独立配置状态与转换依赖。
验收通过项目自身 HTTP 订阅加载 provider，使两套五协议的真实 TCP/UDP 总数由 52 增至 72。
详细支持边界和上游核对见[兼容性说明](compatibility.md)与[技术说明](upstream.md)。

Windows amd64、Go 1.27.3、i5-9400F、默认并行度 6，当前 UDP 基准连续三轮均为
**0 allocs/op**。其中单目标 1400 字节往返为 0 B/op；8 目标批次测得 1–19 B/op，
含会话与后台工作摊销。修改前也为 0 allocs/op，本次改善的是无效流量下的资源持有与
无用操作，不据此宣称正常代理吞吐或整机 RSS 提升。复现命令：

```bash
go test ./addons/vpngate/gateway -run '^$' -bench '^BenchmarkSOCKSUDP(RoundTrip|TargetBatch)$' -benchmem -count=3
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
