# 开发与发布

当前新增验证入口：`go test ./internal/manage -run 'TestInitializeRejects|TestUsageSurvives|TestPanelStreams|TestForwarded'`；`npm test` 包含页面/草稿、可靠性、表格质量、创建后绑定与分享工作流、页面契约、导航与延迟加载六组。数据库使用 schema 13，所有旧数据库及其备份明确拒绝。

`scripts/test-panel-navigation.mjs` 覆盖角色过滤、搜索定位、空/非法路由、同页标签的前进/返回与表单复用、延迟读取合并与取消、错误重试、权益初始读取预算、稀疏订阅草稿和 SSE 启动。`TestPanelEphemeralDevelopmentListener` 实际启动回环随机端口、请求能力接口并关闭服务，同时拒绝公网随机端口；设置校验仍拒绝保存端口 0。`TestPanelHealthSurvivesSaturatedRequests` 验证 32 个业务请求占满限额时，本机健康检查仍能准确判断 worker 就绪状态。

当前新增公网设置和服务登记回归：`go test ./internal/manage -run 'TestPanelAccess|TestPanelSettings|TestShareRejects|TestServiceCatalog|TestCertificateGarbage'`；管理员最低 8/最高 72 UTF-8 字节，`TestEightBytePasswordBoundary` 覆盖创建、登录、修改和重置。前端契约回归包含导航折叠记忆、当前分组展开、公网访问方式切换和先预览后保存；分享回归确认随机 ID 与默认继承权益策略。

`GET /api/panel/settings` 返回 `{options,active,revision,restart_required}`；`POST /api/panel/settings/preview` 接受 `{options}` 并只校验；`POST /api/panel/settings` 接受 `{options,revision}`，旧 revision 返回 409。全部要求管理员会话，写入要求 CSRF；不保存未校验配置。救援 CLI `panel configure` 从 stdin 接受相同 PanelOptions。`/api/capabilities.services` 统一登记服务及工作区；服务动作只接受 `sb-core/sb-vpngate-core/sb-vpngate-gateway/sb-subscription/sb-argo`，没有旧名称别名。

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

上述构建只在开发机或 CI 执行，VPS 安装直接使用 README 中的一键入口。需要部署尚未发布的分支时，将构建产物复制到 VPS 后执行：

```bash
VERSION=v5.0.0 # 改成要安装的发行版本
tar -xzf "sing-box-vpn-${VERSION}.tar.gz"
cd sing-box-vpn
sudo bash install.sh --local
```

已有当前平台安装会自动切换为升级，也可指定 `--local --upgrade`。安装器与 CI 接受标准正式标签 `vMAJOR.MINOR.PATCH`，例如 v5.0.0、v6.2.3，主版本不决定数据兼容性。预发布后缀、缺失版本段、前导零等无效标签会被拒绝。

正式发行需包含完整压缩包及 `SHA256SUMS`。安装器验证唯一校验值、归档路径/类型，以及当前架构的管理器、受管核心和维护脚本；本地包安装也检查同一内容约定。数据格式继续由平台验证，不提供旧数据迁移。无参数打开菜单，`install`/`upgrade` 可直接执行，`uninstall --yes` 用于显式无交互卸载。`SB_ADDRESS` 和 `SB_ADMIN_JSON_FILE` 保留自动化安装支持；JSON 文件须由管理员以 0600 权限保护并在使用后删除。

菜单从管理器 `--version` 读取已安装版本，并限制版本查询等待时间。在线安装将当前入口的函数与固定仓库地址保存到解压目录，再交由发行包安装；不会从已消费的管道重新读取脚本，也不会把管理员输入或环境中的秘密写进入口。`.platform-v4` 和 systemd 所有权标记标识平台格式，与 Release 主版本无关。

交互管理员输入通过 `panel --admin-stdin init/reset-admin` 读取两个 NUL 分隔字段，账号密码不经命令参数、环境变量或 Python 转义。默认 CLI 仍接受原来的 stdin JSON。

