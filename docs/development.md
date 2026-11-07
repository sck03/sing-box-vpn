# 开发、验证与发布

[返回项目首页](../README.md) · [架构与维护](architecture.md)

## 构建环境

工具链锁定 **Go 1.27.3**，模块只依赖标准库。版本与 sing-box 基线 SHA256 记录在
[versions.json](../versions.json)，当前测试基线为 sing-box 1.14.4。
安装包包含源码和 Linux amd64/arm64 静态管理/网关程序，VPS 无需开发环境。

```bash
go run ./cmd/release
```

产物位于 `.build/release/`：统一安装包包含源码和两种架构的静态程序，附 `SHA256SUMS`。
从源码本地安装时，先构建、解压安装包，再在包内运行 `sudo bash install.sh --local`。
参考来源与授权见 [NOTICE.md](../NOTICE.md)；安装不拉取 fanout/conduitvpn 源码，OpenVPN 由 apt 安装。

## 验证

先准备官方 SHA256 校验的 sing-box。基础验证覆盖初始化、更新回滚、认证、真实五协议
TCP/UDP、持续单向 UDP 和资源边界；性能测量命令见[优化记录](optimization.md)。
会话退出回归验证停止后不遗留握手/拨号；节点源回归覆盖断流、尾部错误、读取上限和损坏缓存。
订阅回归覆盖同秒更新与监听失败后的连接关闭；初始化回归覆盖拒绝旧配置、地址错误与重复执行。
出口回归覆盖等价 IP 拒绝、Geo 缓存归一化和 ASN 数值边界；刷新回归覆盖退出等待及停止后拒绝新任务。
缓存回归覆盖切换来源、后台取消、重启归属和 A → B → A 的过期响应；管理状态回归覆盖
顶层 `null` 的错误返回、凭据映射保护及拒绝前不生成证书。候选摘要验证过滤、字段隔离及空数组。

```bash
SING_BOX_BINARY=/path/to/sing-box RUN_UDP_IDLE_TESTS=1 go test -race -count=1 ./...
go vet ./...
gofmt -l manifest.go internal cmd addons/vpngate/gateway
shellcheck --severity=warning sb.sh install.sh addons/vpngate/vpngate.sh lib/*.sh
```

Linux 专项要求 root、OpenVPN、iproute2、iptables 和可用 TUN/netns；在独立测试环境执行：

```bash
CGO_ENABLED=0 go build -trimpath -o .build/yg-vpngate ./addons/vpngate/gateway
sudo env GATEWAY_BINARY="$PWD/.build/yg-vpngate" RUN_NETNS_TESTS=1 \
  "$(command -v go)" test -run '^TestLinuxNamespaceIsolation$' -count=1 -v -timeout=6m ./addons/vpngate/gateway
sudo env RUN_FIREWALL_TESTS=1 "$(command -v go)" test \
  -run '^TestLinuxMultiportFirewall$' -count=1 -v -timeout=1m ./internal/manage
sudo env GATEWAY_BINARY="$PWD/.build/yg-vpngate" RUN_SERVICE_TESTS=1 "$(command -v go)" test \
  -run '^TestLinuxServiceStartupRecovery$' -count=1 -v -timeout=2m ./internal/manage
```

隔离验收覆盖 OpenVPN、隧道 DNS、断线阻断、崩溃恢复和错误国家拒绝；防火墙验收覆盖 IPv4/IPv6
多端口及所有权清理，systemd 验收覆盖启动恢复和意外退出重试。

### Windows 本地工具与 Linux 验证环境

工具统一放在项目 `.tools/`，构建产物位于 `.build/`，缓存、临时文件与日志位于 `.test/`，
三者均不提交 Git。将 Go 1.27.3 Windows 解压到 `.tools/go/` 后，在 PowerShell 运行：

```powershell
. ./scripts/dev-env.ps1
go test -race -count=1 ./...
```

脚本只设置当前进程及其子进程的环境，不改系统配置；Go 缓存、模块下载和临时目录均指向项目。
`GOTOOLCHAIN=local` 防止自动下载工具链到其他目录。已下载的 Linux 验证环境使用 QEMU 和
Ubuntu 24.04 cloud image，基础镜像、可写磁盘、seed ISO、SSH 密钥及 Linux Go 压缩包均位于
`.tools/`。通过 `scripts/linux-vm.ps1 start` 启动、`scripts/linux-vm.ps1 stop` 关闭；
SSH 仅绑定 `127.0.0.1:22222`，连接信息见 `.tools/README.md`。无硬件加速时使用 TCG，耗时
不可用于推断生产性能。删除或移动虚拟机磁盘前先关闭它，工具和本地验证数据可整体手动清理。
启动信息表示 QEMU 进程已创建，SSH 需等待客体系统完成引导。串口日志使用 QEMU 的
`file:` 后端；若进程启动后立即退出，脚本直接报错，详情见 `.test/qemu-error.log`。

## GitHub Actions 与 Release

[CI 工作流](../.github/workflows/ci.yml)对普通分支与 PR 执行验证并上传构建产物。
推送 `v主版本.次版本.补丁` 标签时，用标签版本构建，全部检查通过后发布源码、安装包和 SHA256。

也可在 **Actions → CI → Run workflow** 手动运行：

| 输入 | 作用 |
| --- | --- |
| `publish` | 默认关闭；开启后，验证通过时创建版本标签与 Release |
| `version` | 自定义稳定版本，如 `v1.2.3` 或 `1.2.3`；留空自动递增 |
| `bump` | 自动递增 `patch`、`minor` 或 `major`，默认 `patch` |
| `draft` | 将 Release 保存为草稿 |
| `notes` | 自定义发布说明；留空由 GitHub 自动生成 |

自动版本取现有稳定标签和 `versions.json.release` 的最高值，再按 `bump` 递增；
没有稳定标签时使用清单版本。自定义版本忽略 `bump`，不能使用已有标签或预发布版本。
基准 `v1.2.0` 的 patch/minor/major 分别为 `v1.2.1`、`v1.3.0`、`v2.0.0`。

未开启 `publish` 时也会计算下一版本供预览，但不会创建标签或 Release。选定版本仅写入
CI 工作目录，确保程序、清单、包名与 Release 一致，不自动提交回分支。
手动发布标签指向本次所选分支的提交，发布任务串行运行，结果可在运行摘要中查看。
