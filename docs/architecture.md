# 架构与维护

`control.db` 是配置、管理员、权益、凭据、账本、下载授权和任务的权威状态。SQLite WAL + FULL 同步 + 外键。数据库 schema 17；平台配置 schema=8、节点文档 schema=6、订阅配置 schema=6、模板 version=1 是各自独立的格式版本。只接受当前配置，不提供旧数据库、订阅内容、接口或备份迁移。配置派生 JSON、订阅缓存和证书材料是不同职责。

公网传输由 `internal/publicaccess` 管理，共用 `internal/tlsconfig`；服务登记位于 `internal/servicecatalog`。主机操作按 service/deployment/units/gateway/network/journal 分文件，日志系统适配位于 `internal/hostservice`。任务保留策略与执行终态分离；面板访问重启由独立助手和持久收据完成。账户服务不再回退读取旧 JSON 配置，调用者必须注入配置/控制仓库，或读取当前数据库。

## 身份、能力与运行时投影

任务公开契约位于 `application/taskcommand.go`，HTTP 与 CLI 均只接收业务命令。自动调度标记位于管理器私有 `taskInput`，仅调度入口可设置；不接受公开 `automatic`、无意义 `scope` 或与操作无关的参数。前端 `TaskCommand` 联合类型直接由同一字段定义生成。`platform_tasks.go` 适配排队/执行，`platform_apply.go` 适配配置存储/主机服务；`application/deployment.go` 统一发布、激活、确认和恢复顺序，取消请求后恢复仍使用独立且有时限的上下文。

`runtime/reload.go` 使用固定 sing-box 的公共构造器与生命周期 scope，不使用 unsafe、反射修改私有用户表或复制五套协议实现。新监听构造成功后才释放旧监听，启动失败时恢复旧选项，多入站变更失败会逆序恢复已变更入站；恢复失败必须退出。授权代次仍由 accounting 独立校验，恢复旧监听不会恢复已撤销的账户授权。

`serverconfig.Node` 不含 UUID/密码，VPN profile 也不再生成占位用户秘密。绑定、轮换、导出统一使用账户库的规范化凭据：UUID 解析为非零值后转为标准小写格式，清除协议不使用的字段；库的唯一索引与运行时重复身份防御共同阻止跨账户身份覆盖。

生成的受管服务器 JSON 中 `users` 为空。VLESS 的 `_managed_flow` 是内部策略元数据，受管核心绑定用户时读取并删除；官方核心的配置检查使用去除该元数据的投影。不要直接用官方 sing-box 运行受管服务器文件。用户导出的客户端配置不包含此字段。

`internal/runtimeconfig` 定义受管入站、出站、DNS、端点和服务类型，供管理器、前端 `/api/capabilities.runtime` 与核心共享。实际选项及构建能力由 `runtime_validation.go` 调用发行受管二进制确认，预览/保存/应用使用相同规则。校验诊断限制为 4096 个字符，并移除配置中的秘密值。

`internal/application/credentials.go` 只依赖窄仓储接口，管理器负责任务、配置锁和发布；runtime 独立管理各入站生命周期与凭据加载确认。新增协议需要同时补充领域凭据规则、能力登记、核心注册、前端字段与端到端验收。

## 模块边界

订阅流水线为 Source → ParseResult → normalize/Process → PreparedOutput → renderer → Snapshot。纯解析、连接模型、账户模板策略与声明式模板位于 `internal/subscription`；`renderers.go` 显式登记编码器，sing-box、Mihomo、URI 的字段映射各自独立。`internal/clientprofile` 描述客户端、协议、传输、加密、插件、设备平台与已核实的导入方式；Stash 和 Mihomo 各自拥有完整模板，仅共用明确一致的策略组和路由规则构造。新增客户端必须登记编码器和能力，补充字段往返及核心/应用验收，不使用隐式文本回退。

编译缓存以 SchemaVersion、CompilerVersion、选定节点和有效模板的摘要为键，复用前验证产物 ETag/正文摘要。连接、证书、模板或规则变化会失效；来源/能力/路由安全仍每次检查。解析语义变化时递增 ParserVersion，使来源正文与条件请求缓存失效；渲染语义变化时递增 CompilerVersion；持久化结构变化时递增 SchemaVersion。sing-box 核心校验另以核心身份和产物 ETag 缓存。

subscriptions/schedule.json 是截止时间与权益修订的小型投影，不包含节点、远端凭据或内联正文。subscriptions/operations.json 独立保存投递确认和有界运行历史。subscriptions/activity.json 只包含来源、模板、规则和目标投递的活动状态及通知端点哈希；运行缓存、操作记录、调度与活动投影通过同一发布日志提交。投递仅加载操作记录及小投影，不读取或重写节点与资源正文；配置编译提交前合并最新操作记录。常规空闲调度只读 schedule.json，状态 API 只读活动、输出与通知小文件，不再扫描 runtime.json。subscriptions/output-status.json 记录输出节点数量、实际验证和最多 20 条诊断。这些文件由启动恢复和正常发布重建，配置权威仍为数据库。

认证后的 `/api/subscription-capabilities` 返回 schema、格式和协议，`/api/clients` 返回详细字段范围，`/api/subscription-presets` 返回用途预设。`/api/accounts/outputs` 按账户和实例生成实际可下载范围；用量接口的 profiles 是对象数组。`account_exports.go` 共用同一配置快照、凭据、账户模板和 PreparedOutput，供分享验证、可用性、直接导出及公开下载使用。`account_defaults` / `account_overrides` 仍由统一配置管理。

管理员 `/api/subscriptions/actions`、`/api/outbound-options` 只提供已应用对象。完整预览返回节点数量、变化和结构化 Validation；`POST /api/config/preview-output` 按 `{id,config}` 返回最多 64 KiB 的脱敏产物。公开 API 不返回原始凭据。Validation 分开记录状态、引擎、检查范围与缓存命中，不从英文消息推断结果。

`subscriptions_publication.go` 隔离抓取/编译与发布：锁外运行编码器及 sing-box 核心检查，锁内通过 AppliedState 原子读取确认配置和版本，校对 runtime-revision、订阅配置、账户节点/修订和核心身份。候选文件已发布但尚未确认时拒绝后台刷新和新的投递快照；提交时再核对一次代际。新资源失败时用旧资源重新构建健康输出。单输出检测只展开其来源图，不二次校验同一产物。连接检测统一使用 sing-box；超出能力的节点返回 unsupported，不依赖目标客户端核心。

投递与刷新共用有界 subscription 队列；probe 队列独立执行连通性检查，control 队列独立处理配置及凭据。投递分为快照捕获、两个并发传输 worker、独立限时完成记录提交和结果报告。记录按目标配置摘要匹配；无关配置变更不丢失确认，替换地址或凭据不继承旧确认。配置/凭据/隧道发布统一在提交阶段合并最新投递记录。写入后无法确认远端是否接收时记录 uncertain，可能需要重试，不能宣称恰好一次。

