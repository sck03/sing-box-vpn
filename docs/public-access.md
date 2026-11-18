# 公网访问与系统服务

[使用指南](usage.md) · [订阅与分享](subscriptions.md)

## 面板与普通订阅共用 HTTPS

1. 域名 A/AAAA 记录指向 VPS 的实际公网 IP。在云安全组放行所需 HTTPS 端口；申请 HTTP-01 证书时还需临时允许端口 80。
2. 通过 SSH 转发登录默认本机面板，在「节点管理 → 证书管理」申请 ACME 证书或导入已有 PEM。
3. 在「系统设置 → 公网访问 → 配置面板访问」选择「直接 HTTPS」，填写 `https://panel.example.com:2096`、监听 `0.0.0.0:2096`，选择该域名证书。IPv6 监听用 `[::]:2096`。
4. 查看校验后的完整配置并保存，在 VPS 执行 `systemctl restart sb-panel`，访问新地址登录。
5. 在「用户与分享 → 权益与用量」创建权益、绑定入站、点击分享。地址为 `https://panel.example.com:2096/user#<随机令牌>`；期限和用量只在权益上管理。

保存配置不会立即切断当前会话。重启才更换监听与管理源站；页面区分已保存和当前生效配置。直接 TLS 会验证证书域名、期限与私钥，并校验节点、代理和下载端口冲突。续期更新 PEM 后，新 TLS 连接读取新证书。面板引用的证书材料受清理保护。

使用公网 IP 时，证书的 SAN 必须覆盖该 IP；证书颁发机构是否支持 IP 证书由机构决定。当前内置 ACME 流程面向域名，推荐使用域名或导入有效的 IP 证书。

## 使用已有 HTTPS 反向代理

在「公网访问」选择 HTTPS 反向代理，公网 URL 填 `https://panel.example.com`，监听保持 `127.0.0.1:2096`，可信代理填实际 CIDR（本机 IPv4 通常为 `127.0.0.1/32`）。无需给面板重复配置 TLS 文件。

已有 Nginx HTTPS 站点的转发示例：

```nginx
server {
    listen 443 ssl;
    server_name panel.example.com;
    ssl_certificate /path/to/fullchain.pem;
    ssl_certificate_key /path/to/privkey.pem;
    location / {
        proxy_pass http://127.0.0.1:2096;
        proxy_set_header Host $http_host;
        proxy_set_header X-Forwarded-Proto https;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 180s;
        access_log off;
    }
}
```

Nginx 负责证书安装与续期；面板只信任指定代理地址。URL 必须是协议、域名/IP、端口构成的源站，不包含路径。实时概览使用 SSE，代理须允许长连接；可在该 location 设置 `proxy_buffering off` 使事件及时送达。VMess 的 WebSocket 入口需在对应站点另行保留升级请求头。

救援时也可将同样的 `PanelOptions` JSON 经 stdin 传给 `yg-vpngate panel --root /etc/s-box configure`，程序校验后原子保存 `panel.json`；然后重启 `sb-panel`。这是当前配置接口，没有旧服务名称或数据迁移入口。

## 独立订阅服务与菜单对应

| systemd 服务 | 职责                               | 面板设置入口                                            |
| ------------ | ---------------------------------- | ------------------------------------------------------- |
| sing-box     | 普通节点用户连接、认证、计量       | 节点管理 → 入站与发布；用户与分享 → 权益与用量          |
| vpngate-core | VPNGate 用户入口、认证、计量       | 高级功能 → VPNGate；用户与分享 → 权益与用量             |
| vpngate      | OpenVPN 隧道、出口选择与可达性检查 | 高级功能 → VPNGate                                      |
| subscription | 独立用户页和配置下载               | 系统设置 → 公网访问 → 独立下载监听；高级功能 → 订阅编排 |
| argo         | Cloudflare VMess WebSocket 隧道    | 高级功能 → 出站与隧道 → Argo 隧道                       |
| sb-panel     | 管理 API、页面和任务调度           | 系统设置 → 公网访问 / 管理员安全 / 维护与任务           |

`vpngate-core` 与 `vpngate` 分进程运行：用户认证/计量和隧道生命周期不同，前者通过认证 SOCKS 转给后者。核心单元 `Requires=vpngate.service`，网关失效时转发会阻断。`user` 无法区分普通节点和 VPNGate 核心，因此采用明确的 `vpngate-core`。

可选服务不会生成额外菜单。菜单始终可用于配置；启动服务仅执行已保存的配置。运行概览的“功能设置”直接链接到上表页面。普通账户分享可复用 HTTPS 面板，无需启动 `subscription`；高级编译输出或独立订阅域名才需要该服务。在公网访问填写独立监听和公网下载 URL，保存并应用平台配置后才启动。

独立下载监听目前使用 HTTP。推荐监听 `127.0.0.1:2097`，由另一个 Nginx/Caddy HTTPS 站点转发，公网 URL 填 `https://sub.example.com`。该 URL 可含路径前缀，代理应剥离前缀后转发。后台管理页面仍使用面板源站，独立下载服务只开放用户页和下载相关路由。

## 3x-ui 与 s-ui 的处理方式

2026-11-18 对照的源码固定在以下提交，后续上游变化不影响这次说明。

- [3x-ui 设置与订阅地址构建](https://github.com/MHSanaei/3x-ui/blob/5297fd3c8495d80fab97ae511553fa545c69e913/internal/web/service/setting.go)：分别配置 webListen、webDomain、webCertFile/key，以及 subListen、subPort、subDomain、subCertFile/key、subURI。`BuildSubURIBase` 优先用订阅域名，未填时从访问 Host 提取主机名，再根据证书和端口构建 URL。
- [s-ui 设置](https://github.com/alireza0/s-ui/blob/d1c2ce92a77b32388845bc863a7f0a70871f74ca/service/setting.go)与[下载监听](https://github.com/alireza0/s-ui/blob/d1c2ce92a77b32388845bc863a7f0a70871f74ca/sub/sub.go)：同样分离 webListen/webDomain/webCertFile 和 subListen/subPort/subDomain/subCertFile/subURI；独立下载服务按订阅域名限制 Host，并使用自己的监听及证书配置。

本项目采用明确配置源站、独立订阅地址和 HTTPS/可信反代的方式。通过 SSH 转发访问时，浏览器 Host 为回环地址，不能直接成为发给用户的地址。程序在创建令牌前拒绝这种地址，并提供公网配置入口；域名解析、证书和云安全组仍须对应实际 VPS。
