# sing-box-vpn

面向新部署的 sing-box 统一管理平台。Web 面板管理五协议入站、独立账户与周期额度、订阅与分发、服务端路由与 DNS、WireGuard/WARP、Argo、VPNGate、证书和维护任务。

普通节点与 VPNGate 使用独立受管核心进程。面板停止不停止代理计量；账户停用、到期、额度与凭据代次在认证后的转发边界执行。权威状态为 `control.db`，生成的 JSON 文件不再作为第二套配置入口。

管理员侧栏固定显示「运行概览」「节点管理」「用户与分享」「系统设置」。给用户开通节点时，在「用户与分享」一次填写权益和节点范围；绑定任务完成后直接创建分享。分享页按设备平台筛选，支持的完整配置可直接唤起客户端。VPNGate、路由、出站和订阅编排在默认收起的高级区；搜索“新增节点”“查流量”或“系统设置 备份”可直达相应页面或标签。

## 安装

支持 Debian/Ubuntu、systemd、Linux amd64/arm64。在 VPS 上用 root 执行：

```bash
bash <(wget -qO- https://raw.githubusercontent.com/sck03/sing-box-vpn/main/install.sh)
```

也可使用 `bash <(curl -fsSL https://raw.githubusercontent.com/sck03/sing-box-vpn/main/install.sh)`。菜单提供安装、升级、状态、诊断、管理员重置和卸载；命令末尾加 `install` 可直接安装。安装器自动下载完整正式发行包、校验 SHA256、安装所需系统包，并设置服务。按提示填写发布地址和管理员账号密码即可，VPS 无需编译，也无需安装 Go、Node.js、Python 或 Certbot。

证书申请与续期内置 **lego Go 库**，当前使用 HTTP-01 验证；自签证书与 PEM 导入也由 Go 完成。OpenVPN 仍是 VPNGate 建立隧道所需的系统程序，Go 管理器不能代替它。VPNGate 需要 IPv4、TUN 与 network namespace。

**当前平台使用数据库 schema 16、平台配置 schema 7、节点文档 schema 5、备份格式 8，首次安装要求空目录，不迁移旧数据或兼容旧管理接口。** 发行标签使用 `v主版本.次版本.补丁版本`，例如 `v6.0.0`；不限定主版本号。版本标签与平台数据格式分别校验，缺少当前架构管理器、受管核心或维护文件的包会被拒绝。开发者构建和本地发行包安装见[开发与发布](docs/development.md)。

安装后可随时使用：

```bash
sb             # 打开维护菜单
sb upgrade     # 下载并升级到最新正式版
sb uninstall   # 确认后删除程序和数据
```

也可直接运行 `bash <(wget -qO- https://raw.githubusercontent.com/sck03/sing-box-vpn/main/install.sh) uninstall`。卸载调用本机脚本，无需下载发行包；仅删除本项目标记的默认目录和服务，保留系统共享软件包。自动化卸载可显式加 `--yes`。

菜单显示管理程序实际返回的版本，不从标题或发行包文件名推测。在线安装/升级会保存正在使用的入口，避免发行包内较早的入口覆盖修复。推送 main 会更新在线入口；Go 程序的改动仍需重新构建并发布 Release 才进入 VPS 安装包。

安装后面板默认 `127.0.0.1:2096`，使用安装时设置的管理员账号。在**你自己的电脑**终端执行（将 VPS 替换为服务器 IP）：

```bash
ssh -N -L 2096:127.0.0.1:2096 root@VPS
```

保持 SSH 连接，电脑浏览器访问 `http://127.0.0.1:2096`。管理员密码最低 8、最高 72 个 UTF-8 字节。先在「系统设置 → 公网访问」配置 HTTPS 域名/IP 和证书或可信反向代理，保存后重启面板；再在「用户与分享 → 权益与用量」创建权益、绑定入口并点击分享。完整流程见[公网访问与服务](docs/public-access.md)。初始入口不接受共用预设凭据，不预建公开共享输出。

账号分为后台管理身份和代理使用权益：初始管理员可以创建其他管理员、操作员或只读账号，共同管理本机资源；没有租户隔离。用户通过分享链接查看权益并下载配置。同一权益的多个链接共用额度；时间与流量只需在权益上设置。用户分组为可选高级功能，不需要单独创建订阅者或配置下载次数。

