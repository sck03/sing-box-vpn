# v4 开发与发布

Go 1.27.3，版本锁定于 versions.json。安装包包含 manager + managed-core 两个程序，每个提供 Linux amd64/arm64 静态构建，并携带项目源码及第三方许可证。前端使用原生 ES modules，无生产 Node 依赖。

```bash
go mod download
go test ./...
go vet ./...
npm ci && npm test
go build -tags with_quic,with_utls,with_wireguard -o .build/managed-core ./cmd/managed-core
go run ./cmd/release
```

Windows 开发先 `. ./scripts/dev-env.ps1`。真实链路设置 `SING_BOX_BINARY`、`MIHOMO_BINARY`、`MANAGED_CORE_BINARY`；然后：

```bash
RUN_UDP_IDLE_TESTS=1 go test -race -count=1 ./...
MANAGED_CORE_BINARY="$PWD/.build/managed-core" go test -race -count=1 -run '^(TestManagedAccountIsolation|TestRealFiveProtocolsTCPUDP)$' ./addons/vpngate/gateway
bash scripts/test-program-upgrade.sh
shellcheck --severity=warning sb.sh install.sh lib/*.sh scripts/*.sh
```

Linux root 专项通过 `RUN_NETNS_TESTS=1`、`RUN_FIREWALL_TESTS=1`、`RUN_SERVICE_TESTS=1`、`RUN_STORAGE_TESTS=1` 启用，测试定义明确需要的二进制环境变量。仅使用专门 VM，真实 systemd/firewall 测试会创建系统资源。

`npm test` 执行页面/草稿、可靠性及表格质量三组回归，覆盖重复提交、客户端字段联动、短暂断网、用户页路径前缀、状态搜索、焦点保留、单位换算和授权分页。Mihomo 分流配置检查需要 `MIHOMO_HOME/Country.mmdb`；缺失时返回未完成核心校验的明确诊断，避免在预览时下载地理数据库。原生核心校验与 sing-box 节点连通性检查分别报告，不以其中之一代替商业客户端验收。

本地面板使用独立目录：manager `panel --root <空目录> --core <官方核心> --runtime <受管核心> --address 127.0.0.1 --no-system init`，stdin 为 username/password JSON；随后同样参数 `recover`、`serve`。`--no-system` 仅用于开发，系统动作明确拒绝。

当前数据库 schema 7、备份格式 7。不复用早期开发目录或旧数据库；应用配置 schema 与数据库版本分别维护。对外 CLI 保留 panel、核心 prepare/check、firewall、下载监听和网关运行模式，旧 config/accounts/subscriptions/asset/remote 命令不再暴露。

旧 config/subscriptions 命令适配器只保留在 `legacy_*_test.go` 中服务测试夹具，不编入生产。旧 remote 命令和 remote.json 通知旁路已移除；使用平台订阅分发资源及 webhook。新增授权列表接口：`GET /api/grants?q=...&offset=0&limit=20` 返回 `{items,total,offset,limit}`，limit 范围 1–100，条目含统一 `state`；没有旧数组接口兼容层。

`internal/backuparchive` 单独负责格式 7（8 字节项目标记 + age 流 + tar），使用 scrypt 工作因子 16 并限制解密工作因子。禁止恢复前使用尚未完成认证的条目。新增测试覆盖密文损坏、截断、路径限制、并发发布锁、授权生命周期、任务预算、状态搜索与分页。

计量缓存基准（与 race、虚拟机启动和构建分开运行）：

```bash
go test ./internal/accounting -run '^$' -bench '^BenchmarkMeterTakeComplete$' -benchmem -count=3
```

该基准重复取用并退还已预留的 1 KiB，用于比较授权检查、锁和分配成本；不含 socket、额度补充和磁盘同步，不代表代理吞吐。审查回归还覆盖跨 Store 撤销、无变化不写检查点、lease 压缩后的重放拒绝、TCP 非对齐跨界、资源重建/304、大备份和禁用订阅者。

持久化热路径和 Linux 面板持续负载：

```bash
go test ./internal/accounting -run '^TestSQLiteWriterWait$' -bench '^Benchmark(ActiveAuthorization100|DurableMeter64KiB|SQLiteCommit)$' -benchtime=3s -count=3 -benchmem -v
SB_TEST_PASSWORD=... SB_LOAD_SECONDS=60 python3 scripts/measure-platform-linux.py
sudo env RUN_STORAGE_TESTS=1 go test ./internal/accounting -run '^TestLinux(DiskFullRecovery|KilledMeterPreservesReservation)$' -v
```

前者保持 WAL/FULL，输出每轮 100 个活跃授权、64 KiB 连续预留吞吐、提交 P50/P95 和包含 20 ms 人工竞争的写锁等待。后者只读访问已登录面板，报告请求吞吐、延迟、错误数和面板/核心 CPU/RSS。性能比较使用相同测试源码、相同次数和隔离环境；不能把计量器吞吐等同于网络端到端带宽。存储测试只填满自己的 8 MiB tmpfs，不填满宿主分区。

依赖升级先 `go list -m -u all`，优先正式稳定 tag；sing-box 的 fork/pseudo-version 配套依赖需随核心兼容验证。当前更新了 SQLite、加密、YAML、网络等稳定库；不以任意 master 代替协议适配组合。新增依赖更新 `third_party/modules/`、NOTICE 和兼容性记录。

依赖更新后运行 `python scripts/update-notices.py` 重建许可清单。Linux 安装验收使用 `SB_TEST_PASSWORD=... python3 scripts/test-platform-linux.py`，仅在可丢弃安装执行：脚本会创建权益、暂停服务和制造端口冲突。

CI 执行全量 race、真实客户端矩阵、ShellCheck、升级状态测试、Linux 隔离与防火墙、双架构构建。push 分支只验证和上传构建产物；仅 tag 或显式 workflow_dispatch publish 才发布 Release。不要把分支推送描述为已部署。

官方 Release 元数据可通过 `SBVPN_GITHUB_TOKEN` 使用可选的 GitHub API 令牌，CI 使用自带的临时只读令牌以避免共享出口的匿名限流。该令牌仅用于 HTTPS 的 `api.github.com` 上 sing-box、cloudflared 和本项目的 Release 元数据路径，不发送给下载地址或其他远端，不写入配置或日志。

验收证据写入 [实施记录](unified-management-status.md)，区分单元测试、真实网络测试、系统测试和未测范围。历史性能数据不能充当新版本提升结论。格式化 Go 使用 gofmt，前端使用 Prettier；发布不依赖前端构建工具。