账户导出通过 AccountTemplates 使用同版本已发布的有效模板；本地模板引用的远程规则也是该依赖图的一部分。缺失远程依赖只阻断相关输出，不影响内置模板。状态和投递报告采用命名 DTO，由 panel-contract 同步生成前端契约。

`subscriptions/local.json` 已移除；账户节点从权威配置与凭据构造，内联节点来自当前来源。Snapshot 保存有效模板、节点指纹与校验结果；没有第二套本地节点真源。重名后缀计数按原名推进，策略组复用稳定名称分配；嵌套组检查范围和展开预算。

主机服务单元由 internal/hostservice 的纯 PlatformUnits 函数生成，manage/platform_units_install.go 负责所有权检查、写入与 systemctl。恢复测试直接检查生产生成器及运行恢复边界，不再维护一份过时 shell 单元夹具。

| 模块                                                             | 职责                                                                                            |
| ---------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| internal/domain                                                  | 稳定身份、权益/周期规则，不依赖 HTTP 或 SQLite                                                  |
| internal/application                                             | 类型化命令、账户批量绑定及轮换/解绑用例、任务详情访问规则与仓储边界；不解析 HTTP 或操作 systemd |
| internal/backuparchive                                           | 有界 tar 格式、age 认证加密与磁盘暂存；不依赖配置、SQL schema 或证书提供方                      |
| internal/clientprofile                                           | 无存储/HTTP 依赖的客户端、模式与特性目录，统一表单、授权和渲染能力                              |
| internal/manage/accounts_queries                                 | 返回结构化账户查询、凭据摘要和渲染产物；传输层负责编码                                          |
| internal/manage/account_sharing                                  | 根据已应用入口、无秘密的绑定摘要和权益状态计算可下载范围；列表、创建授权和用量共用              |
| internal/encoding                                                | 按调用场景限制大小的严格 JSON 解码                                                              |
| internal/database                                                | 连接策略、schema 版本与文件权限                                                                 |
| internal/gatewayconfig                                           | 网关类型、默认值、数值边界、URL/DNS/路径校验；供管理器和独立网关共同使用                        |
| internal/adminauth                                               | bcrypt、Cookie 会话、CSRF、角色、限速、TOTP、审计                                               |
| internal/accounting                                              | 权益激活、日历账期、凭据代次、预留/检查点、下载计数                                             |
| internal/controlstore                                            | desired/applied revision、持久任务、运行心跳、观测状态                                          |
| internal/manage/platform\_\*                                     | 应用编排、配置图校验、备份、证书、主机服务、HTTP/CLI                                            |
| internal/servicecatalog                                          | 统一服务登记：systemd 名称、职责、对应工作区、受管核心实例；供状态、能力目录和恢复共用          |
| internal/publicaccess                                            | 面板/订阅共享传输策略、URL/Host 校验与可信代理链，不依赖平台存储                                |
| internal/manage/platform_panel_settings                          | 启动访问配置的验证、预览、并发版本核对和原子保存；不参与权益计量                                |
| internal/manage/panel_restart                                    | 独立 systemd 助手应用面板设置、健康探测、自动回滚与持久终态                                     |
| internal/manage/platform_compile                                 | 候选准备、运行恢复编译、完整草稿预览及输出诊断                                                  |
| internal/manage/platform_acme                                    | 内置 lego 客户端、HTTP-01 验证、账户与签发结果持久化；复用平台任务调度与证书发布                |
| internal/manage/certificate_material                             | 独立的 PEM 匹配、域名验证与有效期检查；已解析材料供状态、续期和导入复用，无共享过期缓存         |
| internal/tlsconfig                                               | 面板与订阅共用 TLS 证书对的 30 秒刷新缓存；刷新失败仅保留未过期材料                             |
| internal/manage/program_upgrade_result                           | 升级助手结果收据、监听健康探测及任务终态；不重放升级副作用                                      |
| internal/manage/platform_task_output                             | 有界结果落盘、秘密隐藏以及取消后的部分结果保留                                                  |
| internal/manage/subscriptions_status、subscriptions_notification | 只读运行状态投影、独立通知活动记录；状态查询跳过缓存中的节点和模板正文                          |
| internal/manage/panel                                            | 原生 ES 模块 SPA，共享表单/列表/草稿，静态嵌入二进制                                            |
| internal/subscription                                            | 纯配置处理、解析、集合预算、节点选择与客户端渲染                                                |
| internal/runtime                                                 | 受管 sing-box 注册、认证后计量守卫                                                              |
| internal/vpngate                                                 | 独立 VPNGate 后端、netns/OpenVPN、出口验证和 SOCKS 转发                                         |
| internal/command                                                 | 子进程期限、取消、有界输出                                                                      |
| internal/hostservice                                             | 不依赖数据库的失败服务恢复、Argo 运行观察、可重试网络设置恢复，通过命令适配器访问主机           |
| internal/httpserver                                              | HTTP 生命周期、连接上限、独立请求并发准入，不依赖业务或数据库                                   |

面板、普通核心、VPNGate 核心、VPNGate 网关和可选下载/Argo 服务分进程运行。一个权益不创建一个进程或端口。普通与 VPNGate 同库共享额度池，网关后端不重复计费。

服务名为 `sb-core`（普通核心）、`sb-vpngate-core`（VPNGate 用户核心）、`sb-vpngate-gateway`（隧道网关）、`sb-subscription`（下载）、`sb-argo`（Cloudflare 隧道）；不注册旧名称别名。`sb-vpngate-core` 依赖 `sb-vpngate-gateway.service`，受管核心实例 ID 仍为业务范围 `normal` / `vpngate`，实例身份与系统服务名分离。前端日志入口消费能力目录，概览消费服务 DTO 的职责和工作区，不另维护一份服务名称清单。

`panel.json` 是面板监听、TLS 和可信代理的启动配置，独立于数据库中的节点/权益配置。管理员通过 `/api/panel/settings/preview` 校验，通过 `/api/panel/settings` 的版本核对保存；变更需明确重启面板才生效。节点配置与面板设置相互核查端口，证书引用纳入材料回收保护。分享地址只读取已应用下载配置与当前生效面板源站；不可分享的回环地址在创建下载令牌前拒绝。

保存的监听端口始终为 1–65535。明确 `--no-system` 的开发 `serve` 进程可在回环地址使用端口 0，由操作系统分配端口；绑定后记录实际地址并检查节点端口冲突。该例外不适用于 `init`、`configure` 或公网监听。CI 与本地共用 `scripts/test-panel-local.mjs`，负责临时目录、凭据、子进程、就绪预算和清理，HTTP 工作流独立保留在 `test-panel-api.mjs`。

数据库连接级 PRAGMA 通过 SQLite DSN 设置，每条新连接均启用外键、WAL/FULL、5 秒 busy timeout 和 trusted_schema=OFF。取消导致驱动淘汰物理连接后仍保持同一策略。节点文档移除 `app.warp`，WireGuard 只从 `advanced.endpoints` 读取；旧节点和订阅文档格式直接拒绝。

## 配置应用

