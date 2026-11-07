# 性能与稳定性验证

[返回项目首页](../README.md) · [验证命令](development.md) · [模块边界](architecture.md)

本文保留当前结论、可复现命令和测量边界。逐轮历史记录见
[c85c2f4 时的优化日志](https://github.com/sck03/sing-box-vpn/blob/c85c2f4/docs/optimization.md)，
不再把已过期的临时目录、旧实现和重复验收记录混入当前说明。

## 本轮修复 · 2026-11-07

基线提交 `c85c2f4`。修改前回归复现：WS 设置接受 `/ws?ed=2048`、百分号转义和控制字符，
即使真实核心配置检查通过，也能发布无法完成 WebSocket 握手的订阅。

- `websocket.go` 集中路径检查和 Host 选择；菜单修改与导出共用校验，错误输入不发布。
  路径以 `/` 开头、最多 255 个 UTF-8 字节，保留合法中文路径，拒绝查询、片段、转义、
  反斜杠、空白及控制字符。普通节点和 VPNGate 仍独立保存。
- `uri.go` 的查询参数直接写入 Builder，去掉逐项临时字符串和切片；VMess 单独生成
  Base64 JSON，省去没有使用的 URI 地址、名称转义和查询参数构造。
- HTTP 订阅的实际请求回归覆盖 IPv4、IPv6 和取消退出。默认 `tcp` 通配监听支持双栈，
  无需增加监听器、常驻协程或旧配置兼容分支。

官方最新稳定版核对为 sing-box 1.14.4、Mihomo 1.19.33，继续使用当前工具链 Go 1.27.3。
真实订阅验收直接读取发布后的文件，覆盖两套原生五协议和三协议 URI 导入的 52 条 TCP/UDP
链路；两套 VMess 使用不同的中文路径。Mihomo URI 对 VMess/TUIC 自签 TLS 的限制见
[兼容性说明](compatibility.md)，不能把成功解析等同于成功连接。

本地全量 `go test -race -count=1 ./...`（启用持续单向 UDP）、52 条真实协议链路、
`go vet`、gofmt、Bash 语法及 ShellCheck 通过。Linux amd64/arm64 发行包构建及 SHA256
核对通过；本轮本地未启动 Linux VM，Linux 专项由 GitHub CI 执行。

## 本轮分配测量

Windows amd64、Intel i5-9400F、Go 1.27.3、GOMAXPROCS=6，非 race 各三次。
`BenchmarkVMessURI` 使用同一中文/emoji 名称、独立 Host/SNI 和带参数路径的编码输入；
这里衡量编码器，路径校验属于本地配置入口，通用编码器仍按原值输出。

| 局部指标 | 修改前 | 修改后 |
| --- | ---: | ---: |
| 分配内存（中位数） | 2,023 B/op | 1,892 B/op |
| 分配次数 | 10/op | 7/op |

分配次数减少 30%，临时分配内存减少约 6.5%。这些是订阅编码的局部指标，不代表转发
吞吐、连接延迟或整机 RSS；未依据微基准宣称 VPN 网速提升。

```bash
go test -run '^$' -bench '^BenchmarkVMessURI$' -benchmem -count=3 ./internal/manage
```

## 持续维护的资源边界

| 流程 | 约束与可用基准 |
| --- | --- |
| 连接准入 | 已接入连接与待拨号共用容量，满载直接拒绝；`BenchmarkManagerOpenAtCapacity` |
| UDP 转发 | 独立目标队列、缓冲复用、有界字节预算；`BenchmarkSOCKSUDPRoundTrip`、`BenchmarkSOCKSUDPTargetBatch` |
| 节点源 | 流式读取、候选数量限制、解码缓冲复用；`BenchmarkNodeSource/Streaming` |
| profile 校验 | 固定字段数，过量参数及时拒绝；`BenchmarkProfileValidation` |
| 配置发布 | 单份事务快照、相同内容不重写；`BenchmarkPublishChanged`、`BenchmarkPublishUnchanged` |
| 核心更新 | 流式摘要，不持有完整配置副本；`BenchmarkCoreConfigValidation` |
| 客户端配置 | 只创建当前范围需要的规则和选择器；`BenchmarkClientConfig` |

测量时固定输入、工具链、机器及并行度；性能测试与 race 分开执行。并发与网络变更须保留
TCP 半关闭、UDP 双向续期、源端口稳定、退出回收及未经验证不转发的行为。
完整测试、Linux netns/防火墙/systemd 专项和发行包构建命令统一维护在
[开发文档](development.md)，不在此复制。

## 本地文件整理

历史 `待手动删除/` 含上游 Go 源码；Git 虽然忽略它，`go test ./...` 仍会扫描并因中文
导入路径报错。其 67 个遗留文件已隔离到 `.test/obsolete-artifacts/`，不再参与包扫描。
递归删除被执行策略拒绝，因此原始归档仍在磁盘上；可用工具、缓存和最新发行包继续保留。
后续临时源码、日志和基准产物一律放入 `.test/`，不再建立工作区顶层的历史源码目录。
