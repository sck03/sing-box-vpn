# 开发与发布

当前实现、必要验证和清理记录见 [实施报告](refactor-review.md)，架构边界见 [架构与维护](architecture.md)。Go 及组件版本以 versions.json 为准，前端依赖以 package-lock.json 为准；运行中的 VPS 不依赖 Node、TypeScript、Python 或浏览器工具。

## 环境与目录

安装固定版本 Go 到系统 PATH 或 `.tools/go`，执行 `npm ci` 恢复前端开发依赖。Windows 可加载 `scripts/dev-env.ps1`，将缓存放入 `.test`。真实核心测试需要显式设置 `SING_BOX_BINARY`、`MANAGED_CORE_BINARY`；只有客户端验收才需要 `MIHOMO_BINARY`。

数据库 schema 18、平台配置 8、节点文档 7、订阅配置 6、模板 1、备份容器 8 分开维护。测试和开发安装使用全新隔离目录，不复用旧数据库。旧格式直接拒绝，不提供迁移器。

```bash
npm ci
npm run typecheck
go run ./cmd/panel-contract -check
go build -o .build/yg-vpngate ./cmd/sbvpn
go build -tags with_quic,with_utls,with_wireguard,with_gvisor -o .build/managed-core ./cmd/managed-core
```

前端是无构建步骤的原生 ES modules；Go embed 包含生产资源，生成的 contracts.d.ts 不进入二进制。Go 使用 gofmt，修改前端使用 Prettier；不引入页面私有框架或重复组件。

## 按变更范围验证

日常修改只执行必要测试，CI 保留完整验收。npm 运行器自动发现面板单元脚本，排除独立 API/本地服务启动器；每个脚本最多 60 秒，未退出视为失败。DOM 测试必须释放窗口、计时器、观察器和取消信号，不能通过强制成功退出掩盖泄漏。

本次相关前端入口：

```bash
npm test -- test-panel-task-requests.mjs test-panel-submissions.mjs test-panel-workflows.mjs test-panel-lifecycle.mjs
npm run typecheck
```

- task-requests：纯 Node 回执测试，覆盖部分存储损坏、摘要计算期间确认、迟到完成和过期窗口。
- submissions：响应丢失后的重试编号、会话重载与退出清理、管理员/订阅者/任务保留策略保存及 TOTP 密钥生成的页面与弹窗归属。
- workflows/lifecycle：共享表单、迟到结果回看、加载弹窗取消和竞争归属。
- state-sync：资源状态失效、隐藏页、局部刷新和生效/草稿分离。
- checkJs 覆盖全部面板 JS；contracts 检查命令正反例，boundaries 对列出的请求和状态边界开启 strict。不能把它描述为全前端严格类型检查。

可见页面夹具使用 `pretendToBeVisual: true`，隐藏行为另设 document.hidden。表单输入有效性用 checkValidity/requestSubmit 验证；直接派发 submit 只能验证处理器。浏览器视觉、焦点、移动端和原生验证仍由真实浏览器验收负责。

Go 定向入口示例：

```bash
go test ./internal/controlstore -run 'TestTaskSubmission|TestAutomaticTasks|TestTaskRetention'
go test ./internal/application -run TestHistoryRetention
go test ./internal/manage -run 'TestHTTPTaskSubmission|TestSchedulerFailure|TestApplySubmission'
go test ./internal/accounting -run 'TestReservation|TestSharedReservation|TestReplayCrash|TestSQLiteWriterWait'
go run ./cmd/panel-contract -check
go vet ./internal/controlstore ./internal/application ./internal/accounting ./internal/manage
```

schema 或备份变更另外验证当前备份往返、结构拒绝和灾难恢复清理。设置核心二进制后运行受影响的 manage 集成测试；缺少变量导致的跳过不能记为通过。凭据/核心生命周期变更还需要带四个发布标签的 runtime race 和真实热重载回归。Unix socket 测试使用短名称临时目录。

CI 的完整 Go、race、协议矩阵、真实面板 API、浏览器和 Linux 主机联动用于发行验收；本机定向测试不能替代这些结果。

## 本地面板与系统验收

本地启动使用独立空目录，管理员 JSON 通过 stdin 输入：

```bash
.build/yg-vpngate panel --root <空目录> --core <官方核心> --runtime <受管核心> --address 127.0.0.1 --no-system init
.build/yg-vpngate panel --root <目录> --core <官方核心> --runtime <受管核心> --address 127.0.0.1 --no-system recover
.build/yg-vpngate panel --root <目录> --core <官方核心> --runtime <受管核心> --address 127.0.0.1 --no-system --listen 127.0.0.1:0 serve
```