初始化先校验并散列管理员凭据，构建配置通过后，在同一 SQLite 事务中写入初始配置与首位管理员。`.initializing-platform` 标记跨数据库/文件投影的恢复边界：数据库提交后投影失败，重复 `panel init` 使用已提交配置完成发布，不重新生成凭据。只有发布成功才移除标记。无标记的已有安装拒绝再次初始化。

HTTP/CLI → Validate → 校验资源引用/核心配置 → revision CAS 保存 desired+任务 → 发布日志 → 主机应用 → 新核心心跳 → applied。只有成功改变 applied revision。失败重建旧投影、恢复原 active/enabled，任务记录回滚结果。重启面板恢复已应用投影并重放幂等 apply；非幂等中断任务标失败供核对。程序升级的 `awaiting-panel-health` 阶段由独立重启助手结束，不重放升级；助手以 0600 原子写入 `program-upgrade.json`，保存任务 ID、目标/实际版本、健康或回滚结果，CLI/启动可幂等消费。

升级等待期间控制通道不执行后续任务，后台探测仍独立。`wait-panel` 经本机 `/healthz` 校验 HTTP/TLS 监听及 worker 启动完成，不依赖公网 DNS、反代或管理员登录。该接口仅允许本机来源并校验 Host。等待超过 20 分钟仍无确认结果时任务失败，要求检查系统状态；不将超时视为升级成功。

一个进程级 worker 文件锁防双管理器；控制通道串行执行配置和分发，独立后台通道串行执行订阅刷新及出站/订阅探测，慢抓取与探测不占用控制通道。排队最多 32，任务默认保留 200 条，可配置 20–2000 条，结果最多 256 KiB；控制任务期限 5 分钟，64 节点探测按统一预算为 8 分 24 秒。调度在任务之间及空闲时执行，相同自动任务去重。提交后的任务不依赖浏览器连接。新的运行实例心跳与配置 revision 分开，未重启的无关实例可继续持旧启动 revision。

凭据撤销和账本变更不参加配置回滚，不能复活已撤销用户。绑定、轮换、解除绑定与 `credential_publications.revision` 在同一事务提交；发布成功更新 `published`，核心每 3 秒读取该实例的发布版本，仅在目标版本已发布且变化时读取完整凭据快照，完成对应入站重载后确认 `loaded`。管理器等待最多 20 秒确认加载；控制通道每 5 秒核对未完成发布，失败指数退避至 5 分钟。核心连续三次重载失败或监听恢复失败会退出，由现有服务恢复策略接管。重试不重放原始轮换命令，手动停止的实例保持停止。`loaded` 表示最近一次成功启动或重载的版本，是否仍在线必须结合心跳判断。账户独立到期不依赖面板或下载监听。

配置提交在互斥锁外完成昂贵的核心校验，锁内重新核对 revision 后保存；执行时再次验证当前凭据、证书与缓存。没有仅按配置 revision 缓存整个编译结果，防止漏掉这些独立变化的输入。账户应用服务可注入已打开的控制仓库，绑定与导出读取已应用的数据库配置，不把 JSON 投影作为权威配置。

所有页面使用同一个完整配置预览入口。候选模式拒绝渲染/核心格式错误，暂不可用的账户或远端来源则返回输出级诊断；运行恢复模式可隔离损坏输出以恢复其他服务。远端来源准备结果在任务输入中有界保存，应用时重新校验这些材料，不重复获取刚接受的来源。订阅 revision 由平台保存版本生成，不是另一套客户端并发控制入口。

账户导出一次捕获完整已应用配置；候选订阅编译显式注入候选快照，地址、入口、VPNGate 与 Argo 不混读不同版本。临时 Argo 观察包含隧道设置、systemd InvocationID 与域名；同一次运行的域名不因日志滚动丢失，设置或运行代次变化后不复用。发布失败用独立有界恢复上下文还原观察记录。BBR 恢复先校验所有权与原值，成功恢复内核值后才删除项目文件，最后清理恢复记录。

## 计量与恢复

订阅编译保持无 HTTP/数据库依赖的 `internal/subscription` 边界；客户端字段与原生模板归属由 `internal/clientprofile` 提供。解析、规范化、能力判断、输出计划及客户端渲染按独立文件组织，Egern/Stash 不再混在通用模板文件内。管理层负责快照、授权、核心校验及发布；`account_export_cache.go` 只持有有界不可变编译结果，不缓存权限和用量。解析缓存与渲染缓存分别由 ParserVersion 和 CompilerVersion 失效，避免修改解析器却继续使用旧 304 节点。

前端 `dialog-session.js` 管理预览弹窗的归属、取消信号和清理；`config-preview.js` 负责内容展示。页面请求和弹窗请求的信号组合后传递至 API，响应正文读取结束仍检查取消状态。其他编辑器替换弹窗时，旧预览不能写回新编辑器。

每权益每实例共享最多 64 KiB 持久预留池，批量累计检查点幂等结算。预留先落盘再交付转发，崩溃保守占用剩余额度。关闭连接时回收无引用池；无图表观看时不影响计量。周期以 IANA 时区与日历计算，短月截断，时钟回拨不回退 period ID。旧周期检查点只结算旧账期。

计量拓扑锁不执行 SQLite 或 socket I/O；每个额度池独立串行预留。凭据代次共用最长 250 ms 授权缓存，到期/重置边界提前失效，同 Store 的策略变更立即失效。只为用量变化或关闭的池写检查点。正常关闭的 lease 按运行代次的单调序号压缩，用高水位和关闭标记防止重放；崩溃的未结清 lease 与账期保留，不能手工删除以释放额度。连续崩溃的保守占用可以累积。

备份在发布锁内使用 VACUUM INTO 取得数据库快照、复制引用证书并检查证书/私钥匹配；锁外将暂存文件以 tar + age 流式认证加密写入下载暂存文件，不再经过 JSON/Base64。数据库上限 80 MiB，完整加密文件上限 96 MiB。恢复读完 age 的认证尾部后才接受内容，校验数据库结构等于本发行 schema，拒绝额外 trigger/view、重复文件、链接和路径逃逸。同机恢复保留安全和账本；灾难恢复临时目录完成后发布，禁用所有权益并撤销 token。

恢复保留当前 ACME 资产、域名及邮箱，恢复目录内的材料可原地续期。提供方仅接受 `acme` / `import`，不保留旧 Certbot 兼容分支。ACME 账户与签发缓存不进入备份：本机登记及缓存仍匹配已发布材料时继续续期；新主机显示 registration_pending，由调度重新登记，需要公网端口 80。服务 start/restart 共用运行时心跳确认，证书更新失败走同一就绪检查和回滚路径。

lego 的账户私钥、登记与签发结果保存于 `acme/lego/`，以 0600 原子写入。每 12 小时检查续期，证书不足 30 天时申请；已签发但发布失败的结果会被复用。目录请求也受任务取消控制，HTTP-01 临时监听随验证结束释放。当前使用 Let's Encrypt、ECDSA P-256 和 HTTP-01，未接入 DNS-01 或泛域名表单。

