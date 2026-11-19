# 公网访问与系统服务

[使用指南](usage.md) · [订阅与分享](subscriptions.md) · [架构](architecture.md)

## 选择访问方式

| 方式            | 谁提供 HTTPS           | 是否需要自行安装 Nginx/Caddy | 适用场景                         |
| --------------- | ---------------------- | ---------------------------- | -------------------------------- |
| 直接 HTTPS      | 面板或订阅服务内置 TLS | 无需                         | 域名/IP、有效证书和端口直接访问  |
| HTTPS 反向代理  | 预先配置的 Nginx/Caddy | 需要已有代理，自行安装配置   | 多域名共用 443、已有代理环境     |
| 本机 / SSH 转发 | SSH 提供加密通道       | 无需                         | 面板本机维护；不作为公网分享地址 |

面板与独立订阅使用同一传输验证规则。仅填写 HTTPS 公网 URL 不会为 HTTP 监听提供加密；独立订阅必须配置内置证书或可信 HTTPS 代理。证书和私钥必须成对，域名/IP、有效期和私钥匹配后才接受，不降级为公网 HTTP。

## 面板与订阅共用 HTTPS

1. 域名 A/AAAA 指向 VPS，云安全组放行实际 HTTPS 端口。申请 HTTP-01 证书还需端口 80 可达。
2. 通过 SSH 转发进入默认本机面板，在「节点管理 → 证书管理」申请或导入证书。
3. 在「公网访问 → 面板访问」选择直接 HTTPS，例如监听 `0.0.0.0:2096`、公网 URL `https://panel.example.com:2096`，绑定该域名证书。IPv6 使用 `[::]:2096`。
4. 查看预览、保存，点击「重启并应用」。独立 systemd 助手重启面板，检查实际本机 HTTP/TLS 监听和任务 worker 就绪；失败恢复旧访问配置并再次检查，结果记录为任务。
5. 稍后访问新地址重新登录。在「权益与用量」创建权益、绑定入站并分享。

普通分享与高级编译输出都可共用面板域名，不必启用独立下载端口。保存与生效分别展示；分享只使用已应用的下载配置和运行中的面板源站。重启期间不允许再次覆盖面板设置。健康检查确认本机服务，不代替公网 DNS、云防火墙或外部反向代理可达性检查。CLI 救援可通过 stdin 调用 `yg-vpngate panel --root /etc/s-box configure`，随后 `systemctl restart sb-panel`。

直接 TLS 的 SAN 必须覆盖公网域名或 IP。内置 ACME 面向域名，IP 证书需符合颁发机构规则或自行导入。证书更新每 30 秒重新加载匹配的有效材料；部分更新失败时只使用仍有效的旧证书，过期后拒绝握手。面板已保存、运行中及重启回滚配置，以及订阅 desired/applied 引用，都纳入证书回收保护。

## 使用已有 HTTPS 反向代理

需要自行安装配置 Nginx/Caddy，程序不会自动创建代理站点。面板 URL 填 `https://panel.example.com`，监听 `127.0.0.1:2096`，可信代理填实际 CIDR，例如 `127.0.0.1/32`；代理负责证书续期，面板无需重复配置 TLS 文件。

已有 Nginx HTTPS 站点示例：

```nginx
server {
    listen 443 ssl;
    server_name panel.example.com;
    ssl_certificate /path/to/fullchain.pem;
    ssl_certificate_key /path/to/privkey.pem;
    client_max_body_size 96m;
    location / {
        proxy_pass http://127.0.0.1:2096;
        proxy_set_header Host $http_host;
        proxy_set_header X-Forwarded-Proto https;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_http_version 1.1;
        proxy_buffering off;
        proxy_read_timeout 180s;
        access_log off;
    }
}
```

