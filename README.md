# sing-box-vpn

面向新部署的 sing-box 统一管理平台。Web 面板管理五协议入站、独立账户与周期额度、订阅与分发、服务端路由与 DNS、WireGuard/WARP、Argo、VPNGate、证书和维护任务。

普通节点与 VPNGate 使用独立受管核心进程。面板停止不停止代理计量；账户停用、到期、额度与凭据代次在认证后的转发边界执行。权威状态为 `control.db`，生成的 JSON 文件不再作为第二套配置入口。

当前重构统一了网关与页面字段约束、WireGuard 端点和配置预览。页面提供完整协议表单、订阅工作区、服务开机启动控制和操作员网关控制；客户端导出与分享使用同一能力目录。

## 安装

支持 Debian/Ubuntu、systemd、Linux amd64/arm64。在 VPS 上用 root 执行：

```bash
bash <(wget -qO- https://raw.githubusercontent.com/sck03/sing-box-vpn/main/install.sh)
```

也可使用 `bash <(curl -fsSL https://raw.githubusercontent.com/sck03/sing-box-vpn/main/install.sh)`。菜单提供安装、升级、状态、诊断、管理员重置和卸载；命令末尾加 `install` 可直接安装。安装器自动下载完整正式发行包、校验 SHA256、安装所需系统包，并设置服务。按提示填写发布地址和管理员账号密码即可，VPS 无需编译，也无需安装 Go、Node.js、Python 或 Certbot。

证书申请与续期内置 **lego Go 库**，当前使用 HTTP-01 验证；自签证书与 PEM 导入也由 Go 完成。OpenVPN 仍是 VPNGate 建立隧道所需的系统程序，Go 管理器不能代替它。VPNGate 需要 IPv4、TUN 与 network namespace。

**当前平台使用数据库 schema 11、备份格式 7，首次安装要求空目录，不迁移旧数据或兼容旧管理接口。** 发行标签使用 `v主版本.次版本.补丁版本`，例如 `v5.0.0`；不限定主版本号。版本标签与平台数据格式分别校验，缺少当前架构管理器、受管核心或维护文件的包会被拒绝。开发者构建和本地发行包安装见[开发与发布](docs/development.md)。

安装后可随时使用：

```bash
sb             # 打开维护菜单
sb upgrade     # 下载并升级到最新正式版
sb uninstall   # 确认后删除程序和数据
```

也可直接运行 `bash <(wget -qO- https://raw.githubusercontent.com/sck03/sing-box-vpn/main/install.sh) uninstall`。卸载调用本机脚本，无需下载发行包；仅删除本项目标记的默认目录和服务，保留系统共享软件包。自动化卸载可显式加 `--yes`。

菜单显示管理程序实际返回的版本，不从标题或发行包文件名推测。在线安装/升级会保存正在使用的入口，避免发行包内较早的入口覆盖修复。推送 main 会更新在线入口；Go 程序的改动仍需重新构建并发布 Release 才进入 VPS 安装包。

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
- 凭据新增/轮换要重启受影响实例载入，影响该实例其他连接；失败后按持久版本自动补发布，不重复轮换。旧凭据撤销立即写入账本，运行时授权缓存最长 250 ms，另受调度、数据库响应和在途 I/O 影响。
- 独立恢复定时器重试 failed 服务，手动停止保持停止；运行概览分别展示配置、凭据发布/加载版本和进程心跳。权益与授权支持服务端分页，订阅失败使用缓存时明确显示降级。
- 第三方订阅只能管理下载；客户端直连第三方时，本项目无法执行其流量额度。

依赖优先采用免费开源的最新稳定版；核心适配依赖按经过验证的版本组合固定。当前 sing-box 1.14.4、lego 5.6.0、SQLite 1.62.0、YAML 3.0.6、x/crypto 0.59.0。版本及许可证见 [go.mod](go.mod)、[NOTICE.md](NOTICE.md)、[第三方声明](third_party/modules/README.md)。

## 文档

- [使用指南](docs/usage.md)
- [订阅与下载授权](docs/subscriptions.md)
- [架构与扩展](docs/architecture.md)
- [VPNGate 实现与边界](docs/vpngate.md)
- [开发与发布](docs/development.md)
- [兼容性与限制](docs/compatibility.md)
- [本次实施和验收记录](docs/unified-management-status.md)
- [性能与资源边界](docs/optimization.md)
- [历史分析（固定提交）](https://github.com/sck03/sing-box-vpn/blob/5975e773383b49dd6daca9f5406665eb47b06266/docs/unified-management-analysis.md)

许可证 GPL-3.0。历史分析与测量资料保留在 Git 历史中，不代表当前版本已重新通过其中所有实验。
