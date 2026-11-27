# 开发与发布

2026-11-27 当前修复与定向验证见[实施报告](refactor-review.md)。本轮覆盖任务保留预算、订阅向导缓存生命周期、VPNGate 状态契约、隐藏任务页、独立后台维护及应用执行基线。历史验证记录只说明对应提交，不代表本轮执行完整验收。

本轮前端回归为 `npm test -- test-panel-state-sync.mjs test-panel-lifecycle.mjs test-panel-subscriptions.mjs test-panel-contracts.mjs test-panel-navigation.mjs`。原生表单校验必须使用 `checkValidity/requestSubmit` 验证，直接派发 submit 事件不能证明浏览器允许提交。

`maintenance_regression_test.go` 验证已发布且未加载凭据的空操作、执行基线持久化与重放，以及服务锁被占用时其他控制命令仍能被领取。该组和 controlstore 的任务恢复测试另做定向 race；有真实核心变量的批量发布、发布失败重试和应用回归见本轮验证记录。修改网关状态时同步 `vpngate.States()`，重新运行 `go run ./cmd/panel-contract`；前端 Record 映射会检查遗漏的状态。

## 本地目录与体积

本轮必要验证与清理结果以 [实施报告](refactor-review.md) 为准。普通可见页面的 JSDOM 夹具使用 `pretendToBeVisual: true`；隐藏页行为单独设置 `document.hidden` 验证。涉及表单保存的回归使用 `checkValidity/requestSubmit`。Unix socket 测试使用短名称临时目录，避免嵌套测试名称超过 Windows 地址长度限制。

相关定向入口：`npm test -- test-panel-quality.mjs test-panel-selection.mjs test-panel-contracts.mjs test-panel-state-sync.mjs test-panel-refactor.mjs`；队列与档案边界使用 `go test ./internal/manage -run 'TestQueuedTasksNotifyTheirOwnLane|TestCountryDeletionPreservesBoundProfiles'`。无需为这些修改执行长时间链路或完整测试套件。

源码不依赖旧数据库或测试运行目录。`.test/gocache`、`.test/gomodcache`、`.test/tmp` 是可重建缓存；完成验证并停止相关进程后可清理。`.test/verification` 中旧数据库、日志、截图和测试二进制也可删除，仅保留需要的验收摘要。

Windows 使用 `./scripts/clean-dev.ps1 -WhatIf` 预览，去掉 `-WhatIf` 清理 `.test`、`.build`、`.cleanup-manual`。加 `-Dependencies` 删除可由 `npm ci` 重建的 node_modules，加 `-Tools` 删除整个项目本地 `.tools`，包括 Go、核心校验器、Mihomo 和其他可重新下载的验证工具；只清 Python 浏览器依赖时用 `-BrowserTools`。脚本核对绝对路径、目录链接和目录内运行程序，无法读取的目录单独报告，不触及 `.git`、源码、文档示例或外部数据库。停止构建和测试后执行；不要把需要发布的唯一发行包留在待清理目录。

清理工具后，按 `versions.json` 安装 Go 到系统 PATH 或 `.tools/go`，执行 `npm ci` 恢复前端开发依赖。`scripts/dev-env.ps1` 优先使用本地 Go，再使用系统 Go，并核对固定版本；缓存统一进入 `.test`。真实协议验证另外准备 `SING_BOX_BINARY`、`MIHOMO_BINARY` 和 `MANAGED_CORE_BINARY`。运行中的 VPS 不依赖这些开发工具。

旧 Linux 测试虚拟机、基础镜像及依赖这些资产的 `linux-vm.ps1` 已移除。Linux 验收在 CI 或自行准备的隔离环境中执行，不依赖工作区私有镜像和 SSH 密钥。清理范围仅限项目的可重建开发目录，不扫描真实部署目录、用户配置或外部数据库。

任务 API 使用当前 `application.TaskCommand`，不接受调度器 `automatic` 或无关字段。增加操作时同步 `CommandFields`、校验和执行器，再运行 `go run ./cmd/panel-contract`；不要手工修改生成类型。调度器只通过私有入口发起自动刷新/投递。

`scripts/task-contracts.ts` 随 `npm run typecheck` 检查合法单条/批量绑定、轮换和服务命令，并要求编译器拒绝调度标记、无关字段、导出参数及错误动作值。账户命令的可选字段不会因为共用导出结构而变成无意义的必填项。

按入站重载的回归入口为 `TestManagedCredentialReload`（真实五协议、跨入站 TCP/UDP 连接、核心启动标识），`internal/runtime/reload_test.go` 还覆盖构造/启动恢复和失败关闭。修改凭据生命周期必须同时运行带四个发布标签的 runtime 竞态测试；单独 `go test ./...` 不替代该检查。

`npm test` 自动发现 16 组面板单元脚本，排除独立 API/本地服务启动器；每组最多 60 秒，进程不退出视为失败。可用 `npm test -- test-panel-quality.mjs test-panel-drafts.mjs` 仅执行受影响检查。新增草稿回归覆盖取消、重载、并发编辑、稳定 ID、权益迟到结果和退出登录；必要的后端回归为 `go test ./internal/manage ./internal/accounting -run 'TestCandidate|TestInboundBindings'`。数据库使用 schema 17，所有旧数据库及其备份明确拒绝。