证书只由平台内置的 lego v5.6.0 负责申请和续期，使用 HTTP-01、Let's Encrypt、ECDSA P-256。平台保留每 12 小时检查和不足 30 天续期策略；已申请结果缓存于 `acme/lego/`，服务发布失败后复用结果重试。账户私钥、登记和缓存证书以 0600 原子保存；备份恢复缺少登记时自动补登记。证书提供方仅接受 `acme` / `import`，不接受旧 `certbot` 标记或提供迁移。

受管核心不启用上游 `with_acme` 或 Cloudflare Origin CA 提供器。发布器按 Linux amd64/arm64 的实际构建依赖检查，拒绝链接 CertMagic/acmez；sing-box 上游模块元数据中的可选依赖不等于进入发行二进制。

Windows 开发先 `. ./scripts/dev-env.ps1`。真实链路设置 `SING_BOX_BINARY`、`MIHOMO_BINARY`、`MANAGED_CORE_BINARY`；然后：

```bash
RUN_UDP_IDLE_TESTS=1 go test -race -timeout=25m -count=1 ./...
MANAGED_CORE_BINARY="$PWD/.build/managed-core" go test -race -count=1 -run '^(TestManagedAccountIsolation|TestRealFiveProtocolsTCPUDP)$' ./addons/vpngate/gateway
MANAGED_CORE_BINARY="$PWD/.build/managed-core" go test -race -count=1 -run '^TestCredentialPublicationRetryDoesNotRepeatRotation$' ./internal/manage
bash scripts/test-program-upgrade.sh
bash scripts/test-installer.sh
shellcheck --severity=warning sb.sh install.sh lib/*.sh scripts/*.sh
```

Linux root 专项通过 `RUN_NETNS_TESTS=1`、`RUN_FIREWALL_TESTS=1`、`RUN_SERVICE_TESTS=1`、`RUN_STORAGE_TESTS=1` 启用，测试定义明确需要的二进制环境变量。仅使用专门 VM，真实 systemd/firewall 测试会创建系统资源。

`npm test` 执行六组回归，覆盖重复提交、客户端字段联动、短暂断网、用户页路径前缀、状态搜索、焦点保留、单位换算、授权分页与导航加载。Mihomo 分流配置检查需要 `MIHOMO_HOME/Country.mmdb`；缺失时返回未完成核心校验的明确诊断，避免在预览时下载地理数据库。原生核心校验与 sing-box 节点连通性检查分别报告，不以其中之一代替商业客户端验收。

本地面板使用独立目录：manager `panel --root <空目录> --core <官方核心> --runtime <受管核心> --address 127.0.0.1 --no-system init`，stdin 为 username/password JSON；随后同样参数 `recover`、`serve`。仅在 `serve` 命令之前加 `--listen 127.0.0.1:0` 自动分配开发端口，日志返回实际 URL；`init` 和 `configure` 保存的端口必须为 1–65535。`--no-system` 仅用于开发，系统动作明确拒绝。Windows 的 Unix socket 测试需要较短的 TEMP/TMP 路径，例如项目下 `.test/tmp`。

CI 与本地共用自动验收入口，无需手动设置测试密码或启动服务：

```bash
GATEWAY_BINARY="$PWD/.build/yg-vpngate" \
SING_BOX_BINARY=/path/to/official/sing-box \
MANAGED_CORE_BINARY="$PWD/.build/managed-core" node scripts/test-panel-local.mjs
```

Windows 先加载 `scripts/dev-env.ps1`，将 `GATEWAY_BINARY` 和 `MANAGED_CORE_BINARY` 环境变量设为相应 `.exe` 路径。入口在 `.test/` 创建独立目录，初始化保存有效默认端口，启动时覆盖为回环随机端口，并通过 `/healthz` 等待 worker 就绪。启动预算 20 秒、整体预算 5 分钟；失败输出最多 64 KiB 面板日志并保留数据，成功停止子进程后删除临时目录。中断会取消子进程，停止超时则强制终止。

可对该独立面板运行真实 HTTP 工作流：设置 `PANEL_TEST_URL`、`PANEL_TEST_USERNAME`（默认 reviewadmin）和 `PANEL_TEST_PASSWORD`，执行 `node scripts/test-panel-api.mjs`。脚本只接受回环 URL，并在创建数据前确认服务均处于 no-system 状态。验证绑定/导出、下载次数、跨站分享、完整候选预览和任务应用；不能指向真实安装。

