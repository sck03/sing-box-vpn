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

本地面板使用独立目录：manager `panel --root <空目录> --core <官方核心> --runtime <受管核心> --address 127.0.0.1 --no-system init`，stdin 为 username/password JSON；随后同样参数 `recover`、`serve`。`--no-system` 仅用于开发，系统动作明确拒绝。

依赖升级先 `go list -m -u all`，优先正式稳定 tag；sing-box 的 fork/pseudo-version 配套依赖需随核心兼容验证。当前更新了 SQLite、加密、YAML、网络等稳定库；不以任意 master 代替协议适配组合。新增依赖更新 `third_party/modules/`、NOTICE 和兼容性记录。

依赖更新后运行 `python scripts/update-notices.py` 重建许可清单。Linux 安装验收使用 `SB_TEST_PASSWORD=... python3 scripts/test-platform-linux.py`，仅在可丢弃安装执行：脚本会创建权益、暂停服务和制造端口冲突。

CI 执行全量 race、真实客户端矩阵、ShellCheck、升级状态测试、Linux 隔离与防火墙、双架构构建。push 分支只验证和上传构建产物；仅 tag 或显式 workflow_dispatch publish 才发布 Release。不要把分支推送描述为已部署。

验收证据写入 [实施记录](unified-management-status.md)，区分单元测试、真实网络测试、系统测试和未测范围。历史性能数据不能充当新版本提升结论。格式化 Go 使用 gofmt，前端使用 Prettier；发布不依赖前端构建工具。
