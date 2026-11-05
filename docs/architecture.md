**模块与维护约定**

项目使用一个仅依赖标准库的 Go 二进制 `yg-vpngate`。Bash 负责安装、菜单和 systemd 单元；
配置、导出、凭据、更新与网关运行逻辑由 Go 管理。使用说明见 [README](../README.md)，
测试与测量记录见 [optimization.md](optimization.md)。

| 网关模块（`addons/vpngate/gateway/`） | 职责与边界 |
| --- | --- |
| `main.go` | 命令入口、守护进程锁、监听器启动和退出协调 |
| `control.go` | 控制 HTTP/CLI、请求完整性校验、配置动作串行提交 |
| `manager.go` | 节点缓存与选择、出口验证、连接代次、隧道生命周期 |
| `config.go` / `profile.go` | 网关设置、Geo 筛选与 OpenVPN 配置校验 |
| `backend.go` | Linux netns、OpenVPN、worker、内核阻断规则和所有权清理 |
| `socks.go` | SOCKS5 握手、认证与 CONNECT/ASSOCIATE 分派 |
| `udp.go` / `udp_queue.go` | UDP 关联、目标会话、异步拨号、按目标发送、有限缓冲及关闭回收 |
| `transport.go` | worker IPC 帧、TCP 半关闭中继及可复用接收缓冲 |
| `worker.go` | 隧道中的域名解析、公网 IPv4 拨号和 TCP/UDP 转发 |

`proxyAccess` 隔离 SOCKS/UDP 与认证、连接容量和隧道选择；`tunnel` 隔离管理循环与具体 Linux
实现。新增后端应实现启动、停止、worker socket 与退出通知，再通过既有出口验证流程启用转发。
转发层不能自行选择未经验证的出口。测试使用替代实现和本机回显服务，Linux 专项再验收实际隔离。

| 管理模块（`internal/manage/`） | 职责 |
| --- | --- |
| `config.go` / `credentials.go` | 管理命令、配置变更、独立国家设置与凭据 |
| `state.go` / `render.go` | 私有文件、原生证书/密钥、服务端与三类客户端导出 |
| `publish.go` | 多文件提交恢复记录、失败恢复和启动等待 |
| `ports.go` / `firewall.go` | 按节点类型预留端口、共用端口变更、规则计划与防火墙批量提交 |
| `core.go` / `assets.go` | 官方资产校验、核心兼容性检查、激活及服务回滚 |
| `subscription.go` / `remote.go` | 有限连接的本地订阅及用户明确选择的远端推送 |

`cmd/release/` 构建两种 Linux 架构并生成安装包和 SHA256；`cmd/ci-release/` 解析发布版本。
`lib/services.sh` 统一生成服务及恢复策略，`lib/ui.sh` 统一输入、取消与错误返回。

配置写入与核心更新先获取 `.operation.lock`，文件校验/替换与恢复再获取 `.manage.lock`。
服务启动只等待配置锁，核心重启在释放配置锁后执行。控制动作同时使用管理层的
`ConfigTransaction`，按最新保存的设置合并；新增操作应沿用此提交路径及恢复记录。

UDP 关联的目标表和地址头缓存共用一把锁；网络拨号在锁外执行。每个关联最多 128 个目标、
256 个地址头缓存、8 个未完成拨号；每个目标最多排队 64 包。各目标使用独立发送协程与 FIFO，
避免一个目标的 DNS 或 IPC 写入阻塞影响其他端口。缓冲按 256 B 到 64 KiB 分档借用；容量计入
排队和正在写入的数据，关联最多 256 KiB，单目标最多 128 KiB。关闭关联取消拨号、关闭全部
连接并等待发送/接收协程退出，再归还缓冲。每个数据包沿用既有协程，不创建新协程。

端口预留按实际节点类型确定，普通节点始终使用 normal 归属，VPNGate 使用国家归属。
冲突校验与端口赋值分开，两类入口共用赋值函数，避免用 Profile.Country 推断临时对象的身份。
一次规则生成只建立一份预留快照，各协议复用它校验范围。防火墙查询仅将退出码 1 视为规则
不存在；nf_tables 返回退出码 2 时，仅明确指出当前查询目标链不存在的错误也作不存在处理。
其余错误均保留，全部所需查询通过后再建立提交计划。表级 COMMIT 不保证多个地址族
跨表同时提交，执行失败须保留所有权以便重试。内核未提供 IPv6 时按 IPv4 处理。

接收缓冲由 `sync.Pool` 在结束的会话之间复用，GC 可以回收池中的缓存。连接、认证与队列
不放入池；借用的缓冲必须在所有读写结束后归还。活跃会话仍需要完整的 UDP 接收空间。
TCP 半关闭、UDP 双向续期、稳定源端口、认证代次和未经验证不转发是维护时必须保留的行为。

新增协议须同时覆盖入口、凭据、端口、sing-box/Mihomo/URI 导出及真实 TCP/UDP 验收。
改动连接或并发逻辑时运行 `go test -race ./...`，改动 UDP 生命周期时启用
`RUN_UDP_IDLE_TESTS=1`；实际协议测试同时设置 `SING_BOX_BINARY`。Linux netns、防火墙与
systemd 专项由 CI 执行。性能结论应提供同机基线、输入与 `-benchmem`，并区分局部分配和整机资源。