配置预览统一为 `POST /api/config/preview`，请求是完整 PlatformConfig；返回 `valid`、普通服务端脱敏配置、输出列表与诊断。旧 `/api/subscriptions/preview` 已删除。公开 `/api/capabilities` 提供协议、网关范围与密码字节范围。节点文档 schema=2、订阅 schema=2、平台 schema=4；备份容器格式仍为 7，但只接受 schema 13 数据库。

当前数据库 schema 13、备份格式 7。不复用早期开发目录或旧数据库；应用配置 schema 与数据库版本分别维护。对外 CLI 保留 panel、核心 prepare/check、firewall、下载监听和网关运行模式，旧 config/accounts/subscriptions/asset/remote 命令不再暴露。

账户策略存储为独立字段，备份结构校验随 schema 更新；所有旧 schema 数据库及其备份明确拒绝。新增回归覆盖配置快照、纯账户订阅自动恢复、模板省略字段、Argo 日志滚动、BBR 部分失败重试、任务详情权限、分页边界、日期精度及分享结果窗口。`Task.details` 是服务端生成的详情访问描述，前端不再维护任务类型白名单。

旧 config/subscriptions 命令适配器只保留在 `legacy_*_test.go` 中服务测试夹具，不编入生产。旧 remote 命令和 remote.json 通知旁路已移除；使用平台订阅分发资源及 webhook。新增授权列表接口：`GET /api/grants?q=...&offset=0&limit=20` 返回 `{items,total,offset,limit}`，limit 范围 1–100，条目含统一 `state`；没有旧数组接口兼容层。

账户列表同样返回分页对象：`GET /api/accounts?q=...&offset=0&limit=20&sort=created&direction=asc`。搜索 ID、名称、订阅者 ID；sort 仅允许 created/name/id，direction 仅允许 asc/desc，offset 为 0–100000。账户 DTO 包含 `subscriber_disabled` 和有效 `state`。管理员选择器使用 `GET /api/account-options` 的 `{id,name}[]`（账户总量上限 1000）以及 `GET /api/inbound-options?scope=normal|vpngate` 的已应用启用入口；不使用列表第一页代替所有选项。未配置 VPNGate 时入口选项为空数组。

主机升级调用 `panel --root <目录> wait-runtime`，stdin 为 `{"instance":"normal","started":<Unix纳秒>}`，等待 started 不早于本次重启的近期心跳，最长 20 秒。重启助手另外调用 `wait-panel` 验证实际本机监听/TLS 和 worker 就绪，最长 30 秒；随后原子保存 `program-upgrade.json` 并调用 `complete-upgrade`。收据按任务 ID 和目标版本匹配，启动可重复消费，成功、回滚和回滚失败均有终态。等待阶段保留跨重启，20 分钟无确认结果则失败。`recover-services` 在打开数据库之前执行；服务变更与升级共用恢复文件锁。

重构回归：`go test ./internal/manage -run 'TestUpgradeReceipt|TestShareReadiness|TestInvalidPanelInitialization|TestPanelHealth|TestSubscriptionSchedule|TestSubscriptionHTTPS|TestPanelAdvanced|TestRestorePreviewUsesArchived'`，TLS 缓存和传输策略单独运行 `go test ./internal/tlsconfig ./internal/publicaccess`。`BenchmarkSubscriptionScheduleRead` 对照完整投影读取与调度元数据读取；`npm test` 覆盖 gRPC/未知传输保留、共用 TLS 字段、任务清理与保留详情提示。真实浏览器覆盖鼠标、键盘、触屏、权益编辑、独立服务链接和访问方式。

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

依赖更新后运行 `python scripts/update-notices.py` 重建许可清单。Linux 安装验收使用 `SB_TEST_PASSWORD=... python3 scripts/test-platform-linux.py`，仅在可丢弃安装执行：脚本会创建权益、暂停服务和制造端口冲突。这里的 Python 只供开发和验收使用，生产安装器不安装它。

CI 执行全量 race、真实客户端矩阵、ShellCheck、升级状态测试、Linux 隔离与防火墙、双架构构建。push 分支只验证和上传构建产物；仅 tag 或显式 workflow_dispatch publish 才发布 Release。不要把分支推送描述为已部署。

