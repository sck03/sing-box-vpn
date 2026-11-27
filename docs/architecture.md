# 架构与维护

`control.db` 是配置、管理员、权益、凭据、账本、下载授权和任务的权威状态。SQLite WAL + FULL 同步 + 外键。数据库 schema 18；平台配置 schema=8、节点文档 schema=7、订阅配置 schema=6、模板 version=1 是各自独立的格式版本。只接受当前配置，不提供旧数据库、订阅内容、接口或备份迁移。配置派生 JSON、订阅缓存和证书材料是不同职责。

## 依赖方向与状态所有权

- `domain` 定义身份、权益、任务来源与提交值对象，不依赖 HTTP、SQLite 或应用服务。
- `application` 编排账户、发布和历史清理用例，通过窄仓储接口访问数据；不解析 HTTP、不操作 DOM、systemd 或具体 SQL。
- `accounting`、`controlstore` 分别拥有业务表、事务和资源预算，只依赖领域类型；不能反向依赖应用用例。
- `manage` 负责装配、鉴权与传输、平台发布和主机适配。新流程先明确其状态所有者，再决定模块，避免只拆文件却继续共享全部平台状态。
- 前端保留原生 ES modules、统一表单/表格/弹窗组件及现有窄屏布局，不增加生产框架依赖。

## 任务来源、幂等与执行

`domain.TaskSubmission` 将审计操作者 `Actor`、可信来源 `Source`、请求标识与指纹分开。HTTP 固定来源 user，CLI 固定 cli，内部调度器固定 scheduler。管理员可以叫 scheduler，其名称不会开启自动去重。公开 TaskCommand 不接受这些内部调度字段。

`controlstore/task_queue.go` 负责队列准入；`task_submission.go` 负责持久请求回执。调度任务只在相同来源、操作者、队列、类型、输入且仍在排队/执行时合并，返回已有任务真实编号。已确认后再次提交产生新意图；未确认的同一请求重试返回原任务编号，已完成任务也不重新执行。

`POST /api/tasks`、`POST /api/config`、`POST /api/panel/restart` 接受可选 `Idempotency-Key`。面板自动生成。HTTP 对端点与解码 DTO 计算指纹，仓储在同一写事务中确认回执、修改 desired、创建任务并记录回执。相同操作者和 key 搭配不同内容返回 409；原任务历史已经删除返回 410，不偷偷创建新任务。旧任务的执行状态和 outcome 仍按原规则查询。

回执保留至少 24 小时，运行中任务不按时间回收，总量最多 10,000；容量不足时整次入队回滚。前端每个登录会话最多保留 128 个未确认请求，只在该标签的 sessionStorage 保存摘要、随机 key 和时间，不保存正文或秘密。网络/超时/服务端故障保留 key，确认响应或确定的客户端错误后释放；退出登录清除。超过重试窗口要求用户先核对结果。没有 key 的调用不承诺请求重试幂等；文件上传恢复和普通资源 CRUD 不属于此任务回执协议。

control/subscription/probe 各有一个工作协程和容量为 1 的唤醒通道，保留每秒轮询发现其他进程入队。总待处理上限 32，probe 最多 8。配置修订采用乐观并发；应用在服务锁内校验、采集服务基线并持久检查点，发布失败走原回滚路径。运行中的 probe 和排队中的非 apply 任务按服务端能力取消。

## 页面、草稿和异步工作流

`draft-controller.js` 拥有草稿、基线、保存确认和代次；`prepareDraft` 对候选请求捕获快照，在取消、编辑器替换、草稿变化时拒绝迟到写回。`app.js` 负责页面与提示协调。

`pageContext` 的只读请求和 render 回调属于当前页面。页面过期后 render 不再刷新新页面；已经提交的写入属于登录会话，可以继续完成。管理员与订阅者保存后的刷新通过 `refreshAfterSave` 检查页面与编辑器归属；`formDialog` 向保存回调提供 dialogSession。分享和账户创建等需要回看结果的流程使用 `workflow-results.js`，迟到结果不能覆盖新窗口。

`task-requests.js` 独立管理任务请求回执，`api.js` 负责 HTTP、会话、错误和响应校验。查询/写入 DTO 由 Go 生成，普通 JS 使用 checkJs，API、回执及其他已列出的边界采用 strict；不宣称整个前端已经严格类型化。