安装器独立验证正式版本标签、包内容约定和 SHA256，不用 Release 主版本推测平台数据格式。菜单读取管理器实际版本；安装时保留当前入口，避免包内较早入口使菜单退回固定版本。基础系统包保留 VPNGate 所需 OpenVPN，不安装 Python 或 Certbot。管理员凭据通过 stdin 交给 Go；开发验收脚本使用的 Python 不属于 VPS 运行依赖。受管核心不注册上游 ACME/Origin CA 提供器，发行构建检查拒绝链接 CertMagic/acmez。

证书状态查询每个资产只读取一次当前 PEM 对，并复用 Go TLS 已解析的叶证书。登记检查用字节一致性将该验证结果复用到签发缓存；不会额外读取当前证书或重复解析私钥。引用入口按证书路径一次建立索引并稳定排序。缓存材料的解析与有效期判定分开，使过期证书仍能作为续期输入，状态查询始终检查当前文件。

每轮变化的池检查点通过同一数据库事务提交；采集计数后释放池锁再写数据库，Tick 与 Close 串行维护。额度预留仍先落盘，未扩大 64 KiB 边界。账户列表复用已读快照，只推进到期账期；导出按账户/实例索引读取凭据，并复用单次证书解析缓存。

账户开始/周期策略存为独立列，以 subscriber 索引关联订阅者，不在授权循环解析策略 JSON。IANA 时区缓存上限 128；计量维护复用句柄、池和检查点容器，并在每轮结束清空引用。密码未知用户的比较使用预计算 cost-12 bcrypt 哈希，避免所有 CLI 进程启动时计算无关密码哈希。

## 安全与资源

面板总连接上限 128，实时流与普通请求分别限制 32 个并发，超限返回 503 和 Retry-After，实时流不会占用普通请求的准入额度。下载专用监听保留 32 个连接上限。只有验证过的可信代理可提供 X-Forwarded-For；从右向左跳过可信代理，以第一个非可信地址限速，忽略客户端伪造的左侧前缀。

概览与候选保存的版本核对只查询 revision 元数据；配置页面通过 `DesiredState` 在同一次 SQL 查询中读取 desired 与两个 revision，不复制 applied 大字段。任务 outcome 在写入结果时单独存储，摘要不读取完整配置或解析结果 JSON。面板和下载监听显式复用账户仓库与控制仓库；导出只读取 applied 配置。

初始化只构建一次已校验的配置产物，提交初始配置和管理员后复用该产物发布；投影失败重试仍从数据库重建。本机 `/healthz` 不查询数据库或启动子进程，不占用业务请求额度，保持 Host、来源及 worker 就绪校验。静态页面直接使用可定位的 embed 文件读取器；JS/CSS 的 ETag 按现有文件缓存，缺失路径不进入缓存，升级后的新进程重新计算。

只允许固定类型的系统动作；没有网页任意 shell。公开面板强制 TLS/可信代理；会话存哈希，CSRF、同源、Host、CSP 与服务端角色检查。SSE 每轮复核会话，状态缓存 4 秒、刷新 5 秒、慢写 10 秒截止；页面隐藏断开，无无限消息队列。

概览 HTTP/SSE 共用采样，I/O 在状态互斥锁外执行；同时到来的读取等待同一采集，等待可取消，采集期间发生失效不会重新缓存旧数据。systemctl 一次查询多个单元。前端离页取消只读请求、阻止重复提交，临时断网保留草稿，仅认证失效退出。`live.js` 管理单一 SSE、过期回调、隐藏/退出清理及 1–30 秒指数重连抖动；`dom.js`、`tables.js`、`ui.js` 分别负责 DOM、列表和表单。JS/CSS 使用 ETag 重新验证，HTML 与敏感下载不缓存。HTTP 恢复使用有界 multipart 二进制文件，密文暂存到权限受限的临时文件，错误和取消后关闭删除；解密仍保留有界明文材料。单个面板进程同一时刻只接受一个备份/恢复请求。

授权 Tick 一次批量查询活跃凭据及权益，账户账期只推进一次，不逐凭据重复 SELECT；同 Store 变更通过 epoch 失效，其他进程的变更仍在复查周期发现。下载授权最多 10,000 个有效记录，失效历史最多 1,000 条/30 天；创建时及每小时回收。账期保留最近 120 期，存在未结清 lease 的旧账期不删除。撤销凭据 ID 与 lease 高水位永久保留，不能回收以复活历史身份。

已结束任务默认保留最近 200 条/30 天，详细结果默认预算 8 MiB，支持策略配置、删除和处理标记；超出部分保留明确的结果已回收标记。执行状态与业务 outcome 分别展示：passed、partial、failed、cached。刷新报告包含检查数、成功数、输出数、失败来源和最后成功时间；没有可用输出时不会标记为缓存成功。账户与下载授权 GET 接口使用分页 envelope；账户只推进当前页账期，显示订阅者停用的有效状态。空任务队列只读探测，不先取得 SQLite 写锁。批量绑定通过唯一表达式索引检查认证身份，每批只回收一次事件历史。

HTTP 统一完成 Host/Origin、会话与 CSRF 检查后分发给 `platform_http_accounts.go`、`platform_http_subscriptions.go`、`platform_http_config.go`、`platform_http_tasks.go`。新增接口经过同一入口。连接限额包装原始 TCP，TLS 由标准 ServeTLS 建立，保留 request.TLS 和 HTTP/2；独立下载服务另有请求准入，不能用 HTTP/2 多路复用绕过资源边界。

公开首页、用户页、静态资源和能力元数据允许跨站 GET 导航，带令牌下载支持 GET/HEAD；Host 和代理来源检查仍执行。管理 API 和所有写请求继续校验 Origin/CSRF。`GET /api/capabilities` 仅返回发行级约束，不携带安装状态或秘密。

任务 DTO 的 `details` 描述允许访问详情的操作员范围，前后端复用同一规则：管理员可查看，操作员可查看服务/网关任务及自己提交的 apply，viewer 不可查看。服务端始终重新授权。前端 `tasks.js` 负责详情呈现，`sharing.js` 复用权益与授权页的分享流程；按钮忙碌状态与业务禁用状态独立维护，异步结束不会错误启用末页按钮。旧订阅配置写入/计划/归档适配器只用于测试，不编入生产。

`sb-recovery.timer` 在开机 5 分钟后、此后每 5 分钟加 0–30 秒抖动触发独立 oneshot。只重试项目服务名单中已安装且 failed 的单元，reset-failed 后异步启动，inactive 保持停止；数据库损坏不阻止此控制器运行。配置应用、服务操作、升级回滚共用 `.service-recovery.lock`，避免恢复与人工变更交错。永久错误仍需修正，定时重试不能修复无效配置或损坏存储。

每小时清理直属 `certificates/` 下带 `.managed-material` 标记、超过 7 天且未被 desired/applied 引用的目录；保护未保存草稿的宽限期，保留外部证书、ACME 账户/签发缓存和无标记目录。导入与清理共用发布锁。启动或显式恢复遇到可解析读取但内容损坏的订阅运行缓存时，将原文件隔离到 `subscriptions/runtime.corrupt.json`，记录 cache-recovery 并重新获取来源；I/O 错误仍失败。该诊断文件只保留最近一次。