公开管理访问需要 TLS 或明确配置的可信 HTTPS 反向代理，可在「公网访问」编辑、预览、保存并点击「重启并应用」，失败自动回滚。直接 HTTPS 使用内置 TLS，无需 Nginx/Caddy；代理方式需自行安装配置。账户和高级输出可共用面板域名，独立订阅也支持内置 TLS。回环地址不会被生成成分享链接。高级区可以收起并记住状态；概览服务表提供职责说明和不同工作区入口。`sb-vpngate-core` 负责用户入口和计量，`sb-vpngate-gateway` 负责 OpenVPN 隧道与出口检查；`sb-subscription` 和 `sb-argo` 分别负责下载监听与 Cloudflare 隧道。

Reality 私钥及密码输入提供显示/隐藏按钮，默认隐藏。证书可在面板访问、独立订阅、入站和 VPNGate 中选择；访问设置回选当前绑定并保留预览草稿。程序与受管核心、官方校验器、lego 的版本可在「维护与任务 → 程序更新」查看，点击「检查更新」后按明确的发行版本更新。更新前校验摘要、配套版本和实际配置，失败一起恢复相关组件。

「维护与任务 → 任务与日志」支持已结束任务删除、标记处理、批量清理及保留策略。运行日志使用专用 `sb-vpn` 命名空间，提供筛选、下载、占用查询和项目历史清理。

## 功能边界

- VLESS Reality、VMess WS、Hysteria2、TUIC、AnyTLS；原生 sing-box/Mihomo 配置与 URI、二维码、指定 TUIC 转发端口导出。
- 管理员/操作员/只读角色、bcrypt、Cookie 会话撤销、CSRF、持久登录限速、TOTP 与审计。
- 订阅者与多权益、首次代理使用/创建/预约开始、固定期限、每 N 日或每月按时区重置。下载令牌与代理额度分别管理。
- 配置 revision、持久任务、实际运行心跳、失败回滚、在线加密备份和空目录灾难恢复。
- VPNGate 出口校验失败阻断转发；「家宽候选」是筛选结果，志愿者节点和固定 IP 不作保证。
- 凭据新增/轮换按入站重载，保留核心进程和其他入站连接；受影响入站可能短暂中断。失败恢复旧监听并按持久版本重试，不重复轮换。旧凭据撤销立即写入账本，运行时授权缓存最长 250 ms，另受调度、数据库响应和在途 I/O 影响。
- 独立恢复定时器重试 failed 服务，手动停止保持停止；运行概览分别展示配置、凭据发布/加载版本和进程心跳。权益与授权支持服务端分页，订阅失败使用缓存时明确显示降级。
- 订阅刷新与投递在后台队列运行，发布核对已确认配置版本；取消投递保留已确认结果。状态页显示目标级退避和接收状态，任务变化自动刷新，搜索会取消过期请求。
- 第三方订阅只能管理下载；客户端直连第三方时，本项目无法执行其流量额度。
- 订阅 schema=6：修正 Hysteria2 认证/默认端口与 Trojan 传输，支持受控 SIP003、WS early-data、gRPC 参数及 VLESS XHTTP。按实际账户节点显示客户端可用性，账户与高级输出共用可编辑模板，同一账户可同时分享仅节点与完整配置；提供八种预设（优先推荐仅节点）、脱敏配置预览、节点变化与独立验证结果。实验客户端仍需应用导入验收。参考和示例见[订阅与分享](docs/subscriptions.md)。

依赖优先采用免费开源的最新稳定版；核心适配依赖按经过验证的版本组合固定。当前 sing-box 1.14.4、lego 5.6.0、SQLite 1.62.0、YAML 3.0.6、x/crypto 0.59.0。版本及许可证见 [go.mod](go.mod)、[NOTICE.md](NOTICE.md)、[第三方声明](third_party/modules/README.md)。

## 文档

- [导航与日常使用](docs/navigation.md)
- [使用指南](docs/usage.md)
- [公网访问与系统服务](docs/public-access.md)
- [订阅与分享](docs/subscriptions.md)
- [架构与扩展](docs/architecture.md)
- [VPNGate 实现与边界](docs/vpngate.md)
- [开发与发布](docs/development.md)
- [兼容性与限制](docs/compatibility.md)
- [本次实施和验收记录](docs/unified-management-status.md)
- [本轮修复、重构与清理](docs/refactor-review.md)
- [性能与资源边界](docs/optimization.md)
- [历史分析（固定提交）](https://github.com/sck03/sing-box-vpn/blob/5975e773383b49dd6daca9f5406665eb47b06266/docs/unified-management-analysis.md)

许可证 GPL-3.0。历史分析与测量资料保留在 Git 历史中，不代表当前版本已重新通过其中所有实验。

当前主干的重构与接口变更见 [当前实施报告](docs/refactor-review.md)。服务端使用 sing-box，Mihomo 等客户端格式由内置转换器生成，无需安装客户端核心。纯节点、Provider 和完整配置使用独立输出标识，授权不会相互扩大。