`observeStatus` 合并通知、处理可见性并在离页时释放资源。任务历史使用摘要签名，VPNGate 运行标签每 5 秒读取自主状态，其余可见状态默认每 60 秒补读。资源局部编辑保留列表搜索、排序和位置；需要重新推导页面时才刷新。

## 后台清理与诊断

`application.HistoryRetention` 只依赖 `HistoryRepository`，依次处理权益、任务和证书材料；每类资源拥有独立 30 秒预算，前一项失败不会耗尽后一项上下文。管理器提供具体仓储及证书适配，将结果送入有界 backgroundHealth。失败五分钟后重试，全部成功恢复每小时执行。

故障摘要先在内存记录并失效概览，审计另给两秒预算，不复用已超时的操作上下文。清理恢复时清除对应摘要，审计写入本身失败也可见；错误分类不向面板暴露秘密或原始命令输出。后台清理、调度与任务领取各自拥有生命周期。

## 身份、能力与运行时投影

任务公开契约位于 `application/taskcommand.go`，HTTP 与 CLI 均只接收业务命令。自动调度标记位于管理器私有 `taskInput`，仅调度入口可设置；不接受公开 `automatic`、无意义 `scope` 或与操作无关的参数。前端 `TaskCommand` 联合类型直接由同一字段定义生成。`platform_tasks.go` 适配排队/执行，`platform_apply.go` 适配配置存储/主机服务；`application/deployment.go` 统一发布、激活、确认和恢复顺序，取消请求后恢复仍使用独立且有时限的上下文。

`runtime/reload.go` 使用固定 sing-box 的公共构造器与生命周期 scope，不使用 unsafe、反射修改私有用户表或复制五套协议实现。新监听构造成功后才释放旧监听，启动失败时恢复旧选项，多入站变更失败会逆序恢复已变更入站；恢复失败必须退出。授权代次仍由 accounting 独立校验，恢复旧监听不会恢复已撤销的账户授权。

`serverconfig.Node` 不含 UUID/密码，VPN profile 也不再生成占位用户秘密。绑定、轮换、导出统一使用账户库的规范化凭据：UUID 解析为非零值后转为标准小写格式，清除协议不使用的字段；库的唯一索引与运行时重复身份防御共同阻止跨账户身份覆盖。

生成的受管服务器 JSON 中 `users` 为空。VLESS 的 `_managed_flow` 是内部策略元数据，受管核心绑定用户时读取并删除；官方核心的配置检查使用去除该元数据的投影。不要直接用官方 sing-box 运行受管服务器文件。用户导出的客户端配置不包含此字段。

`internal/runtimeconfig` 定义受管入站、出站、DNS、端点和服务类型，供管理器、前端 `/api/capabilities.runtime` 与核心共享。实际选项及构建能力由 `runtime_validation.go` 调用发行受管二进制确认，预览/保存/应用使用相同规则。校验诊断限制为 4096 个字符，并移除配置中的秘密值。

`internal/application/credentials.go` 只依赖窄仓储接口，管理器负责任务、配置锁和发布；runtime 独立管理各入站生命周期与凭据加载确认。新增协议需要同时补充领域凭据规则、能力登记、核心注册、前端字段与端到端验收。

## 模块边界

账户节点投影位于 `application/account_node.go`：输入为服务端节点、规范凭据及公开证书读取接口，不依赖管理器、HTTP、文件路径操作或 SQLite。管理器只负责读取已应用配置、授权、证书缓存及端点/端口覆盖；完成所有覆盖后统一验证最终节点。节点 ID 使用实例与凭据 ID 的确定性 UUID，不依赖入站名称或秘密，64 字符入站/凭据 ID 和 Argo 派生端点都不会超长。`App.Version` 已移除；节点文档 schema=7，只接受当前格式。

账户名称的 1–128 UTF-8 字节和禁止回车、换行、NUL 的规则属于 domain；`/api/capabilities.account_name` 发布同一约束给表单，保存错误返回 `account.name` 字段定位。`controlstore.Task` 只用于查询和响应；`ClaimedTask` 额外携带执行输入，只从领取入口取得，worker 不接收详情 DTO。批量任务领取返回真实 running/validating 状态。

计量查询中的可空时间与额度使用单次维护周期持有的有界缓冲。连接侧授权缓存只复制周期、截止时间、代次和错误，不保留完整 Usage 或指向可复用缓冲的指针。250 ms 授权窗口、撤销代次、到期时间、WAL/FULL 与 64 KiB 持久预留语义保持一致。