下载授权统一保存在 download_grants，权益授权选择客户端格式，输出授权选择 output ID，两者互斥。账号与输出下载共用撤销、有效期及原子次数检查。下载监听复用数据库连接，不按请求打开 SQLite。远程模板/规则正文与 ETag 同存有界运行缓存，编译时覆盖配置中的回退值，不写回期望配置；删除资源或修改 URL 同时移除旧缓存。后台仅为到期的刷新和自动分发排队。

VPNGate 转发必须通过独立 namespace 的已验证出口，DNS 和公网 IPv4 拨号在隧道 worker 内执行；出口验证失败或身份改变立即撤销准入。防火墙/网卡/namespace 仅处理完整所有权匹配资源，不清空用户系统规则。OpenVPN 输出流有 2 MiB 日志预算和持续消费，cloudflared 使用 journald。

## 扩展方式

### 权益读写与生命周期

`domain.Account` 保存管理员的配置，`accounting.Usage` 另外提供只读的 `effective_expires_at` 和 `archived`。计量缓存、预留和授权都使用实际截止时间。归档在同一 SQLite 写事务中校验 revision、标记权益、写入凭据撤销记录、移除绑定、更新凭据发布版本并撤销专属下载授权。事务保留原权益、账期和租约，已预留的流量仍可结算。其他进程通过已有授权刷新周期看到撤销。

