# 性能与稳定性验证

[返回项目首页](../README.md) · [验证命令](development.md) · [模块边界](architecture.md)

本文记录当前实现和验证边界；以往的分配测量与修复记录见
[5bca219 时的优化日志](https://github.com/sck03/sing-box-vpn/blob/5bca219/docs/optimization.md)。

## 本轮修复 · 2026-11-09

基线提交 `5bca219`。Geo 响应先解码到 `map[string]any`，ASN 会经过浮点舍入：
`42.00000000000000001` 被当作 ASN 42，可能意外通过允许列表。字符串形式的机房
标记也会被静默忽略。响应中的无关对象和数组全部展开，增加临时内存与 GC 压力。

新增 `geo_response.go`，按专用结构解析策略字段，ASN 直接按原始十进制整数校验，
已知字段类型错误时拒绝整份响应；无关字段只做 JSON 语法校验，不创建对应对象树。
出口探测、重试和缓存仍由 `geo.go` 负责，非法响应不进入缓存，修正后可以重新验证。

控制客户端原先将超过 1 MiB 的响应截断后作为成功输出，还会跟随重定向；错误响应没有
`error` 字段时只返回空错误。现在与探测请求共用有界读取，超限和断流均在输出前报错，
拒绝重定向及其他非 2xx 响应，并保留有效错误信息或 HTTP 状态码。

同机 Windows amd64、Go 1.27.3、i5-9400F、默认并行度 6，`-benchmem -count=3` 中位数：

| 输入 | 修改前 | 修改后 |
| --- | --- | --- |
| 125 B 常见 Geo 响应 | 5.65 µs / 1,281 B / 27 次分配 | 1.94 µs / 289 B / 5 次分配 |
| 约 240 KiB Geo 响应，含 8,191 个无关对象 | 11.47 ms / 4,093,074 B / 90,152 次分配 | 1.12 ms / 295 B / 5 次分配 |

常见输入临时分配下降约 77%，大量无关字段输入下降约 99.99%。同一基准先在原解析器上
运行，再对最终实现测量；输入字节在计时前创建，测试与竞态检查、构建分开执行。
结果仅衡量 Geo JSON 解析，不包括 HTTP 读取、网络、TLS 或代理转发，不能据此推断
整机 RSS 或代理吞吐量。当前实现复现命令：

```bash
go test ./addons/vpngate/gateway -run '^$' -bench '^BenchmarkGeoResponse$' -benchmem -count=3
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
| Geo 响应 | 1 MiB 响应上限、按需字段解析、整数精确校验；`BenchmarkGeoResponse` |
| 控制客户端 | 1 MiB 完整响应上限、拒绝截断成功和重定向；`TestControlResponseBoundaries` |
| profile 校验 | 固定字段数，过量参数及时拒绝；`BenchmarkProfileValidation` |
| 配置发布 | 单份事务快照、相同内容不重写；`BenchmarkPublishChanged`、`BenchmarkPublishUnchanged` |
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