订阅可用性由 subscription/policy.go 的 OutputPolicy/PolicyForOutput 统一推导贡献来源、账户归属与新鲜度；manage/subscriptions_policy.go 仅完成实时账户授权适配。状态投影、下载和投递不得各自维护一套可用性判定。

启动和 CLI 恢复复用 recoverApplied，在发布锁内读取数据库已确认配置并重建同一代次产物。显式 CLI 恢复先取得服务切换锁；worker 启动不取得该锁，因为重启助手会持锁等待 worker 就绪。临时 Argo 观察依据 applied 配置独立更新，desired/applied 差异不能暂停在线观察；服务切换和域名发布仍需序列化。

Overview 的 resource_version 表示当前管理进程的状态失效代次，不是持久化配置版本，也不用于授权。它与 revision、applied_revision、task_counts 共同驱动前端 status-observer.js；12 条任务摘要仅用于展示。观察器合并并发刷新，隐藏标签/页面停止读取，离页清理资源，一般可见页每 60 秒兜底处理自然到期或外部修改；VPNGate 运行标签使用 5 秒周期反映自主重连。运行标签读取实测或已应用状态，草稿仅用于编辑与候选预览。

分享列表也复用该观察器和 remoteTable.refresh，保留搜索、页码与焦点。下载次数、自然到期不保证改变概览代次，因此保留每 60 秒可见页兜底和重新可见刷新；本页撤销直接局部更新，失败由共享表格展示重试入口。

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
| internal/domain                                                  | 稳定身份、权益/周期规则、分页与任务值对象，不依赖 HTTP、SQLite 或应用用例                       |
| internal/application                                             | 类型化命令、账户批量绑定及轮换/解绑用例、发布编排与仓储边界；不解析 HTTP 或操作 systemd         |
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
| internal/manage/platform\_\*                                     | 平台装配、配置图校验、备份、证书和主机适配、HTTP/CLI                                            |
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

凭据撤销和账本变更不参加配置回滚，不能复活已撤销用户。绑定、轮换、解除绑定与 `credential_publications.revision` 在同一事务提交；发布成功更新 `published`，核心每 3 秒读取该实例的发布版本，仅在目标版本已发布且变化时读取完整凭据快照，完成对应入站重载后确认 `loaded`。管理器等待最多 20 秒确认加载；独立维护 worker 每 5 秒核对未完成的文件发布，失败指数退避至 5 分钟。核心连续三次重载失败或监听恢复失败会退出，由现有服务恢复策略接管。重试不重放原始轮换命令，手动停止的实例保持停止。`loaded` 表示最近一次成功启动或重载的版本，是否仍在线必须结合心跳判断。账户独立到期不依赖面板或下载监听。

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

任务 DTO 的 `details` 描述允许访问详情的操作员范围，前后端复用同一规则：管理员可查看，操作员可查看服务/网关任务及自己提交的 apply，viewer 不可查看。服务端始终重新授权。前端 `tasks.js` 负责详情呈现，`sharing.js` 复用权益与授权页的分享流程；按钮忙碌状态与业务禁用状态独立维护，异步结束不会错误启用末页按钮。

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
| `GET /api/grant-options`     | 管理员读取已应用的输出 ID、名称、监听启用状态 `listener_enabled`、分享入口 `public_url`、版本差异 `pending` 及地址问题 `unavailable`。创建授权时服务端重新校验。 |
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

## 新增功能的验收边界

扩展命令时同步 CommandFields、校验、执行器和生成契约；扩展状态时更新服务端状态目录及前端完整映射。只新增一种公开语义，不保留旧别名或旧数据库迁移分支。

更改持久结构必须递增数据库版本并检查备份结构。当前数据库 schema 18 包含任务来源和 task_requests；平台 8、节点 7、订阅 6、备份容器 8 各自独立。所有旧数据库及其备份拒绝导入。

计量批次在任何配额/租约读取和 SAVEPOINT 之前取得一次 SQLite 写锁；单行回滚不释放批次锁。保持跨进程配额一致性、WAL/FULL、64 KiB 单池上限和提交后才交付额度。不得为了吞吐绕开这些约束。

变更验证按受影响边界选择，包含真正的竞态、失败恢复或契约行为；文档区分本机微基准、真实协议、浏览器及 Linux 系统验收，不把前次全量结果当成本次结果。当前交付范围见 [实施报告](refactor-review.md)。
