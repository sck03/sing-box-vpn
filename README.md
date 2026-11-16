# sing-box-vpn v4

面向新部署的 sing-box 统一管理平台。Web 面板管理五协议入站、独立账户与周期额度、订阅与分发、服务端路由与 DNS、WireGuard/WARP、Argo、VPNGate、证书和维护任务。

普通节点与 VPNGate 使用独立受管核心进程。面板停止不停止代理计量；账户停用、到期、额度与凭据代次在认证后的转发边界执行。权威状态为 `control.db`，生成的 JSON 文件不再作为第二套配置入口。

## 安装

支持 Debian/Ubuntu、systemd、Linux amd64/arm64。需要 root；安装器自动安装 Python 3、OpenVPN、Certbot 等免费系统包。VPS 无需 Go 或 Node.js。VPNGate 需要 IPv4、TUN 与 network namespace。

**当前 v4 使用数据库 schema 5、备份格式 5，要求全新空目录，不迁移旧数据或兼容旧管理接口。** 分支提交不等于正式发行；安装器默认获取 GitHub 最新正式 Release。部署当前分支应先构建完整包：

```bash
go run ./cmd/release
tar -xzf .build/release/sing-box-vpn-v4.0.0.tar.gz
cd sing-box-vpn
sudo bash install.sh --local
```

安装后面板默认 `127.0.0.1:2096`，使用安装时设置的管理员账号：

```bash
ssh -L 2096:127.0.0.1:2096 root@VPS
```

浏览器访问 `http://127.0.0.1:2096`。首次进入「权益与用量」创建权益、绑定入口，再到「下载授权」创建分享。初始入口不接受共用预设凭据，不预建可公开下载的共享输出。

账号分为后台管理身份和代理使用权益：初始管理员可以创建其他管理员、操作员或只读账号，共同管理本机资源；没有租户隔离。订阅者通过专属链接查看自己的权益，不使用后台账号登录。一个订阅者可以拥有多个独立额度权益，同一权益的多个客户端链接共用额度。

公开管理访问需要 TLS 或明确配置的可信 HTTPS 反向代理，配置见[使用指南](docs/usage.md)。日常操作全部在面板；`sb` 仅保留安装、诊断、管理员重置、恢复、升级救援和卸载。

## 功能边界

- VLESS Reality、VMess WS、Hysteria2、TUIC、AnyTLS；原生 sing-box/Mihomo 配置与 URI、二维码、指定 TUIC 转发端口导出。
- 管理员/操作员/只读角色、bcrypt、Cookie 会话撤销、CSRF、持久登录限速、TOTP 与审计。
- 订阅者与多权益、首次代理使用/创建/预约开始、固定期限、每 N 日或每月按时区重置。下载令牌与代理额度分别管理。
- 配置 revision、持久任务、实际运行心跳、失败回滚、在线加密备份和空目录灾难恢复。
- VPNGate 出口校验失败阻断转发；「家宽候选」是筛选结果，志愿者节点和固定 IP 不作保证。
- 凭据新增/轮换要重启受影响实例载入，影响该实例其他连接；旧凭据撤销立即写入账本，运行时授权缓存最长 250 ms，另受调度、数据库响应和在途 I/O 影响。
- 第三方订阅只能管理下载；客户端直连第三方时，本项目无法执行其流量额度。

依赖优先采用免费开源的最新稳定版；核心适配依赖按经过验证的版本组合固定。当前 sing-box 1.14.4，SQLite 1.62.0，YAML 3.0.6，x/crypto 0.59.0。版本及许可证见 [go.mod](go.mod)、[NOTICE.md](NOTICE.md)、[第三方声明](third_party/modules/README.md)。

## 文档

- [使用指南](docs/usage.md)
- [订阅与下载授权](docs/subscriptions.md)
- [架构与扩展](docs/architecture.md)
- [开发与发布](docs/development.md)
- [兼容性与限制](docs/compatibility.md)
- [本次实施和验收记录](docs/unified-management-status.md)
- [实施前完整分析](docs/unified-management-analysis.md)

许可证 GPL-3.0。历史分析与测量资料保留为证据，不代表当前版本已重新通过其中所有实验。