`scripts/test-panel-navigation.mjs` 覆盖角色过滤、搜索定位、空/非法路由、同页标签的前进/返回与表单复用、延迟读取合并与取消、错误重试、权益初始读取预算、稀疏订阅草稿和 SSE 启动。`TestPanelEphemeralDevelopmentListener` 实际启动回环随机端口、请求能力接口并关闭服务，同时拒绝公网随机端口；设置校验仍拒绝保存端口 0。`TestPanelHealthSurvivesSaturatedRequests` 验证 32 个业务请求占满限额时，本机健康检查仍能准确判断 worker 就绪状态。

当前新增公网设置和服务登记回归：`go test ./internal/manage -run 'TestPanelAccess|TestPanelSettings|TestShareRejects|TestServiceCatalog|TestCertificateGarbage'`；管理员最低 8/最高 72 UTF-8 字节，`TestEightBytePasswordBoundary` 覆盖创建、登录、修改和重置。前端契约回归包含导航折叠记忆、当前分组展开、公网访问方式切换和先预览后保存；分享回归确认随机 ID 与默认继承权益策略。

`GET /api/panel/settings` 返回 `{options,active,revision,restart_required}`；`POST /api/panel/settings/preview` 接受 `{options}` 并只校验；`POST /api/panel/settings` 接受 `{options,revision}`，旧 revision 返回 409。全部要求管理员会话，写入要求 CSRF；不保存未校验配置。救援 CLI `panel configure` 从 stdin 接受相同 PanelOptions。`/api/capabilities.services` 统一登记服务及工作区；服务动作只接受 `sb-core/sb-vpngate-core/sb-vpngate-gateway/sb-subscription/sb-argo`，没有旧名称别名。

本轮新增 `internal/database/database_test.go` 的连接替换/超时外键回归，以及 `platform_contracts_test.go` 的共享参数、公开分享、候选校验、WARP、缓存持久化与取消结果测试。前端第五组 `scripts/test-panel-contracts.mjs` 覆盖 UTF-8 密码、网关范围、协议字段、移动排序与操作员网关控制。

`POST /api/country` 请求为 `{country,config}`，`POST /api/preset` 为 `{id,protocol,config}`；config 必须是完整当前草稿。预设按草稿的入站端口、UDP 范围、VPNGate 及订阅监听端口分配，同时避开面板监听端口，不回退读取数据库配置。两接口仅返回候选，不修改数据库；可能准备本地证书材料。请求绑定编辑器与页面，写回前比较草稿身份和内容。可信反代必须追加真实客户端地址到 X-Forwarded-For，并设置 X-Forwarded-Proto=https。

并发持久预留基准：`go test ./internal/accounting -run '^$' -bench '^BenchmarkConcurrentDurableReservations$' -benchmem -count=3 -cpu=4`。保持 WAL/FULL 与 64 KiB 单池边界，结果不能等同端到端代理带宽。

Go 1.27.3，版本锁定于 versions.json。安装包包含 manager + managed-core 两个程序，每个提供 Linux amd64/arm64 静态构建，并携带项目源码及第三方许可证。前端使用原生 ES modules，无生产 Node 依赖。