只在 serve 前使用回环随机端口，init/configure 保存的端口仍须为 1–65535。`--no-system` 明确禁止系统操作。`scripts/test-panel-local.mjs` 可启动真实管理器/API/浏览器夹具，数据进入 `.test`，成功后释放；运行前准备所需二进制与浏览器开发依赖。

Linux 主机验收在 CI 或可丢弃隔离系统执行 `scripts/test-platform-linux.py`、`scripts/test-access-maintenance-linux.py` 和安装脚本回归；它们会创建账户、修改服务、监听及网络配置，不用于真实用户数据目录。仓库不依赖旧私有 VM、镜像或 SSH 密钥。

## 任务与页面开发约定

公开 `application.TaskCommand` 只描述业务命令；操作者、来源、调度去重和请求指纹由可信边界构造 `domain.TaskSubmission`。新增命令同步 CommandFields、验证和执行器，再运行 panel-contract；不要手工编辑生成类型。

HTTP 任务、配置提交和面板重启可传 `Idempotency-Key`，面板自动处理。相同 key 只用于同一请求；409 表示内容冲突，410 表示已受理但历史被清理。其他 CRUD 和 multipart 恢复不承诺此重试协议。请求回执最多 10,000、至少保留 24 小时；前端只存摘要和随机编号，最多 128 个未确认项，退出清除。

页面读取绑定 pageContext.signal，已提交写入绑定登录会话。保存完成后使用 refreshAfterSave 检查页面及 dialogSession 归属；需要回看结果时使用 workflow-results，不用迟到回调替换当前窗口。资源编辑尽量局部更新，保留搜索、排序、分页和焦点。多选、自由 token、字段联动统一复用现有控件；动态候选来自当前资源，不维护平行状态副本。

## 性能验证

持久计量基准使用真实磁盘，保持 WAL/FULL 和 64 KiB 限额：

```bash
go test ./internal/accounting -run '^$' -bench 'Benchmark(DurableMeter64KiB|ConcurrentDurableReservations)$' -benchmem -benchtime=1s -count=3 -cpu=4
```

对照测量时使用同一机器、磁盘和并发度，避免同时构建或跑 race。记录分配数、事务等待和分位数；微基准不是端到端 VPN 带宽。吞吐、RSS、主机服务锁等待需要目标 Linux 环境单独测量。已有账号编译、证书、调度及 UDP 基准按受影响路径选择，见 [资源与性能](optimization.md)。

## 发行

`go run ./cmd/release` 构建 Linux amd64/arm64 管理器与受管核心、源码和许可证归档。正式发行须有压缩包及 SHA256SUMS，安装器验证摘要、归档路径/类型及当前架构内容。不要用发行主版本推断数据库兼容性。

```bash
VERSION=v6.0.1 # 替换为实际发行版本
tar -xzf "sing-box-vpn-${VERSION}.tar.gz"
cd sing-box-vpn
sudo bash install.sh --local
```

GitHub 工作流保留完整发行验证。提交到 main 不等于生产部署或创建 Release。依赖更新遵循固定正式版本与上游组合，运行 `scripts/update-notices.py` 更新 NOTICE/third_party；不删除第三方许可证来压缩体积。

## 清理与 main 交付

先停止项目测试进程，再预览并清理可重建目录：

```powershell
./scripts/clean-dev.ps1 -Dependencies -Tools -WhatIf
./scripts/clean-dev.ps1 -Dependencies -Tools
```

脚本限定 `.test`、`.build`、`.cleanup-manual`，可选 node_modules 和 `.tools`；先校验绝对路径、链接及运行进程。日志、截图、旧测试数据库、旧程序与缓存可随这些目录删除。外部审查临时目录须单独确认完整绝对路径与所有权，不扫描或清理全局缓存、真实部署数据库及其他项目目录。被系统策略拒绝的清理须报告，不能计入释放量。

保留当前源码、必要回归、文档引用的示例图、紧凑验收摘要及许可证。历史测试输出以摘要代替整份日志，避免把可执行文件、数据库、截图或下载包提交到 Git。清理后用 npm ci 与固定 Go 工具链恢复环境。

交付前检查 git diff --check、生成契约和受影响边界；先 fetch 核对远端。分支独有提交先审查整合，确认已进入 main 后删除分支。保留 main、完整提交历史和标签，不通过重写历史来缩小工作区。
