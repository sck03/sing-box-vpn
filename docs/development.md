# v4 开发与发布

当前新增验证入口：`go test ./internal/manage -run 'TestInitializeRejects|TestUsageSurvives|TestPanelStreams|TestForwarded'`；`npm test` 包含页面/草稿、可靠性、表格质量、创建后绑定与分享工作流五组。数据库使用 schema 11，所有旧数据库及其备份明确拒绝。

本轮新增 `internal/database/database_test.go` 的连接替换/超时外键回归，以及 `platform_contracts_test.go` 的共享参数、公开分享、候选校验、WARP、缓存持久化与取消结果测试。前端第五组 `scripts/test-panel-contracts.mjs` 覆盖 UTF-8 密码、网关范围、协议字段、移动排序与操作员网关控制。

`POST /api/country` 请求为 `{country,config}`，config 是完整当前草稿；返回候选配置，不修改数据库。前端拒绝用迟到结果覆盖重载或继续编辑后的草稿。可信反代必须追加真实客户端地址到 X-Forwarded-For，并设置 X-Forwarded-Proto=https。

并发持久预留基准：`go test ./internal/accounting -run '^$' -bench '^BenchmarkConcurrentDurableReservations$' -benchmem -count=3 -cpu=4`。保持 WAL/FULL 与 64 KiB 单池边界，结果不能等同端到端代理带宽。

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
RUN_UDP_IDLE_TESTS=1 go test -race -timeout=25m -count=1 ./...
MANAGED_CORE_BINARY="$PWD/.build/managed-core" go test -race -count=1 -run '^(TestManagedAccountIsolation|TestRealFiveProtocolsTCPUDP)$' ./addons/vpngate/gateway
MANAGED_CORE_BINARY="$PWD/.build/managed-core" go test -race -count=1 -run '^TestCredentialPublicationRetryDoesNotRepeatRotation$' ./internal/manage
bash scripts/test-program-upgrade.sh
shellcheck --severity=warning sb.sh install.sh lib/*.sh scripts/*.sh
```

Linux root 专项通过 `RUN_NETNS_TESTS=1`、`RUN_FIREWALL_TESTS=1`、`RUN_SERVICE_TESTS=1`、`RUN_STORAGE_TESTS=1` 启用，测试定义明确需要的二进制环境变量。仅使用专门 VM，真实 systemd/firewall 测试会创建系统资源。

`npm test` 执行页面/草稿、可靠性及表格质量与工作流五组回归，覆盖重复提交、客户端字段联动、短暂断网、用户页路径前缀、状态搜索、焦点保留、单位换算和授权分页。Mihomo 分流配置检查需要 `MIHOMO_HOME/Country.mmdb`；缺失时返回未完成核心校验的明确诊断，避免在预览时下载地理数据库。原生核心校验与 sing-box 节点连通性检查分别报告，不以其中之一代替商业客户端验收。

本地面板使用独立目录：manager `panel --root <空目录> --core <官方核心> --runtime <受管核心> --address 127.0.0.1 --no-system init`，stdin 为 username/password JSON；随后同样参数 `recover`、`serve`。`--no-system` 仅用于开发，系统动作明确拒绝。

可对该独立面板运行真实 HTTP 工作流：设置 `PANEL_TEST_URL`、`PANEL_TEST_USERNAME`（默认 reviewadmin）和 `PANEL_TEST_PASSWORD`，执行 `node scripts/test-panel-api.mjs`。脚本只接受回环 URL，并在创建数据前确认服务均处于 no-system 状态。验证绑定/导出、下载次数、跨站分享、完整候选预览和任务应用；不能指向真实安装。

配置预览统一为 `POST /api/config/preview`，请求是完整 PlatformConfig；返回 `valid`、普通服务端脱敏配置、输出列表与诊断。旧 `/api/subscriptions/preview` 已删除。公开 `/api/capabilities` 提供协议、网关范围与密码字节范围。节点文档 schema=2、订阅 schema=2、平台 schema=4；备份容器格式仍为 7，但只接受 schema 11 数据库。

当前数据库 schema 11、备份格式 7。不复用早期开发目录或旧数据库；应用配置 schema 与数据库版本分别维护。对外 CLI 保留 panel、核心 prepare/check、firewall、下载监听和网关运行模式，旧 config/accounts/subscriptions/asset/remote 命令不再暴露。

账户策略存储为独立字段，备份结构校验随 schema 更新；所有旧 schema 数据库及其备份明确拒绝。新增回归覆盖配置快照、纯账户订阅自动恢复、模板省略字段、Argo 日志滚动、BBR 部分失败重试、任务详情权限、分页边界、日期精度及分享结果窗口。`Task.details` 是服务端生成的详情访问描述，前端不再维护任务类型白名单。

旧 config/subscriptions 命令适配器只保留在 `legacy_*_test.go` 中服务测试夹具，不编入生产。旧 remote 命令和 remote.json 通知旁路已移除；使用平台订阅分发资源及 webhook。新增授权列表接口：`GET /api/grants?q=...&offset=0&limit=20` 返回 `{items,total,offset,limit}`，limit 范围 1–100，条目含统一 `state`；没有旧数组接口兼容层。

账户列表同样返回分页对象：`GET /api/accounts?q=...&offset=0&limit=20&sort=created&direction=asc`。搜索 ID、名称、订阅者 ID；sort 仅允许 created/name/id，direction 仅允许 asc/desc，offset 为 0–100000。账户 DTO 包含 `subscriber_disabled` 和有效 `state`。管理员选择器使用 `GET /api/account-options` 的 `{id,name}[]`（账户总量上限 1000）以及 `GET /api/inbound-options?scope=normal|vpngate` 的已应用启用入口；不使用列表第一页代替所有选项。未配置 VPNGate 时入口选项为空数组。

主机升级调用 `panel --root <目录> wait-runtime`，stdin 为 `{"instance":"normal","started":<Unix纳秒>}`，等待 started 不早于本次重启的近期心跳，最长 20 秒。`recover-services` 在打开数据库之前执行，独立于面板健康；服务变更与升级共用恢复文件锁。

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

CI 另执行 `scripts/test-install-ownership.sh` 和真实面板 API 工作流。所有回滚单元测试应包含非项目同名单元和符号链接，避免只在空宿主环境中证明成功路径。

官方 Release 元数据可通过 `SBVPN_GITHUB_TOKEN` 使用可选的 GitHub API 令牌，CI 使用自带的临时只读令牌以避免共享出口的匿名限流。该令牌仅用于 HTTPS 的 `api.github.com` 上 sing-box、cloudflared 和本项目的 Release 元数据路径，不发送给下载地址或其他远端，不写入配置或日志。

验收证据写入 [实施记录](unified-management-status.md)，区分单元测试、真实网络测试、系统测试和未测范围。历史性能数据不能充当新版本提升结论。格式化 Go 使用 gofmt，前端使用 Prettier；发布不依赖前端构建工具。