```bash
go mod download
go test ./...
go vet ./...
npm ci && npm test
go build -tags with_quic,with_utls,with_wireguard,with_gvisor -o .build/managed-core ./cmd/managed-core
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

菜单从管理器 `--version` 读取已安装版本，并限制版本查询等待时间。在线安装将当前入口的函数与固定仓库地址保存到解压目录，再交由发行包安装；不会从已消费的管道重新读取脚本，也不会把管理员输入或环境中的秘密写进入口。`.platform-v5` 和 systemd 所有权标记只用于安装所有权；实际数据版本由数据库与配置 schema 校验。

交互管理员输入通过 `panel --admin-stdin init/reset-admin` 读取两个 NUL 分隔字段，账号密码不经命令参数、环境变量或 Python 转义。默认 CLI 仍接受原来的 stdin JSON。

证书由平台内置的 lego v5.6.0 负责申请和续期，使用 HTTP-01、Let's Encrypt、ECDSA P-256。调度器每十分钟检查已应用登记，证书进入 30 天续期窗口时入队，同一证书成功入队后的重试间隔为 12 小时；修订变化立即检查，入队失败不等待该间隔。应用失败留下的待应用配置不会暂停续期。已申请结果缓存于 `acme/lego/`，服务发布失败后复用结果重试。账户私钥、登记和缓存证书以 0600 原子保存；备份恢复缺少登记时自动补登记。新导入/申请接受 `import` / `acme`，初始化的测试材料为 `generated`；不接受旧 `certbot` 标记或提供迁移。

受管核心不启用上游 `with_acme` 或 Cloudflare Origin CA 提供器。发布器按 Linux amd64/arm64 的实际构建依赖检查，拒绝链接 CertMagic/acmez；sing-box 上游模块元数据中的可选依赖不等于进入发行二进制。

Windows 开发先 `. ./scripts/dev-env.ps1`。真实链路设置 `SING_BOX_BINARY`、`MIHOMO_BINARY`、`MANAGED_CORE_BINARY`；然后：

```bash
RUN_UDP_IDLE_TESTS=1 go test -race -timeout=25m -count=1 ./...
MANAGED_CORE_BINARY="$PWD/.build/managed-core" go test -race -count=1 -run '^(TestManagedAccountIsolation|TestManagedCredentialReload|TestRealFiveProtocolsTCPUDP)$' ./internal/vpngate
MANAGED_CORE_BINARY="$PWD/.build/managed-core" go test -race -count=1 -run '^TestCredentialPublicationRetryDoesNotRepeatRotation$' ./internal/manage
bash scripts/test-program-upgrade.sh
bash scripts/test-installer.sh
shellcheck --severity=warning sb.sh install.sh lib/*.sh scripts/*.sh
```

Linux root 专项通过 `RUN_NETNS_TESTS=1`、`RUN_FIREWALL_TESTS=1`、`RUN_SERVICE_TESTS=1`、`RUN_STORAGE_TESTS=1` 启用，测试定义明确需要的二进制环境变量。仅使用专门 VM，真实 systemd/firewall 测试会创建系统资源。

`npm test` 执行 16 组回归，覆盖重复提交、客户端字段联动、短暂断网、用户页路径前缀、状态搜索、焦点保留、单位换算、授权分页、导航加载、恢复界面和状态同步。Mihomo 仅作为开发/CI 测试客户端，版本及 Linux amd64 摘要位于 scripts/client-test-versions.json，使用官方基础 amd64-v1 构建。CI 仍执行原生配置与真实链路测试；面板 API/浏览器测试刻意设置无效 MIHOMO_BINARY，证明部署功能不依赖它。

本地面板使用独立目录：manager `panel --root <空目录> --core <官方核心> --runtime <受管核心> --address 127.0.0.1 --no-system init`，stdin 为 username/password JSON；随后同样参数 `recover`、`serve`。仅在 `serve` 命令之前加 `--listen 127.0.0.1:0` 自动分配开发端口，日志返回实际 URL；`init` 和 `configure` 保存的端口必须为 1–65535。`--no-system` 仅用于开发，系统动作明确拒绝。Windows 的 Unix socket 测试需要较短的 TEMP/TMP 路径，例如项目下 `.test/tmp`。

CI 与本地共用自动验收入口，无需手动设置测试密码或启动服务：

```bash
GATEWAY_BINARY="$PWD/.build/yg-vpngate" \
SING_BOX_BINARY=/path/to/official/sing-box \
MANAGED_CORE_BINARY="$PWD/.build/managed-core" node scripts/test-panel-local.mjs
```

Windows 先加载 `scripts/dev-env.ps1`，将 `GATEWAY_BINARY` 和 `MANAGED_CORE_BINARY` 环境变量设为相应 `.exe` 路径。入口在 `.test/` 创建独立目录，初始化保存有效默认端口，启动时覆盖为回环随机端口，并通过 `/healthz` 等待 worker 就绪。启动预算 20 秒、整体预算 5 分钟；失败输出最多 64 KiB 面板日志并保留数据，成功停止子进程后删除临时目录。中断会取消子进程，停止超时则强制终止。

可对该独立面板运行真实 HTTP 工作流：设置 `PANEL_TEST_URL`、`PANEL_TEST_USERNAME`（默认 reviewadmin）和 `PANEL_TEST_PASSWORD`，执行 `node scripts/test-panel-api.mjs`。脚本只接受回环 URL，并在创建数据前确认服务均处于 no-system 状态。验证绑定/导出、下载次数、跨站分享、完整候选预览和任务应用；不能指向真实安装。

配置预览为 `POST /api/config/preview`，请求是完整 PlatformConfig；返回普通服务端脱敏配置、输出节点数/变化、结构化验证和 `deployment` 服务变更计划（`changes`、`runtime_observed`、`applied_revision`）。`POST /api/config/preview-output` 接受 `{id,config}`，只编译目标图并返回有界脱敏内容。旧 `/api/subscriptions/preview` 不恢复。节点文档 schema=7、订阅 schema=6、CompilerVersion=8、模板 version=1、平台 schema=8；备份容器为 8，数据库为 17，旧订阅内容明确拒绝。

新增回归 `compiler_regression_test.go`、`preview_test.go` 覆盖 URI 认证/端口、SIP003、WS early-data、gRPC/XHTTP、名称和策略组范围、脱敏与真实核心检查。`TestImportedTrojanTransportsWithMihomo` 使用真实 sing-box 服务端和 Mihomo 客户端验证 WS/gRPC，错误路径或服务名必须失败。`TestRefreshValidationDoesNotHoldPublicationLock` 覆盖锁外编译与过期配置拒绝，`TestLimitedServerPreservesTLSAndHTTP2` 覆盖 TLS 状态及协议协商。

`test-panel-subscriptions.mjs` 是第七组前端回归，覆盖默认 DNS 与显式关闭、动态客户端范围、迟到响应、候选向导和局部列表更新。真实 API 套件检查订阅 schema=6、账户模板覆盖、可用性、扩展名、下载元信息和脱敏预览；浏览器套件沿用隔离目录与真实事件。

`template_contract_test.go` 覆盖 DNS 三态与空排除数组、原生字段归属、Egern IPv6/TLS 和端口跳跃间隔。`account_export_cache_test.go` 覆盖容量/过期/并发合并、凭据轮换、授权撤销、正文隔离、严格核心校验和 ParserVersion 升级后的完整抓取。运行 `go test ./internal/manage -run '^$' -bench '^BenchmarkAccountCompilation$' -benchtime=300ms -count=3 -benchmem` 可复测缓存，测量范围见[当前验证记录](validation/2026-11-21-refactor.json)。

`GET /api/accounts/outputs?id=…&scope=…` 和用户用量的 profiles 对象数组共用实际导出计划；`GET /api/outbound-options` 返回已应用出站。新字段与契约直接更新，不增加旧数组或旧接口适配器。完整订阅说明见 [订阅](subscriptions.md)。

当前数据库 schema 17、备份格式 8。不复用早期开发目录或旧数据库；应用配置 schema 与数据库版本分别维护。对外 CLI 保留 panel、核心 prepare/check、firewall、下载监听和网关运行模式，旧 config/accounts/subscriptions/asset/remote 命令不再暴露。

账户策略存储为独立字段，备份结构校验随 schema 更新；所有旧 schema 数据库及其备份明确拒绝。新增回归覆盖配置快照、纯账户订阅自动恢复、模板省略字段、Argo 日志滚动、BBR 部分失败重试、任务详情权限、分页边界、日期精度及分享结果窗口。`Task.details` 是服务端生成的详情访问描述，前端不再维护任务类型白名单。

旧 config/subscriptions 命令适配器及其 `legacy_*_test.go` 实现已删除；回归夹具调用现有 PlatformService 编译器，应用测试经过版本检查和持久任务。旧 remote 命令和 remote.json 通知旁路已移除；使用平台订阅分发资源及 webhook。新增授权列表接口：`GET /api/grants?q=...&offset=0&limit=20` 返回 `{items,total,offset,limit}`，limit 范围 1–100，条目含统一 `state`；没有旧数组接口兼容层。

账户列表同样返回分页对象：`GET /api/accounts?q=...&offset=0&limit=20&sort=created&direction=asc`。搜索 ID、名称、订阅者 ID；sort 仅允许 created/name/id，direction 仅允许 asc/desc，offset 为 0–100000。账户 DTO 包含 `subscriber_disabled` 和有效 `state`。管理员选择器使用 `GET /api/account-options` 的 `{id,name}[]`（账户总量上限 1000）以及 `GET /api/inbound-options?scope=normal|vpngate` 的已应用启用入口；不使用列表第一页代替所有选项。未配置 VPNGate 时入口选项为空数组。

主机升级调用 `panel --root <目录> wait-runtime`，stdin 为 `{"instance":"normal","started":<Unix纳秒>}`，等待 started 不早于本次重启的近期心跳，最长 20 秒。重启助手另外调用 `wait-panel` 验证实际本机监听/TLS 和 worker 就绪，最长 30 秒；随后原子保存 `program-upgrade.json` 并调用 `complete-upgrade`。收据按任务 ID 和目标版本匹配，启动可重复消费，成功、回滚和回滚失败均有终态。等待阶段保留跨重启，20 分钟无确认结果则失败。`recover-services` 在打开数据库之前执行；服务变更与升级共用恢复文件锁。

重构回归：`go test ./internal/manage -run 'TestUpgradeReceipt|TestShareReadiness|TestInvalidPanelInitialization|TestPanelHealth|TestSubscriptionSchedule|TestSubscriptionHTTPS|TestPanelAdvanced|TestRestorePreviewUsesArchived'`，TLS 缓存和传输策略单独运行 `go test ./internal/tlsconfig ./internal/publicaccess`。`BenchmarkSubscriptionScheduleRead` 对照完整投影读取与调度元数据读取；`npm test` 覆盖 gRPC/未知传输保留、共用 TLS 字段、任务清理与保留详情提示。真实浏览器覆盖鼠标、键盘、触屏、权益编辑、独立服务链接和访问方式。

`internal/backuparchive` 单独负责格式 8（8 字节项目标记 + age 流 + tar），使用 scrypt 工作因子 16 并限制解密工作因子。禁止恢复前使用尚未完成认证的条目。新增测试覆盖密文损坏、截断、路径限制、并发发布锁、授权生命周期、任务预算、状态搜索与分页。

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

订阅扩展回归为 internal/subscription/extensions_test.go，覆盖 auto/文本输入、跳过与数量边界、gRPC/HTTPUpgrade/salamander 往返、唯一名称、缓存摘要/污染、组循环/空组、VPNGate 禁止直连以及有序规则。设置 SING_BOX_BINARY 与 MIHOMO_BINARY 后，TestExtendedTemplatesWithInstalledCores 实际检查带 DNS URL、策略组和国内规则的配置。新增 HTTP 能力/管理员操作接口与无秘密投影回归位于 manage。单纯编码器测试不能替代目标应用导入。

服务单元生成器测试位于 internal/hostservice/platform_test.go，直接验证生产 renderer。GitHub 全量 race 步骤使用 sing-box 和 Mihomo 测试客户端执行互通验收；客户端进程运行器位于 \_test.go，不编入服务端。前端保持无构建步骤的原生模块。

Linux systemd 恢复验收也直接使用 `hostservice.PlatformUnits`，仅将主进程替换为测试 helper，并将防火墙命令包装到隔离命名空间；重启、启动前恢复和限速策略均来自生产单元。Windows 的 `go test ./...` 不编译 `//go:build linux` 文件，交付前另行交叉编译 Linux 全部测试：设置 `GOOS=linux`、`CGO_ENABLED=0`，运行 `go test -exec <仅返回成功的本机程序> -run '^$' ./...`。该检查只证明编译成功，真实执行由 Linux CI 完成。

真实 HTTP 测试会校验权益改名、实际到期字段、额度与次数拒绝、归档 revision 冲突以及归档筛选。完成 `node scripts/test-panel-api.mjs` 后，在同一临时面板上运行 `python scripts/test-panel-browser.py`。沿用 `PANEL_TEST_URL` 和测试管理员环境变量，需要 Python Playwright 和 Chromium。通过 `CHROME_BINARY` 可指定已有 Chrome 可执行文件。该脚本使用真实鼠标、键盘和触屏事件，并验证 390 × 844 视口中首条权益的位置。

共用启动器支持 `PANEL_BROWSER_TEST=1 node scripts/test-panel-local.mjs` 自动续跑浏览器验收；`PYTHON_BINARY` 可指定开发 Python，`PANEL_TEST_SCREENSHOT_DIR` 保存桌面/手机及新增设置页面截图。Linux 公网访问专项为 `sudo env RUN_ACCESS_MAINTENANCE_TESTS=1 SB_TEST_ROOT=/etc/s-box SB_TEST_PASSWORD=... python3 scripts/test-access-maintenance-linux.py`，仅在可丢弃 VM 执行：检查内置订阅 TLS、面板重启/回滚、任务处理/删除/保留以及项目日志清理不会影响全局日志。HTTP 切换到 HTTPS 使用不同 Cookie 名称，客户端需重新登录。此专项已接入全新安装 CI。

停止测试进程和专用 VM 后，可删除 `.test/` 中的日志、临时数据库、测试二进制及 Go 缓存，`.build/release/.bin/` 中的打包中间文件，以及 `scripts/__pycache__/`。下一次加载 `scripts/dev-env.ps1` 会重建缓存目录；Go 会重新下载和编译所需模块。

清理前先将仍需发布的 `.build/release/*.tar.gz` 与 `SHA256SUMS` 移出待清理目录。源码回归、第三方许可证和文档使用的示例图保留；不用的工具链副本通过 `-Tools` 清除，不维护旧虚拟机资产或私有镜像路径。

无法自动删除的条目集中移动到 `.cleanup-manual/` 后手动删除；仅有残留时创建此目录。它已被 Git 忽略，不能提交到 main 或打入发行包。Windows 删除/移动使用原生 PowerShell，并先确认实际绝对路径位于项目目录内。

交付前核对本地分支和远端 heads，将有效改动提交到 main；仅在确认其他分支提交已合入后删除那些分支。`git fetch --all --prune` 清除失效的远端跟踪引用。保留正式版本标签，分支清理不删除 Release。main 推送触发 CI 验证；用户可安装的版本仍需通过既有发布流程上传完整归档和 SHA256SUMS。

## 当前重构验收入口

```bash
go run ./cmd/panel-contract -check
npm ci
npm run typecheck
npm test
SBVPN_REQUIRE_CLIENT_CHECK=1 go test -race -timeout=25m ./...
python3 -m pip install -r scripts/browser-requirements.txt
python3 -m playwright install --with-deps chromium
PANEL_BROWSER_TEST=1 PYTHON_BINARY=python3 GATEWAY_BINARY=... MANAGED_CORE_BINARY=... SING_BOX_BINARY=... node scripts/test-panel-local.mjs
```

前端共有 16 组回归，新增跨页面任务详情、并发弹窗归属、错误契约和按需资源编辑器检查。CI 先运行严格协议夹具预检，再运行完整 race/协议矩阵；实际 API 与桌面/手机浏览器验收均启用。浏览器只属于开发/CI 依赖，VPS 运行不安装 Node、TypeScript、Playwright 或 Python。

更改 Go 面板边界模型后运行 `go run ./cmd/panel-contract`，同时提交生成类型。`tsconfig.json` 对全部 JS 运行 checkJs；`tsconfig.contracts.json` 检查命令正反例，`tsconfig.boundaries.json` 对 API、账户 API 和证书字段模块启用 strict。`npm run typecheck` 顺序执行三者。没有宣称所有 UI 都已启用 strict。边界测试夹具与旧 CLI 回归辅助函数只编译进 `_test.go`，生产程序没有旧接口兼容层。

性能对照可用 `go test ./internal/manage -run '^$' -bench '^BenchmarkAccountCompilation$' -benchtime=500ms -count=3 -benchmem` 及 accounting 的 ActiveAuthorization100 基准。避免与发行构建、race 或 VM 测试同时计时。

## 2026-11-20 的验证要求

平台集成测试需要 `SING_BOX_BINARY` 与 `MANAGED_CORE_BINARY`，运行前先构建受管核心；CI 已将这一步移到全套测试之前。受管核心使用 `with_quic,with_utls,with_wireguard,with_gvisor` 四个标签，缺少 gVisor 的构建不能校验默认用户态 WireGuard endpoint。

```sh
go run ./cmd/panel-contract -check
npm run typecheck
npm test
go test -tags with_quic,with_utls,with_wireguard,with_gvisor ./internal/runtime
go test ./...
go vet ./...
```

生成器展开 Go 的嵌入结构、时间和 RawMessage，覆盖配置、凭据、账户、授权、任务、会话与能力响应；`contracts.d.ts` 不编入二进制。前端全模块 `checkJs` 使用非严格模式，类型化请求边界与运行时 HTTP 回归共同验证接口，不能把它描述为完全严格 TypeScript。

任务历史新增 `GET /api/tasks?before=<id>&limit=50`，响应 `{items,next_before}`，`limit` 为 1–100；0 的 `next_before` 表示没有下一页。概览任务数量固定上界，`task_counts.failed` 是未标记处理的失败总数，`attention` 是未处理失败或 partial/cached/failed 业务结果总数，`active` 是排队/运行中总数。顶部告警与概览统计使用聚合值，不依赖最近 12 条摘要。任务详情权限独立检查。

备份读取返回私有暂存文件路径，调用者必须 `Close` 释放；失败读取自动清理。不要重新把所有条目读回一个内存集合，也不要在密文认证完成前信任数据库。当前备份格式 8、数据库 17、平台 8、节点 6、订阅 6，需新初始化目录。

## 5.1 重构回归

前端共有 16 组回归，覆盖密钥可见性、证书回选、访问预览草稿、迟到响应、协议字段、自动绑定、平台选择和输出用途。浏览器验收使用明确的提交按钮定位，避免把显示密码按钮当作登录操作。

`manifest_test.go` 要求组件版本与 go.mod 一致。模板导入测试拒绝有损逻辑条件、非末尾 MATCH、脚本、未知字段和过量输入。发行包选择测试拒绝草稿、预发行、错误资产地址/摘要和重复资产；升级 Shell 测试同时断言校验器回滚。

Windows 的 VPNGate 配置在构造时统一目录格式，后续校验/重载不因路径分隔符差异误判为修改目录。离线启动测试使用五秒本地调度上限，节点来源一直阻塞到取消，仍验证启动不等待来源请求完成。

推送 main 只更新源码并触发 CI；发布可安装包继续使用现有 tag 或 workflow_dispatch publish 流程。无需建立或保留额外开发分支。

安装脚本的 `verify_installation` 逐个确认 sb-core / sb-panel，然后等待新核心心跳和面板 worker 就绪。不要用 `systemctl is-active --quiet sb-core sb-panel` 作为「两个均正常」条件：该命令任一个活动就可能成功。CI 的 `test-install-health.sh` 覆盖单服务失败、心跳失败和面板未就绪。

## 6.0 专项回归

npm test 包括首次导出查询取消、替换对话框、默认模板重置、模式隔离和简单规则编辑。Go 专项包括输出身份、精确授权、远程模板隔离，以及 TestClientExportsDoNotNeedMihomo、TestUnsupportedProbeIsNotNodeFailure、TestProbeSummaryDistinguishesUnsupportedAndFailed、TestArtifactParsersCoverGeneratedClientModes。历史版本比较测试不是生产兼容层。

变更输出结构时同时更新 clientprofile 输出目录、subscription 策略、授权 schema、面板生成契约、API/浏览器夹具和文档。验证记录单独记录实际命令与限制，历史记录保持原样。

## 当前源码入口

管理程序使用 `go build -o .build/yg-vpngate ./cmd/sbvpn` 构建；VPNGate 测试包为 `./internal/vpngate`。原 addons/vpngate/gateway 源码目录已移除。修改能力 DTO 后运行 `go run ./cmd/panel-contract`，并运行 `npm run typecheck`、`npm test`。

完整验收应设置 `MANAGED_CORE_BINARY`、`SING_BOX_BINARY`、`MIHOMO_BINARY`，未设置时相关集成测试会跳过，不能只凭 `go test ./...` 的绿色结果宣称客户端链路通过。Windows 使用 `scripts/dev-env.ps1` 配置工具链；Linux 的 namespace、systemd、nftables、磁盘故障和 race 验收见 `.github/workflows/ci.yml`，在 CI 或独立准备的隔离环境运行。

## 审查问题回归与契约

`usage_queries_test.go` 验证详情/列表一致、账期推进、首次使用不激活及身份冲突。`audit_consistency_test.go` 验证 viewer/operator 配置脱敏和 HTTP 409。`subscriptions_fetch_test.go` 验证并发上限、取消和公平队列；`subscriptions_result_test.go` 验证手动/自动失败报告、取消后的来源保存与版本冲突。`test-panel-navigation.mjs` 从真实 app 延迟进入日志页并注入 SSE，验证任务更新及离页停止。`test-panel-workflows.mjs` 验证链接限制真实提交。

Telegram/GitLab 的回归直接调用生产分发入口和快照捕获逻辑，旧 remoteSettings / pushTelegram / pushGitLab 测试实现已删除。其余仍有调用方的 legacy 测试夹具仅用于构造和验证历史场景，不是生产兼容层。新增测试应优先使用当前 application、HTTP 或 CLI 入口。

授权快照只复用容器容量与临时值数组，每轮重新查库、完成后清空，闲置时释放；不能把复用改成跨轮授权结果缓存。新增敏感配置字段须扩展统一字段策略和角色 HTTP 回归，URL 字段族也按凭据处理。

`npm run typecheck` 同时运行全量 JS 检查与 `tsconfig.contracts.json` 的 strict 契约检查。公开任务、刷新结果和分享限制具有正反类型用例；不把现有 JS 的非严格检查误称为全项目严格类型覆盖。

## 订阅一致性与投递回归

配置编译可在锁外进行，但提交前必须核对数据库确认代际，不能只比较订阅 revision。运行状态写入统一通过 fileBundle.subscriptionRuntime，配置/凭据/隧道发布用 publishPlatformFiles 合并最新操作记录。增加新的状态字段时同步更新 Go DTO、panel-contract 和 scripts/task-contracts.ts，禁止手改生成的 contracts.d.ts。

新增关键测试：

- subscriptions_generation_test.go：确认前拒绝刷新、确认后恢复、编译期间代际变化。
- account_remote_template_test.go：本地模板的远程规则与已发布输出一致，丢失依赖只阻断相关格式。
- subscriptions_delivery_lifecycle_test.go：取消保留确认、配置变化匹配目标、并发发布合并、独立后台队列、截断确认与明确拒绝。
- subscriptions_activity_projection_test.go：状态读取不依赖大运行文件，响应不包含投递凭据；另提供 4096 节点状态读取基准。
- test-panel-subscription-lifecycle.mjs：真实页面 SSE、标签页重访、隐藏页、合并刷新和搜索/排序取消；已纳入 npm test。

Windows 验证先加载 scripts/dev-env.ps1，再设置 MANAGED_CORE_BINARY 指向使用 with_quic,with_utls,with_wireguard,with_gvisor 构建的 managed-core。缺少这些环境变量会跳过实际核心集成夹具，不能把跳过视为完成验收。

运行状态查询基准：go test ./internal/manage -run '^$' -bench '^BenchmarkSubscriptionActivityRead$' -benchmem -benchtime=1s -count=3。基准比较相同合成缓存的元信息解码与小投影读取，不测端到端 HTTP 或 VPN 吞吐。


## 2026-11-25 重构验证入口

新增 `config_consistency_test.go` 覆盖隧道候选/应用/下载一致性、临时观察归属、已应用版本缓存失效和废弃字段拒绝。历史 CLI 测试实现已移除；`config_fixture_test.go` 只组装测试材料并调用正式编译或应用服务。固定文件名导出的旧断言已替换为显式输出快照与旧文件不生成断言。

`test-panel-refactor.mjs` 覆盖结构化 DNS 解析器在编辑、动态选项和无关字段修改中的保留及错误定位。浏览器验收额外检查 1280/375/320px 登录输入框与显隐按钮宽度、切换行为及溢出。使用 `PANEL_BROWSER_TEST=1` 执行本地 API + 浏览器；可设置 `PANEL_TEST_SCREENSHOT_DIR` 保存证据。

运行 `go test ./internal/manage -run '^$' -bench '^BenchmarkBindingAvailability$' -benchmem -benchtime=1s -count=3` 比较 256 个入站下重复读取完整配置与版本缓存的成本。该指标只覆盖列表可用性计算，不代表 VPN 吞吐或整机内存。

## 本轮稳定性验证入口

数据库 schema=17 只支持新初始化目录。新增的服务计划与任务能力字段由 Go 契约生成，修改后执行 `go run ./cmd/panel-contract`；CI 使用 `-check` 防止前后端漂移。普通面板类型检查启用 noUnusedLocals/noUnusedParameters，回调占位参数显式使用下划线前缀。

- Go 回归：publication_validation_test、deployment_plan_test、task_cancellation_test、controlstore/task_lifecycle_test、domain/address_test、accounting/reservation_ownership_test。
- 前端第 12 组：`scripts/test-panel-deployment.mjs`，验证部署计划、主机状态说明、证书回显和服务端驱动的探测取消。
- 运行中取消与完成竞态、并发认领排他、后台刷新隔离、探测队列容量，以及无效发布字段不推进配置版本都有明确断言。
- 完整的 Windows 测试、race、真实核心/API 与性能样本记录于 `docs/validation/2026-11-25-stability.json`。Linux systemd/netns/防火墙及磁盘故障由 Linux CI 执行；本地交叉编译不等同于运行验收。

## 多选与接口收敛验证入口

`scripts/test-panel-selection.mjs` 是第 13 组前端回归：出站组顺序、勾选焦点、1000 个候选只渲染 40 项、筛选保留、限额、必填错误聚焦、自由文本和整数、兼容性失败/重试，以及只有 VPNGate 时真实批量绑定页面的默认值和明确入口快照。已有页面生命周期测试继续覆盖请求取消与迟到响应。

`selection_contract_test.go` 验证空绑定拒绝且不写凭据、只绑定所选入口、后台摘要脱敏/恢复/容量，以及未初始化发布拒绝。订阅测试用 `confirmSubscriptionFixture` 创建数据库确认状态，不在生产函数中增加测试专用放行分支。类型反例要求省略或传空 `inbound_ids` 都不能通过。

`dom.el` 对 HTML 布尔属性使用存在性语义，禁止生成 `disabled="false"` 或 `hidden="false"`；ARIA 属性仍保留显式字符串值。回归必须实际触发排序/分页按钮并检查焦点，不只检查渲染后的文本或数组；内联复选框和兼容性详情的可见性也有断言。

修改任务契约时也须检查 Python/Linux 安装验收脚本。`test-platform-linux.py` 先读取当前入口再提交绑定 ID；`test-panel-api.mjs` 用真实 HTTP 断言省略或传空入口数组都返回 400。脚本不能依赖被移除的接口默认行为。

真实浏览器脚本验证搜索和勾选、跨搜索保留、Escape 焦点、分享的设备筛选，以及 390px/320px 视口中选择器和保存栏。截图是临时验证产物；公开文档只记录测试范围，不提交临时账户、数据库或日志。当前命令、结果和环境限制见 `docs/validation/2026-11-25-selection-refactor.json`。

增加选择组件时优先扩展共享字段协议，不引入页面私有状态副本。动态候选通过字段工厂在打开表单时读取；候选不足 40 项也走同一组件。仅在选项或依赖变化时刷新候选，单次勾选不重建复选框。格式分组使用服务端的 mode，协议使用 `/api/clients` 目录，不按展示文字猜测后端能力。

## 本轮权限、导出与状态同步验证（2026-11-26）

本轮只运行相关回归及全包编译检查，结果见[验证记录](validation/2026-11-26-projection-export-refactor.json)。projection_export_test.go 覆盖来源 URL 脱敏、多 TUIC 入站端口覆盖和凭据发布范围；audit_consistency_test.go 通过真实 HTTP handler 验证 viewer/operator 看不到来源令牌而管理员仍可编辑。refresh_result_test.go 覆盖刷新等待、旧缓存、失败、缓存写入失败、重复读取和最近成功时间；close_batch_test.go 覆盖多批退出结算、防重放与旧租约 ID 拒绝。

前端第 16 组 test-panel-state-sync.mjs 验证证书状态局部刷新、并发通知合并、离页取消和按入站导出端口；恢复、契约、输出与账户工作流脚本按需运行。凭据变更同时执行四个发布标签下的 runtime 竞态检查及五协议真实核心热重载；集成夹具显式提供 MANAGED_CORE_BINARY，跳过的测试不能记作通过。本轮没有执行 Linux 主机联动、真实浏览器或生产规模性能测试。

## 此前恢复与资源验证（2026-11-26）

本轮按改动范围执行验证，不运行完整测试套件。[验证结果与清理记录](validation/2026-11-26-recovery-resource-refactor.json)。证书恢复、状态能力、UDP 归属、账户导出和订阅小文件检查位于 recovery_boundaries_test.go；前端新增第 14 组 test-panel-recovery.mjs。已有发布/投递生命周期、端口防火墙、证书调度、前端导航及部署回归按需运行。计量请求复用执行 accounting 的定向 race 检查。类型检查和 panel-contract -check 检查生成契约，编译检查不等同于 Linux 运行验收。

平台 schema 8 / 节点文档 schema=7 不兼容旧配置；数据库表结构仍为 schema 17。测试应创建新临时目录，不复用之前审核生成的数据库。需要启动真实管理集成夹具时设置 MANAGED_CORE_BINARY；没有该变量的跳过不能记作通过。

验证结束后使用 `scripts/clean-dev.ps1 -Dependencies` 清理项目内 `.test`、`.build`、`.cleanup-manual` 和 node_modules，包括旧测试数据库、二进制、缓存、日志与截图。需要最小工作区时追加 `-Tools` 清除工具副本。脚本先确认路径和运行程序，保留文档仍引用的示例图；开发恢复方式见本页「本地目录与体积」。

## 订阅与绑定修复验证（2026-11-26）

本次继续完成中断的实现，基线为 5ef4a39；结果见 [验证与清理记录](validation/2026-11-26-subscription-binding-refactor.json)。只验证受影响模块，不把历史全量检查记作本轮结果。

- subscriptions_policy_test.go 覆盖真实响应计次（GET/HEAD/304/206/412/416、无效 ETag 前缀、多段 Range）、并发限次、未确认发布、快照版本不匹配、同版本配置文件替换及实际贡献账户授权。远程源过期覆盖可选未贡献、贡献和必需三种语义。
- accounting/bindings_test.go 检查账户、实例和入站查询范围；accounts_test.go 检查重复绑定不创建新凭据；refactor_regression_test.go 通过真实 runAccountBatch 入口检查发布重试不会再次轮换。
- test-panel-workflows.mjs 覆盖提交完成前关闭、打开新编辑器、离页、登出，以及已完成链接的主动查看和清除。相关契约、订阅生命周期、弹窗生命周期、输出配置、恢复和状态同步脚本按需执行。
- 三组类型检查和 panel-contract -check 验证 Go 生成契约。scripts/task-contracts.ts 增加面板设置修订类型、订阅者状态和 MFA 代码类型的错误输入用例。真实浏览器断言已同步“订阅输出”标题，本轮不运行浏览器验收。

绑定查询基准命令：`go test ./internal/accounting -run '^$' -bench '^BenchmarkBindingKeyLookup$' -benchmem -benchtime=20x -count=3`。同一磁盘 SQLite 比较全凭据读取并建立关系表与定向关系查询，初始化不计时；串行执行，避免并行构建干扰。该结果仅表示关系读取的耗时和累计分配，不表示整机 RSS、完整绑定事务或 VPN 吞吐。