面板 URL 不包含路径。代理保持 Host，并设置 HTTPS 协议头；只信任指定直接代理的头部，从转发链右侧找到第一个非可信地址。SSE 需要关闭代理缓冲和允许长连接。VMess 的 WebSocket 入口另行配置 Upgrade/Connection 请求头。

## 独立订阅（可选）

在「公网访问 → 独立订阅」启用：

- **直接 HTTPS**：监听 `0.0.0.0:2097`，URL `https://sub.example.com:2097`，绑定证书；无需 Nginx/Caddy。
- **HTTPS 反向代理**：监听 `127.0.0.1:2097`，URL `https://sub.example.com`，可信代理 `127.0.0.1/32`；自行配置代理转发。
- 代理 URL 可包含路径前缀，代理需剥离前缀再转发，并保留外部 Host。直接 HTTPS 不支持前缀。

保存并应用平台配置后启动独立服务。它只开放用户页、静态资源及下载路由；证书同样参与续期发布、备份和恢复路径重定位。后台仍用面板地址。

## 服务与工作区

| 服务                 | 职责                            | 设置入口                     |
| -------------------- | ------------------------------- | ---------------------------- |
| `sb-core`            | 普通用户接入、认证、计量        | 入站与发布                   |
| `sb-vpngate-core`    | VPNGate 用户接入、认证、计量    | VPNGate → 接入核心           |
| `sb-vpngate-gateway` | OpenVPN 隧道、出口验证、IP 切换 | VPNGate → 出口网关           |
| `sb-subscription`    | 独立用户页与配置下载            | 公网访问 → 独立订阅          |
| `sb-argo`            | Cloudflare VMess WebSocket 隧道 | 出站与隧道 → Argo            |
| `sb-panel`           | 管理 API、页面、任务调度        | 公网访问 / 管理员安全 / 维护 |

核心与出口网关职责独立，服务名和“功能设置”链接都区分。运行实例身份仍为 `normal` / `vpngate`，不等同于 systemd 服务名。VPNGate 核心依赖出口网关；验证失败阻断转发。

## 任务与运行日志

管理员可以删除单个已结束任务，清理已处理任务、清理全部已结束任务，或按数量/天数/详情预算立即回收。排队与运行中的任务不可删除。失败阶段显示“失败于”，详细结果被回收时保留最终状态；“标记已处理”让历史问题退出待关注提示，不改变成功或失败事实。

默认保留 200 条 / 30 天 / 8 MiB 详情，可配置 20–2000 条、1–365 天、256 KiB–64 MiB。清理失败记为维护异常，不影响任务终态和 HTTP worker。SQLite 删除空间供后续记录复用。

运行日志由项目专用 journald 命名空间 `sb-vpn` 管理，配置 64 MiB 磁盘目标、16 MiB 运行时目标和最长 7 天。面板提供服务/时间筛选、占用查询、有界下载和管理员清理；清理先轮转再回收项目历史日志。活动文件受 journald 管理，日志清理不会操作全局系统日志。

## 开源参考与验证边界

参考 [3x-ui 订阅 TLS](https://github.com/MHSanaei/3x-ui/blob/5297fd3c8495d80fab97ae511553fa545c69e913/internal/sub/sub.go)、[日志清理](https://github.com/MHSanaei/3x-ui/blob/5297fd3c8495d80fab97ae511553fa545c69e913/internal/web/job/clear_logs_job.go)，以及 [s-ui 订阅监听](https://github.com/alireza0/s-ui/blob/b659142befa4881a889b6c6735b966febc966fd9/sub/sub.go)、[有界日志](https://github.com/alireza0/s-ui/blob/b659142befa4881a889b6c6735b966febc966fd9/logger/logger.go)。

采用独立面板/订阅设置、内置 TLS、日志容量边界；保留本项目严格传输验证、权益计量和任务恢复模型。未复制证书错误后回退 HTTP 的行为。参考范围是固定源码阅读与本项目回归，未部署或压测这两个上游项目。实际验收见[实施记录](unified-management-status.md)。
