# 性能与稳定性验证

[返回项目首页](../README.md) · [验证命令](development.md) · [模块边界](architecture.md)

本文记录当前实现和验证边界；以往的分配测量与修复记录见
[ea6ecb1 时的优化日志](https://github.com/sck03/sing-box-vpn/blob/ea6ecb1/docs/optimization.md)。

## 本轮修复 · 2026-11-09

基线提交 `ea6ecb1`。客户端发送 TCP EOF 后，中继正确保留响应方向，但代理退出时只关闭
客户端连接。如果 worker 一直保持响应流打开，另一个复制方向仍阻塞在读取 worker，
导致代理服务无法完成退出，连接跟踪和相关资源也不能释放。主动取消和永久监听错误均可触发。

TCP 中继现在统一接收会话 context，取消时关闭双端并等待两个复制方向完成；普通 EOF
仍只执行半关闭，保留完整响应。SOCKS 入口和 worker 共用这条回收路径，UDP 继续保留自身
队列与关联生命周期。回归测试使用独立 worker 保持响应流打开：修改前两个退出场景均超时，
修改后均能退出且连接跟踪归零；正常半关闭响应验收继续通过。

文件复制原先只隐藏输入的 `WriteTo`，输出文件的 `ReadFrom` 仍会绕过已借出的 32 KiB
缓冲，再创建一份临时缓冲。现在同时隐藏两端的快捷接口，使备份、恢复和文件写入实际使用
存储模块的可复用缓冲；原有错误返回、原子发布和同步落盘流程不变。

同机 Windows amd64、Go 1.27.3、i5-9400F、默认并行度 6，`-benchmem -count=3`
的首次前后测量中位数：

| 输入 | 修改前 | 修改后 |
| --- | --- | --- |
| 8 KiB 文件复制 | 32,831 B / 2 次分配 | 32 B / 2 次分配 |
| 128 KiB 文件复制 | 32,814 B / 2 次分配 | 33 B / 2 次分配 |

每次复制的临时分配下降约 99.9%，接口包装的小额分配仍然存在。基准在计时前创建源文件
和目标文件，测量文件定位与复制，不含 fsync；测试、竞态检查和构建与测量分开执行。
复测的分配量仍为几十字节，但磁盘耗时波动明显，因此不据此推断吞吐提升、整机 RSS 或
生产磁盘性能。缓冲池仍可被 GC 回收，首次借用可能重新分配。当前实现复现命令：

```bash
go test ./internal/manage -run '^$' -bench '^BenchmarkCopyFileContent$' -benchmem -count=3
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
