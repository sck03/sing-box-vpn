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

Linux root 专项通过 `RUN_NETNS_TESTS=1`、`RUN_FIREWALL_TESTS=1`、`RUN_SERVICE_TESTS=1` 启用，测试定义明确需要的二进制环境变量。仅使用专门 VM，真实 systemd/firewall 测试会创建系统资源。

`npm test` 同时执行页面/草稿回归与可靠性回归，覆盖重复提交、客户端字段联动、短暂断网和用户页路径前缀。Mihomo 分流配置检查需要 `MIHOMO_HOME/Country.mmdb`；缺失时返回未完成核心校验的明确诊断，避免在预览时下载地理数据库。原生核心校验与 sing-box 节点连通性检查分别报告，不以其中之一代替商业客户端验收。

本地面板使用独立目录：manager `panel --root <空目录> --core <官方核心> --runtime <受管核心> --address 127.0.0.1 --no-system init`，stdin 为 username/password JSON；随后同样参数 `recover`、`serve`。`--no-system` 仅用于开发，系统动作明确拒绝。

当前数据库 schema 6、备份格式 6。不复用早期开发目录或旧数据库；应用配置 schema 与数据库版本分别维护。对外 CLI 保留 panel、核心 prepare/check、firewall、下载监听和网关运行模式，旧 config/accounts/subscriptions/asset/remote 命令不再暴露。

计量缓存基准（与 race、虚拟机启动和构建分开运行）：

```bash
go test ./internal/accounting -run '^$' -bench '^BenchmarkMeterTakeComplete$' -benchmem -count=3
```

该基准重复取用并退还已预留的 1 KiB，用于比较授权检查、锁和分配成本；不含 socket、额度补充和磁盘同步，不代表代理吞吐。审查回归还覆盖跨 Store 撤销、无变化不写检查点、lease 压缩后的重放拒绝、TCP 非对齐跨界、资源重建/304、大备份和禁用订阅者。

依赖升级先 `go list -m -u all`，优先正式稳定 tag；sing-box 的 fork/pseudo-version 配套依赖需随核心兼容验证。当前更新了 SQLite、加密、YAML、网络等稳定库；不以任意 master 代替协议适配组合。新增依赖更新 `third_party/modules/`、NOTICE 和兼容性记录。

依赖更新后运行 `python scripts/update-notices.py` 重建许可清单。Linux 安装验收使用 `SB_TEST_PASSWORD=... python3 scripts/test-platform-linux.py`，仅在可丢弃安装执行：脚本会创建权益、暂停服务和制造端口冲突。

CI 执行全量 race、真实客户端矩阵、ShellCheck、升级状态测试、Linux 隔离与防火墙、双架构构建。push 分支只验证和上传构建产物；仅 tag 或显式 workflow_dispatch publish 才发布 Release。不要把分支推送描述为已部署。

验收证据写入 [实施记录](unified-management-status.md)，区分单元测试、真实网络测试、系统测试和未测范围。历史性能数据不能充当新版本提升结论。格式化 Go 使用 gofmt，前端使用 Prettier；发布不依赖前端构建工具。