CI 另执行 `scripts/test-install-ownership.sh` 和真实面板 API 工作流。所有回滚单元测试应包含非项目同名单元和符号链接，避免只在空宿主环境中证明成功路径。

`scripts/test-platform-linux.py` 的全新安装分享验收先确认默认回环地址拒绝创建令牌，再在可丢弃安装中配置测试用 HTTPS 下载源站，验证分享和次数限制，随后恢复原配置。测试下载仍使用本机面板，不访问该测试域名；不能通过放宽公网地址校验迁就旧夹具。

官方 Release 元数据可通过 `SBVPN_GITHUB_TOKEN` 使用可选的 GitHub API 令牌，CI 使用自带的临时只读令牌以避免共享出口的匿名限流。该令牌仅用于 HTTPS 的 `api.github.com` 上 sing-box、cloudflared 和本项目的 Release 元数据路径，不发送给下载地址或其他远端，不写入配置或日志。

CI 的独立下载验证和全新安装步骤均显式传递该只读令牌；通过 sudo 启动安装器时使用 `--preserve-env=SBVPN_GITHUB_TOKEN`，避免安装阶段退回匿名元数据请求。

验收证据写入 [实施记录](unified-management-status.md)，区分单元测试、真实网络测试、系统测试和未测范围。历史性能数据不能充当新版本提升结论。格式化 Go 使用 gofmt，前端使用 Prettier；发布不依赖前端构建工具。

## 本地清理与 main 交付

真实 HTTP 测试会校验权益改名、实际到期字段、额度与次数拒绝、归档 revision 冲突以及归档筛选。完成 `node scripts/test-panel-api.mjs` 后，在同一临时面板上运行 `python scripts/test-panel-browser.py`。沿用 `PANEL_TEST_URL` 和测试管理员环境变量，需要 Python Playwright 和 Chromium。通过 `CHROME_BINARY` 可指定已有 Chrome 可执行文件。该脚本使用真实鼠标、键盘和触屏事件，并验证 390 × 844 视口中首条权益的位置。

共用启动器支持 `PANEL_BROWSER_TEST=1 node scripts/test-panel-local.mjs` 自动续跑浏览器验收；`PYTHON_BINARY` 可指定开发 Python，`PANEL_TEST_SCREENSHOT_DIR` 保存桌面/手机及新增设置页面截图。Linux 公网访问专项为 `sudo env RUN_ACCESS_MAINTENANCE_TESTS=1 SB_TEST_ROOT=/etc/s-box SB_TEST_PASSWORD=... python3 scripts/test-access-maintenance-linux.py`，仅在可丢弃 VM 执行：检查内置订阅 TLS、面板重启/回滚、任务处理/删除/保留以及项目日志清理不会影响全局日志。HTTP 切换到 HTTPS 使用不同 Cookie 名称，客户端需重新登录。此专项已接入全新安装 CI。

停止测试进程和专用 VM 后，可删除 `.test/` 中的日志、临时数据库、测试二进制及 Go 缓存，`.build/release/.bin/` 中的打包中间文件，以及 `scripts/__pycache__/`。下一次加载 `scripts/dev-env.ps1` 会重建缓存目录；Go 会重新下载和编译所需模块。

保留当前 `.build/release/*.tar.gz` 与 `SHA256SUMS`、在用工具链、回归测试和第三方许可证。Linux VM 的 `.tools/linux-validation.qcow2` 依赖 `.tools/ubuntu-noble-amd64.img`，两者均须保留；已经展开或复制到 VM 的重复下载归档可以清理。

无法自动删除的条目集中移动到 `.cleanup-manual/` 后手动删除；仅有残留时创建此目录。它已被 Git 忽略，不能提交到 main 或打入发行包。Windows 删除/移动使用原生 PowerShell，并先确认实际绝对路径位于项目目录内。

交付前核对本地分支和远端 heads，将有效改动提交到 main；仅在确认其他分支提交已合入后删除那些分支。`git fetch --all --prune` 清除失效的远端跟踪引用。保留正式版本标签，分支清理不删除 Release。main 推送触发 CI 验证；用户可安装的版本仍需通过既有发布流程上传完整归档和 SHA256SUMS。
