# 使用指南

[返回项目首页](../README.md) · [兼容性说明](compatibility.md)

## 凭据与入口设置

普通节点在菜单 **3** 设置，VPNGate 在 **17 → 14** 设置，两类节点的修改与订阅独立。

自定义凭据分别位于 **3 → 2 → 1** 和 **17 → 14 → 2 → 1**。选择“全部协议”后填写标准
UUID，同时用于 VLESS、VMess、TUIC 的 UUID 和 Hy2、TUIC、AnyTLS 的密码。单协议也可独立
修改，TUIC 可另填密码；回车或“随机重置”自动生成凭据。修改后从菜单 9 获取新链接。

普通 VMess 支持 WS/CDN 和 Argo；VPNGate VMess 使用独立 WS 入口。

## 端口、跳跃与复用

| 操作 | 普通节点 | VPNGate |
| --- | --- | --- |
| 修改主端口 | 4 → 1 | 17 → 14 → 1 |
| Hy2 跳跃 / TUIC 多端口 | 4 | 17 → 14 → 7 |
| 生成指定 TUIC 端口链接 | 4 → 6 | 17 → 14 → 7 → 6 |

范围输入支持 `31000:31100 31200`，本次输入替换旧设置；程序合并重复或相邻范围，并拒绝
与其他节点、历史国家、代理、订阅及系统占用冲突。TUIC 的额外 UDP 端口转发到主端口，
使用同一组凭据；主端口变更保留复用范围。修改复用范围不重启服务。

默认订阅包含五协议主端口，Hy2 导出跳跃参数；TUIC 可指定主端口或已配置的复用端口：

```bash
sb tuic-link 31200
sb tuic-link 31200 vpngate
```

## 分流与证书

菜单 **5** 配置普通节点的 SOCKS5、WARP 或域名解析优先级。IPv4/IPv6 是域名解析优先级，
保留可用地址回退；客户端直接传来的 IP 保持原值。现代规则支持情况见[兼容性说明](compatibility.md)。

菜单 **12** 导入 PEM 证书/私钥或使用 Certbot；VPNGate 统一 TLS 证书位于 **17 → 14 → 5**。
导入文件必须存在、密钥匹配，证书须覆盖填写的域名且在有效期内。VLESS 继续使用 Reality。
自签证书的 URI/Mihomo 使用对应客户端模式，sing-box 配置使用公钥指纹。

## 节点与订阅

菜单 **9** 先选择普通节点或 VPNGate，再查看 URI、二维码、Mihomo 和 sing-box 配置，
或主动推送到 Telegram / 私有 GitLab。VPNGate 只导出当前国家的五个节点。

| 内容 | 普通节点 | VPNGate |
| --- | --- | --- |
| 导出目录 | `/etc/s-box/` | `/etc/s-box/vpngate/` |
| 客户端配置 | `sbox.json` / `clmi.yaml` | 同名文件 |
| URI / Base64 聚合 | `jhsub.txt` / `jhsub-base64.txt` | 同名文件 |
| HTTP 订阅 | `/<令牌>/sbox.json` 等 | `/<令牌>/vpngate/sbox.json` 等 |

本地订阅在 **3 → 8** 启用或重置，两套地址使用同一令牌；重置令牌后需更新客户端订阅地址。
HTTP 的 `jhsub.txt` 路径提供 Base64 聚合，`links.txt` 提供原始 URI。公网加密传输需 HTTPS 反向代理。
GitLab 的 VPNGate 文件放在 `vpngate/`，普通订阅继续使用原路径。

```bash
sb links normal
sb links vpngate
sb export normal
sb export vpngate
```

## 核心更新与配置恢复

菜单 **8** 查询、更新或回滚核心。最低支持 1.14.0，1.14.4 为测试基线；安装和更新默认采用
官方最新稳定版。更新校验 SHA256 和两套配置，启动失败时恢复原核心，节点凭据与链接保留。

```bash
sb core-check
sb core-update
sb core-update 1.14.4
sb core-rollback
sb recover
```

`sb recover` 恢复未完成的配置提交。每次修改只保留一份修改前的临时快照，提交或恢复成功后
清理，不累积历史配置。恢复记录只存文件列表与校验值；备份缺失或损坏时保留快照并报错。
迁移已有 sing-box-yg
配置时先保存 `migration-backup.json`。升级成功后，旧 Python 模块、旧模板及另一架构二进制
移入 `/etc/s-box/待手动删除/迁移-XXXXXXXX/`，附原始路径清单，供确认后手动删除。

## VPNGate 控制与排查

菜单 **17** 管理国家、候选节点、固定/sticky 模式及暂停；**17 → 12** 仅卸载 VPNGate。
固定模式只重连选定节点，sticky 优先保持原节点；节点故障可能使恢复延长至数分钟。
换出口 IP 保持分享链接，切换国家会关闭旧国家入口，已有客户端连接需重新建立。

```bash
yg-vpngate ctl status
yg-vpngate ctl nodes
yg-vpngate ctl swap
yg-vpngate ctl mode --mode fixed
yg-vpngate ctl mode --mode sticky
yg-vpngate ctl stop
yg-vpngate ctl start
```

高级设置位于 `/etc/s-box/vpngate/gateway.json`，修改后执行 `yg-vpngate ctl reload`。
菜单 17 可配置镜像源和 ISP/hosting 筛选；默认使用官方 HTTPS 节点 API。
更多隔离、校验与资源限制见[实施方案](../VPNGate-实施方案.md)和[架构说明](architecture.md)。

菜单 **10** 查看运行日志，也可执行：

```bash
sb status
yg-vpngate ctl status
journalctl -u sing-box -u yg-vpngate -u sing-box-vpngate -n 100 --no-pager
```

已启用服务在开机启动时恢复配置和防火墙；主动停止或暂停会保留。VPNGate 为志愿者网络，
家宽筛选不代表住宅接入认证，节点在线和固定 IP 无法保证；未经出口校验不会转发。