| 接口                         | 契约                                                                                                                                                                                                                         |
| ---------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /api/accounts`          | 默认分页查询当前权益。`archived=true` 只查归档记录。`expires_at` 是配置值，`effective_expires_at` 是计算结果。                                                                                                               |
| `GET /api/accounts` 分享字段 | `download_scopes` 是有启用入口与对应凭据的实例列表；`download_denial` 是权益或入口禁止原因，不返回凭据秘密。                                                                                                                 |
| `POST /api/accounts/archive` | 管理员提交 `id`、`revision`。成功释放名额，过期 revision 或重复归档返回 409。                                                                                                                                                |
| `GET /api/grant-options`     | 管理员读取已应用的输出 ID、名称、监听启用状态 `listener_enabled`、版本差异 `pending` 及地址问题 `unavailable`。创建授权时服务端重新校验。                                                                                    |
| `GET /u/{token}/usage`       | 返回 `effective_expires_at` 和 `download_denial`。空字符串表示入口就绪；`no_bound_inbound`、`quota_exhausted`、`download_limit` 分别表示无可用绑定、流量耗尽和下载次数耗尽，仍可查看用量。客户端格式兼容性在导出时再次校验。 |

`accounting/reservations.go` 按需启动每 Store 的预留写入器；队列和批次各有 64 个请求上限，只合并已经等待的请求，不增加聚合定时器。每个池仍受原有额度与代次检查，批次内用保存点隔离业务失败，整个事务 FULL 提交后才返回可转发字节。关闭 Store 前排空已接收请求。无预留的面板查询不会创建该写入器。

账户分享的用量访问与下载资格分离：次数耗尽仍可查看权益，撤销/过期仍拒绝。未撤销且未过期的账户分享纳入 10,000 条有效授权预算，即使下载次数已耗尽也不会按无效历史清除；输出分享耗尽后仍可回收。

前端 `lifecycle.js` 区分页面读取与用户工作流。页面重绘取消页面读取，弹窗绑定/分享只随会话结束取消；异步响应另有会话代次保护。网络页面按入站、路由、出站、VPNGate 拆分，字段规则位于 `network-fields.js`。国家准备接口接收完整当前草稿，返回候选配置；返回前草稿变更或重载会拒绝覆盖。表格索引缓存搜索/排序标量，实时更新焦点行数据，延迟重排和操作闭包刷新，期间阻止执行过时操作。

`page-catalog.js` 统一页面权限、分组、标题、搜索关键词、动态模块加载和页内定位；`navigation.js` 只负责侧栏、搜索及同组标签，`app.js` 负责会话、路由和完整草稿。新增页面登记后即可进入路由和搜索；服务设置链接消费同一目录。非法或越权地址归一化，空 hash 回到概览。

新增维护、出站或订阅标签时，在目录登记稳定的 section ID、标题、关键词和可选角色，在页面传给 `workspace` 的对象中提供同名内容工厂。搜索、路由和标签取同一份可见列表。URL 是选中状态的唯一来源；同页切换只通知当前工作区，不重载模块或表单。跨页和登出取消旧视图读取；工作区首次打开某个标签才启动其加载器，失败可重试。

`sections.js` 统一可选内容的延迟创建、并发合并、页面内复用、取消和失败重试，`workspace.js` 管理可键盘操作的标签。权益页只先读取分页权益，用户分组展开时读取，客户端目录在导出时读取；出站、维护、订阅按标签创建内容。查看省略可选字段的订阅配置不会改写草稿。`capabilities.js` 缓存公开约束，`outbound-fields.js` 维护协议字段规则。订阅表单、分享和直接导出使用同一客户端目录。账户操作按策略 revision 判断是否过时，单纯流量变化不会禁用按钮。SSE 在会话建立后启动，内容不变时只发送小型 tick；权益页至少间隔 5 秒刷新用量并合并并发刷新，概览不重复重绘。

来源、集合、输出、分发编辑通过 resourceEditor.updateItems 局部更新，保留搜索、排序和分页；workspace 使其他标签按需失效，避免旧选项残留。分享与导出使用 account-outputs.js，实例切换取消旧检查并隔离迟到响应。subscription-wizard.js 先生成完整候选、预览，再核对草稿后加入；template-settings.js 负责预设、全局默认及账户覆盖。前端仍为原生 ES modules。

服务单元先全量检查归属再写入，符号链接和非项目单元拒绝覆盖。安装失败和卸载只停用、删除确认为本项目所有的单元。旧配置/凭据/端口写入辅助函数及旧预览适配器只存在于测试夹具，协议实测通过现行账户导出 API 取得材料。

新增协议先加入能力登记、领域映射与运行时认证适配，再通过 TCP/UDP、两个账户、禁用/到期/轮换验收。新增客户端扩展 subscription renderer；新增通知实现目的地适配；新增节点应提供 RuntimeID 和独立额度分配策略。当前只支持单机共享 SQLite，不把该模型误称分布式全局严格额度。

高级核心 JSON 局限于适配边界；字段保留、引用和循环检查必须通过。VPNGate 保护路由不被普通高级对象覆盖。未来拆分 manage 时以业务模块为界，不再增加平行配置真源或重复表单业务。

## 强类型配置与任务边界（2026-11-19）

`internal/serverconfig` 独立定义托管 Node、TLS、WebSocket 传输和字段约束，不依赖 HTTP、SQLite 或主机服务。`App.Nodes` 是内存与数据库中的唯一节点来源，正常节点不再保存 Profile / 动态 Inbounds 副本。`App.Inbounds()` 返回独立核心投影；VPNGate Profile 在初始化边界经 `MakeNodes` 转为同一节点契约。节点文档 schema=6，数据库 schema=17，旧数据直接拒绝。

托管 VMess 只接受 WS，支持 path/host/early_data/early_data_header；不支持的传输与未知字段明确拒绝，不能接受后丢弃。新节点默认关闭 early-data，用户开启后按真实目标客户端能力判断。关闭时核心投影显式选择 WS 请求头模式，避免上游按编码后的 RequestURI 比较中文路径；开启 early-data 且路径含非 ASCII 字符时必须指定请求头。Reality 私钥、short ID、flow 与握手信息可通过表单编辑。订阅导入模型仍单独支持更广的协议与传输，不与受管服务端模型混用。

账户服务必须注入账户仓库及配置仓库（或明确的候选快照）；请求路径不再隐式打开数据库。独立刷新/探测在 `openAccountSource` 边界拥有明确的仓库生命周期。生产发布传递 Core 验证器，协议验收夹具使用相同依赖。

控制队列将连续的账户命令合并为最多 16 条的一批，在同一主机服务锁内依次提交，再统一发布凭据；遇到其他控制任务立即停止组批。每条命令保留独立结果和审计，失败不撤销已提交凭据，也不重放非幂等操作。发布失败继续使用持久版本对账。受管核心按入站比较用户列表，仅替换变化入站。独立 scope 关闭该入站的连接并释放监听，未变更入站与全局路由/DNS/出站保持运行。此行为不是单用户无缝热更新。

编译缓存同时保存不可变的解析验证结果，键含 CompilerVersion、ParserVersion、输入摘要与模板；权限、撤销、用量以及依赖核心二进制/证书文件的验证不进入这个缓存。账户授权快照复用 SQL 扫描参数容器，减少按凭据分配；持久预留、WAL/FULL 和额度语义不变。

`go run ./cmd/panel-contract` 从 Go 导出面板边界类型到 `contracts.d.ts`，CI 用 `-check` 拒绝漂移并检查 API 客户端类型。业务错误统一为 `{error:{code,message,field?,retryable}}`；版本冲突为 409，字段错误为 400，存储故障为 500，临时不可用为 503。前端 RequestError 保留状态、代码与字段信息。

任务详情使用页面取消信号及 dialog-session 归属判断，关闭、切页或打开新详情后，旧响应不能覆盖当前对话框。模板资源按策略组/远程规则/本地规则选择展示，非活动编辑器不创建 DOM；空列表与短资源列表隐藏冗余搜索分页。

## 访问与组件维护的模块边界（5.1）

- `internal/certificates` 持有证书资产、具名状态 DTO、有界 PEM 读取/解析和续期调度器；材料的域名匹配与有效期交给 `tlsconfig`。调度器拥有修订缓存、扫描截止时间和互斥状态，通过 `RenewalSource` 读取已应用登记及持久尝试记录。`manage/certificate_schedule.go` 适配仓储、任务队列和 ACME 登记；签发与服务恢复仍由宿主机编排完成。
- `internal/deployment` 校验正式发行标签、固定仓库资产地址、SHA256 元数据和数值版本顺序。HTTP 层只调用组件状态/更新用例，外部查询不自动触发安装。
- `versions.json` 统一记录受管核心、官方校验器和 lego 版本，测试校对 Go 依赖。管理器提供 `--runtime-version`；发行安装使用候选管理器默认的已验证校验器版本。
- 程序升级先准备对应校验器，再用候选管理器、受管核心和校验器验证当前配置；切换失败或面板健康检查失败同时恢复三个二进制。运行/开机状态保持既有语义。
- `secret-field.js`、`certificate-fields.js`、`certificate-editor.js` 共享表单控件；`program.js` 负责组件维护视图。访问预览使用 `dialogSession` 绑定请求生命周期，关闭/替换弹窗后旧请求不能写入新窗口。
- 模板导入位于 `subscription/template_import.go`，不引入第二套渲染器。受支持输入转成现有 Template，保留来源，再经过原有能力校验、编译和发布。

新增接口：管理员 `GET /api/program`、`GET /api/program?check=true`、`POST /api/certificates/prepare`、`POST /api/subscription-templates/import`。对应模型由 panel-contract 生成到前端。版本查询失败返回 check_error，不把失败描述为没有更新。

未使用的官方校验器 activate 路径已删除，其专属实现测试同步移除。旧工作流测试需要的 systemServices helper 只放在 \_test.go，不再进入生产二进制。DNS independent_cache 已从表单移除，后端明确拒绝该已废弃字段。

续期调度使用原子的 `AppliedStateSince` 读取已应用修订，未变化时 SQL 不返回配置正文；只有修订变化才解码并缓存证书登记。每十分钟扫描材料，同一证书成功入队后十二小时内不重复调度；队列错误在下次调度重试。待应用或应用失败的无关配置不会阻断在线证书续期，未应用证书不会提前签发。额度账本 WAL/FULL、预留和撤销保证保持原语义。

订阅手动与自动任务统一经过 `application.ExecuteSubscription`，通过 `SubscriptionOperations` 调用刷新、检测或投递。`manage/subscriptions_application.go` 只适配基础设施，不重复分发。刷新是全局操作，不能携带 ID 或 probe_url；检测必须有有效输出 ID 和 HTTP(S) 探测 URL；手动投递必须指定有效目的地 ID，不能携带探测参数。自动调度单独使用 `ValidateScheduled`，仅允许无目标、无探测参数的全局刷新或到期投递；公开 API 不能设置 automatic。Go 校验、生成的命令联合类型、HTTP 与类型正反例共同约束这些语义。

`configuration_queries.go` 提供只读配置查询用例，将选项构造与 HTTP 适配分开。`AppliedViewSince` 在一次数据库快照中读取 desired/applied 修订号，仅已应用修订变化时返回配置正文。每个服务持有一份不可变解码结果，供选项、网关、隧道轮询与账户导出使用；账户权限、撤销、额度及隧道观察仍实时读取。调用者不得修改共享 map/slice；写命令使用独立候选。服务/运行实例/订阅操作/出站选项采用明确 DTO，前端 `configuration-api.js` 参加 strict 检查。

VPNGate 的客户端准入与 worker 转发流分别受 max_connections 限制，pending dial 与已建立转发流共享后者；UDP 关联套接字归控制连接所有，不再次扣减准入。撤销时关闭两类连接并取消拨号。面板端口校验同时保护保存值与当前监听，重启完成后释放旧端口。账户配额最大为 `2^53-1` 字节，确保 JSON 与前端安全整数一致；null 仍表示不限额。

`subscription.Schedule` 拥有纯调度规则和轻量投影；`manage/platform_scheduler.go` 只加载文件、读取账户有效状态和入队。投影不包含节点正文、模板正文和秘密。缺失投影可从当前配置与期限字段重建，这属于派生缓存恢复，不是旧数据迁移。

表格按稳定 ID 更新，出站、DNS 和端点显式使用 tag；远程表格透传 rowKey。焦点存在时保留控件和行序，立即更新值，使用渲染时保存的操作签名判断动作是否失效；原地修改和重复标识也不能运行过期动作。全部 JS 继续 checkJs，API、账户 API、证书字段模块额外通过 `tsconfig.boundaries.json` 的严格检查。

## 输出用途与维护边界（6.0）

clientprofile/output.go 定义稳定输出身份（格式与用途）；subscription/account_output.go 负责纯兼容性规划，account_policy.go 校验模板 client/mode。manage 负责配置快照、授权、编译缓存和核心验证；HTTP 不重新定义模板业务。账户导出请求与凭据命令为不同的 application 类型。

授权 profiles 精确匹配输出身份；数据库 schema=17 的 profiles 列取代旧 clients 列。平台 schema=8、订阅 schema=6，CompilerVersion=7；旧数据或旧接口明确拒绝。内置默认不再复制成账户配置，远程模板发布故障不阻断不依赖它的内置输出。

列表只分析节点，创建授权与下载才编译所选格式。面板配置预览、保存、导出、授权等重请求独立限制为 2 并发；每个下载监听的账户请求限制为 4 并发；状态、任务及 SSE 保持独立预算。ConfigurationState、AccountExportRequest 与 AccountExportResponse 由 Go 类型生成前端契约。只读状态递归擦除结构化秘密值，保留字段类型。

loadDialog 在首次查询前占有对话框；切页、关闭或替换会取消读取并阻止迟到响应。账户保存后的绑定向导拥有独立工作流会话，允许保存触发的页面刷新。简单规则与策略组、远程规则的能力独立控制。

卸载停止项目服务后调用与面板相同的网络恢复流程，成功后才删除数据库和程序。deployment/retention.go 只清理带项目所有权和成功标记的升级目录，保留最近两份；失败目录和无标记目录不自动删除。服务端不安装、探测路径或启动 Mihomo；客户端原生校验与互通仅存在于测试代码和 CI。

## 命令入口、适配与后台维护（2026-11-20）

生产入口为 `cmd/sbvpn`，负责管理命令与 VPNGate 命令装配，发行文件名仍由 release 构建定义。VPNGate 的运行、控制、SOCKS 与 namespace 实现位于 `internal/vpngate`；生产代码不导入 manage。统一入口注入配置事务，保留现有发布恢复和跨进程互斥，测试夹具也使用同一事务。没有复制第二套配置发布流程。

`internal/statefile` 负责有界 JSON 与持久原子文件写入，`internal/filelock` 负责平台文件锁，`internal/terminal` 与 `internal/country` 提供共享显示规则。manage 中保留窄适配调用；账户、控制仓库、运行时与订阅编译器继续独立。程序仍为模块化单体，运行实例和后台进程保持原有隔离。

`platform_service.go` 仅组装资源与生命周期；configuration_view 负责脱敏投影，platform_start 负责恢复后启动，node_presets 负责预设与国家准备。读取不带秘密的配置直接从持久 JSON 生成脱敏视图，减少重复往返；查看或修改界面不会改写原始秘密。单协议预设仅分配该协议的端口，VLESS / 默认 VMess 不生成未使用的 PEM。

历史回收拥有独立有界 worker；不在每次交互任务认领前串行执行清理。证书回收仍持有配置和发布锁，SQLite 回收仍可能与写入竞争，不能承诺零等待。关闭时取消并等待所有 worker。凭据对账、配置应用和主机状态变更继续由控制通道按顺序执行。

`runtimeconfig.Capabilities.outbound_fields` 统一 TLS / 必需 TLS / uTLS 字段能力，前端显隐与清理消费同一目录。AnyTLS 改名保留指纹，QUIC 协议不显示 TCP uTLS。编辑用节点和证书集合规范化为空数组，避免表单写入临时数组。

账户字段定义与开通工作流分离。创建后可自动排队绑定，任务成功后读取单账户当前状态再进入分享；队列失败保留已创建权益。工作流不继承已刷新页面的取消信号，但对话框替换与注销仍终止对应读取。公开下载监听显式提供平台筛选模块，未扩大管理 API 的访问权限。

## 有效状态、刷新阶段与前端生命周期

`accounting.AccountUsage` 与分页列表共用有效状态处理，结合订阅者停用与当前账期，不激活首次使用权益；基础 `Account` 查询供内部账务操作读取原始记录。SQLite 的主键/唯一冲突在仓库边界转换为 domain.ErrAlreadyExists，HTTP 映射为 409/already_exists，其他存储故障继续作为故障处理。

刷新使用 subscription 队列，来源抓取 90 秒、资源抓取 45 秒，每阶段最多 4 并发；最久未检查者优先，未完成输入保持原调度状态。候选准备复用抓取器，不再持有刷新锁等待网络。配置/账户输入/核心身份在最终提交时重新检查，防止并发配置更改被旧结果覆盖。

刷新结果包含 checked、updated、failures、outputs、expected_outputs、published、deferred、interrupted。手动和自动任务共用执行器，错误和成功都通过独立 5 秒保存预算记录报告。取消后的来源检查点不发布产物或未经验证的规则，版本不匹配则拒绝保存。分发快照由独立函数在发布锁内捕获，网络发送位于锁外。

页面上下文拥有独立 updateOverview 槽，主页面事件分发读取当前槽，支持异步注册；取消后的视图不再接收更新。分享授权高级选项与后端 expires_at/max_downloads 一致，其权限只能收窄权益。

取消后保存的来源进度携带 refresh_pending 标记；即使来源 Next 尚未到期，调度器也会安排继续发布，成功发布后清除标记。该标记属于派生运行状态，不修改期望配置。

## 配置与表单边界（2026-11-25）

平台 schema=8、节点文档 schema=6。普通路由和出站的唯一来源是 `app.advanced`：默认只有 direct 出站、本地 DNS 与 direct 默认路由。旧 `app.routing`、`app.socks_port`、`app.argo` 已移除。需要 SOCKS 时在出站资源中显式创建并在路由中引用。

隧道期望状态仅保存于 `tunnel`。归一化派生固定域名或关闭状态，`observedConfig` 只在临时隧道设置相同时注入运行观察；该字段不序列化。候选预览、应用编译和下载均使用该流程，单次编译中的账户来源共用捕获的观察值。

`ConfigurationPreview`、`OutputPreviewRequest`、`OutputPreview` 由 Go 定义并自动生成面板声明；HTTP 层负责解码和授权。`form-dialog.js` 负责字段状态、验证与提交，`form-values.js` 负责结构化解析器转换，`ui.js` 组织资源列表。DNS/出站/默认路由接受标签或 JSON 解析器；未改动的对象及未暴露的高级字段保留。

账户列表缓存只保存按 AppliedRevision 派生的入站可用集合。`AppliedStateSince` 用一次 SQL 快照同时读取版本和必要时的配置正文；待应用版本不改变缓存。缓存只有一份不可变集合，新版本替换旧集合；权益、配额、到期和实际凭据绑定每次查询。

删除 Profile 自签标志、Reality 公钥副本、Profile/App country 和 profile_order：自签/公钥由有效材料派生，国家由 profiles 键与 active_country 表达。VPNGate 的 proxy_port 保留并提供界面入口，修改时同步 gateway.listen，最终由服务端校验引用和端口冲突。

## 发布校验、部署计划与探测取消（2026-11-25）

- `internal/domain/address.go` 提供裸 IP / DNS 名称约束，平台地址、可选 CDN 地址、托管节点 SNI/Reality 目标与订阅节点复用。拒绝 URL、端口、路径、控制字符与 IPv6 zone；IPv6 地址不带方括号。平台字段错误返回 invalid_field 与具体 field。CDN/Argo 转换完成后再次验证最终订阅节点。
- `internal/manage/deployment_plan.go` 是纯部署差异计算。比较实际服务输入的规范 JSON，避免 JSON 数字的 Go 类型差异导致误重启。普通核心使用实际服务端配置，VPNGate 核心只使用当前国家生成配置，网关使用自身策略，下载服务使用监听设置。临时 Argo 只比较模式和绑定入站的本地 scheme/port，固定 Argo 只比较模式和 token；发布域名变化单独更新订阅。
- ServiceChange 同时供预览和执行使用。预览标注观察到的运行状态及 applied_revision；no-system 只给出服务运行时的潜在操作，不伪造主机状态。实际应用重新计算计划，保持手动停止的服务停止。
- 三个任务队列各有一个工作协程；probe 仍有 64 节点、2 并发、每节点 12 秒、总计 504 秒的预算。全局待处理上限 32，probe 最多占 8，避免探测填满队列。ClaimLane 在取得写锁后再次检查运行任务，防止并发认领同一队列。
- 数据库 schema=17 增加 probe/subscription 队列及 cancel_requested。仅运行中的 probe 和排队中的非 apply 任务可取消。取消先落库，再中断已注册上下文；注册后再次读取持久标志，覆盖认领到注册之间的竞态。运行资源释放前保持 running，Finish 原子产生 cancelled；进程恢复尊重取消请求。Task.cancellable 与 Task.cancel_requested 是前端唯一的取消能力来源。
- Argo 观察与日志管理共用 servicecatalog.Journal。读取当前 invocation 的日志时指定 --namespace=sb-vpn；已确认域名仍按 invocation 缓存。

旧数据库及包含旧数据库的备份直接拒绝，无迁移器、旧队列别名或旧接口适配层。平台/节点/订阅配置 schema 与备份容器版本未因任务表变化一起递增；ParserVersion=3、CompilerVersion=7 使收紧地址语义前的解析与编译缓存失效。

## 选择模型、显式绑定与后台诊断（2026-11-25）

- `selection-model.js` 只管理去重、顺序、限额和选中状态；`choice-field.js` 负责原生复选框、过滤、分页、键盘和可选排序；`token-field.js` 负责自由多值输入。它们不发请求，也不持有页面全局监听。`form-dialog.js` 组合控件、依赖更新、验证和保存；候选列表未变化时不重建，普通勾选保留焦点。新业务只提供字段元数据，避免每页维护一套多选实现。
- 账户输出表单负责查询生命周期，区分查询中、检查成功和检查失败；失败后禁止用旧权益的能力保存，重试入口不被设备筛选覆盖。设备过滤只控制可见项，实际能力改变才移除无效选择并提示。协议选项来自客户端目录，出站/路由/规则集使用当前资源列表。
- `account-api.js` 集中账户保存/归档、候选、凭据和分享请求。DTO 从 Go 生成，绑定任务的 `inbound_ids` 是非空元组，运行时同样验证。application 只处理明确选择的入口；不在执行时扩张为全部。HTTP 继续负责解码、角色和 CSRF，账户存储仍由 repository 注入。
- 分发任务一次打开账户仓储供两个 worker 共用，关闭由任务拥有者负责；每次发送前仍实时校验权益。已经取得确认配置的订阅捕获只打开所需的账户仓储，不再重复打开控制仓储。`checkOutputAccounts` 要求显式仓储依赖，调用者不能依赖函数隐式打开/关闭数据库。
- 生产发布始终要求数据库确认代际。未初始化订阅的特殊放行分支已移除；纯编译测试通过 `_test.go` 初始化实际确认状态，不改变生产路径。
- `backgroundHealth` 保存进程内有界摘要，最多 16 类问题，按操作记录首次/最近失败与累计次数；成功清除，进程退出丢弃。只暴露固定分类消息，同类型日志至少间隔五分钟，不把 URL、令牌或原始命令错误写进概览。它不替代任务和审计账本。概览快照/SSE 推送该字段，前端展示对应说明；调度器、凭据发布和审计写入错误不再被静默忽略。

入站、出站和端点的资源编辑更新本地列表并使相关页面失效，保留搜索、排序和位置。跨区配置应用、需要重新推导数据的操作继续通过原有刷新入口。模块边界以状态所有权和生命周期划分，不增加新框架、额外服务或多套兼容接口。

## 恢复、入站归属与存储生命周期（2026-11-26）

- 公网访问分为传输策略验证与证书材料验证。新配置、预览和保存继续检查证书域名、密钥与有效期；已确认配置的恢复启动只校验传输策略，TLS 握手通过 tlsconfig.Cache 独立拒绝无效材料。后台 worker 不再被面板或下载监听器的过期证书阻断。buildApplied 仅用于已确认状态的恢复、对账和回滚，新候选仍使用完整验证。
- 证书状态查询在 certificate_status.go 合并 desired/applied 登记，提供 saved/applied/pending/can_renew/renewal_unavailable。资产材料和操作以 applied 为准，草稿移除不隐藏在线资产。新资产 ID 同时检查已保存和已生效登记，防止应用失败后复用在线材料目录。排队及执行都重新确认生效登记；独立 CLI renew-certificate 持有服务锁，不依赖面板 worker。前端 certificate-page.js 独立管理列表和能力展示。
- 普通入站的 udp_ports 直接属于 serverconfig.Node。端口预留按入站 ID 区分所有者，禁止范围重叠和覆盖监听端口；防火墙、账户导出及前端编辑消费同一字段。停用入站保留预留但不生成转发，复制入站清空额外端口。删除或重排其他同协议入站不改变归属。VPNGate 的国家 Profile 在 MakeNodes 边界映射为同一字段。
- App 不再包含 hopping/tuic_ports，不再通过“第一个同协议入站”推导普通配置。平台配置 schema 8、节点文档 schema 6；数据库 schema 17、订阅 schema 6 和备份容器 8 不随之递增，旧配置明确拒绝。
- 订阅刷新和投递从 PlatformService 借用账户及控制仓储，独立调用只在入口创建并关闭一组仓储。代际确认、编译前后账户捕获与投递授权复用这一组依赖，不逐检查重新打开 SQLite。
- 预留请求复用对象及响应通道，返回后清除上下文和账户标识。队列容量、FULL 提交后才返回额度、取消后的确定提交结果与 64 KiB 单池边界保持不变。
- panel-contract 生成关键查询的 QueryResponses 和写入的 MutationContracts。前端 query/mutate 按端点推导请求与响应；类型反例阻止错误端点、错用 DTO 和遗漏证书申请字段。其余动态查询继续由现有具名 API 包装器约束，未声称全前端 strict。
