# RFC-0000：术语表

> 状态：已接受
> 范围：Foundation
> 依赖：无
> 依据：`docs/learn/004-整理索引.md`、`docs/learn/006-Go-Service-Runtime概要设计与约定.md`

## 目的

本文冻结 GSR 的核心术语。后续代码、文档、测试、Codex Prompt 都必须使用这里的命名。

聊天记录中出现过的旧术语只作为设计演进背景，不再进入正式文档。

## 核心术语

| 术语 | 含义 |
|-|-|
| GSR | Game Service Runtime，基于 Skynet 思想设计的 Go 游戏服务器 Runtime。 |
| Core Runtime | 通用运行时内核，只理解 Service、Command、Envelope、Mailbox、Scheduler、Timer、生命周期和 Cluster 数据面。 |
| Runtime Tooling | 工具与工程化层，提供 Discovery、Snapshot、Supervisor、Monitor、Control Plane、ServiceGroup、Drain、Record/Replay 等能力。 |
| Business Layer | 最外层业务层。游戏业务、示例业务、Player/Battle/Room/Wallet 等具体领域概念都放在这里。 |
| Game Layer | Business Layer 的游戏业务部分，提供 Battle、Room、Player、Timeline、Broadcast 等能力。 |
| Business Template | 业务模板。为某类业务提供职责划分、Command 流程和测试样例；不是 Core Runtime 的内建实体，也不是所有业务必须继承的基类。 |
| Service | 可寻址的运行时实体，拥有状态、生命周期、Mailbox 和 Command 入口。 |
| ServiceRef | Service 的运行时地址，不是对象指针。 |
| ServiceID | 单节点内的动态 Service 实例编号。系统 Service 不使用特殊 ID；只有 `ServiceID(0)` 保留给 Core 节点端点和 Runtime caller。 |
| ServiceName | 长生命周期逻辑服务名，例如 `.db`、`.match`、`.config`。 |
| ServiceGroup | 一组承担同一职责的 Service。它是发现和路由层概念，不是 Core Runtime 实体。 |
| ServiceSetVersion | ServiceGroup 完整地址快照的版本身份，由 Directory AuthorityEpoch 和组内 Revision 组成，用于 watch、切换和旧权威 fencing。 |
| RoutingPolicy | 把一个 ServiceSet 映射为目标 ServiceRef 的 Tooling 策略，例如 `Hash`、`RoundRobin`、`Broadcast`；直接投递单个 ServiceRef 不需要策略。 |
| NodeID | Cluster 节点标识。 |
| Command | Service 的能力入口。Command 可通过 Send 或 Call 投递。 |
| CommandID | Command 的稳定数字编号。 |
| RequestID | 一次业务或控制操作的稳定标识，用于跨 Service 重试时的幂等去重。它是业务字段，不替代 Runtime 的 `Session`。 |
| Envelope | Runtime 内部消息包，包含 Source、Target、Session、Command、Payload。 |
| Send | 异步投递 Command，不等待业务结果。 |
| Call | 同步投递 Command，通过 Session 等待 Reply。 |
| Reply | 对 Call 的响应。 |
| Session | Call 和 Reply 的关联编号。 |
| Mailbox | 每个 Service 的消息队列。 |
| Scheduler | 调度 ready Service 的 Runtime 组件。 |
| Execution Permit | Scheduler 的有限执行许可，用于约束同时运行的 Service handler 数量。它不等于固定 goroutine。 |
| Runtime Task | Runtime 创建并追踪的 Service 执行任务，记录 owner、任务类型、开始时间、取消函数和完成句柄。 |
| Timer | 生成未来 Command 的 Runtime 组件。 |
| Cluster Transport | 远程 Service 消息投递通道，不是传统 RPC。 |
| Cluster Data Plane | 集群数据面，负责业务 `Envelope` 的跨节点投递。 |
| Cluster Control Plane | 集群控制面，负责节点管理、健康检查、观测查询和受控运维命令。 |
| Transport Protocol | 传输层编码和链路协议，例如 protobuf、TCP、WebSocket、QUIC。它不参与业务分发。 |
| Login Adapter | 登录连接适配器，负责连接握手、外部认证调用、受控 secret 存储和 ticket 响应写回。它不拥有玩家业务状态。 |
| LoginService | 客户端登录状态 Service，负责重复登录策略、Generation、`SecretRef` 和登录票据编排。它不监听 socket、不调用可能阻塞的认证方，也不持有连接 goroutine。 |
| SessionRegistry | 登录会话注册表，原子保存 `LoginTicket`、受控密钥引用、proof 序号和连接绑定。它属于 Runtime Tooling，不是 Core Service。 |
| LoginTicket | LoginService 签发给客户端进入 Gateway 的短期凭证，包含 uid、subid、server、Generation、过期时间和密钥引用。 |
| SecretRef | 不可预测的登录密钥引用，不是明文密钥。明文 `secret` 不进入普通业务 Command、日志、快照和录制回放。 |
| SessionIdentity | 已认证连接的身份上下文，包含 uid、subid、playerID、server 和登录 Generation；它不复用 Core `SessionID`。 |
| Gateway Adapter | 客户端入口适配器，负责连接、proof 验证、协议解包、限流、连接绑定和转发。它属于 Runtime Tooling 或外层 adapter，不属于 Core Runtime。 |
| Proof Sequence | 同一 LoginTicket 内严格递增的 Gateway proof 序号。Registry 只在成功绑定时原子接受更大的值，用于拒绝重放。 |
| AuthProvider | 业务提供的账号认证适配器，负责验证平台 token、账号密码或渠道登录结果。它属于 Business Layer 或业务 adapter。 |
| ProtocolMapper | 业务协议映射器，把客户端包、HTTP 请求或外部事件转换成 GSR `Command`。它属于 Business Layer。 |
| DiscoveryService | 系统服务，负责节点发现和长期服务名解析。 |
| DirectoryService | ServiceGroup 事实的唯一状态 owner，负责版本化发布、查询和 Watch lease；不决定路由策略。 |
| ClusterObserverService | 系统服务，保存静态节点配置并编排只读节点观测；它不执行收敛或运维动作。 |
| NodeAgentService | 每个节点上的系统服务，维护本节点 Discovery 租约，并响应受限的只读 Monitor 查询。 |
| NodeConfig | 用于定位和观测节点的静态部署事实，例如地址、角色、是否启用；不是可收敛的期望状态。 |
| Desired State | Controller 持有的可收敛目标，例如 Service 副本数、版本和放置约束；它需要独立的认证、授权和审计契约。 |
| Observed State | 控制面读取的当前运行事实，例如节点心跳、延迟、错误和实际 Service 实例。 |
| Snapshot | Service 状态快照，用于恢复、重连或容灾。 |
| Command Record | 记录进入 Service 的 Command 序列，用于复现问题。 |
| Replay | 使用 Command Record 重放 Service 行为。 |
| Supervisor（监督器） | Runtime Tooling 的故障隔离和恢复策略组件。它不接管 Core 生命周期，不等同于 Erlang/OTP 的 Supervisor Tree。 |
| Monitor | Runtime 可观测组件。 |
| Drain | 平滑下线过程。受控操作先切走新流量、使旧实例的 Drain Guard 拒绝新外部工作，再等待已有访问释放，最后关闭旧 Service。 |
| Drain Guard | 包装在目标 Service 外的 Tooling decorator。它在目标 Mailbox 内不可逆地开始 Drain，并拒绝组合根显式列出的外部 Command；它不是 Core 生命周期状态，也不负责切流、等待 Visitor 或 Stop。 |
| Exit Hook | Service 退出时执行的清理逻辑。GSR 第一版通过 `Stop(ctx)` 和 `Close()` 表达，不新增全局 `atexit` API。 |
| Visitor Tracking | 访问者追踪，用于判断旧 Service 是否仍被其他 Service 使用。 |
| VisitorRegistryService | Visitor lease 的唯一状态 owner。它通过 Command 保存、续订、释放和查询关系，不探测 Service 存活，也不编排 Drain。 |
| Visitor Lease | 一个带 AuthorityEpoch、Generation、owner 和过期时间的 Target/Visitor 关系。Visitor 同时是 lease owner；迟到的 Renew 或 Release 不得影响更新后的 lease。 |
| Weak Visitor | 弱访问者。弱访问不阻止旧 Service Drain 退出。 |
| Principal | 已认证 Gateway 断言的操作者身份。修改型 Control Plane Command 同时验证精确 Gateway Service source、Principal 授权与 RequestID；NodeID 不能替代它。 |
| Drain Operation | 以 RequestID 标识、由 DrainCoordinatorService 保存的受控切换记录。它记录 Directory 发布、Guard、Visitor 和 ReadyToStop 事实，但不执行 Stop。 |
| Controller | 对比 Desired State 与 Observed State、计算差异并持续收敛的 Tooling 组件。它决定动作，不直接持有业务 Service。 |
| Reconcile | Controller 根据期望状态与实际状态反复计算并执行收敛动作的循环。 |
| 设计决策索引 | `docs/DECISIONS.md`。用于检索设计结论及其权威 RFC；它不定义公开 API，也不替代 RFC。 |
| Battle | Game Layer 中一组参与者的游戏活动上下文，默认由 `BattleService` 拥有参与者、准备、重连、当前局、动作和计时器等强一致状态。 |
| Timeline | Game Layer 中的游戏时间轴，底层基于 Timer。 |
| Broadcast | Game Layer 的玩家广播封装，底层基于 Send。 |
| PlayerModule | PlayerService 内的业务模块组合单元，属于 Business Layer。 |
| BattleEpoch | Battle 版本号，用于重连、并发校验和旧消息过滤。 |
| TimelineRev | Timeline 版本号，用于同步和重连校验。 |
| 状态 owner | 唯一有权写入一份权威状态的 Service。其他 Service 只能通过 Command 请求其变更。 |

## 禁止术语

| 禁止术语 | 使用 |
|-|-|
| Actor | Service |
| ActorRef | ServiceRef |
| Spawn | CreateService |
| Boot | Start 或 CreateService，按语境选择 |
| Tell | Send |
| Ask | Call |
| Request | Call |
| Attack | Battle 或具体动作命令 |
| RPC Stub | Cluster Transport 或 Proxy |
| GameManager | 具体 Service、Registry 或 Runtime 组件 |
| PTYPE / Protocol Type | 不进入 GSR 公开模型。Skynet 的 `PTYPE_LUA`、`PTYPE_RESPONSE` 等只作为设计背景。 |
| Remote Code Injection | 不进入生产 Runtime。远程运维只能通过白名单 Command。 |
| Agent 登录握手 | Login Adapter + LoginService |

## 术语裁决

### 为什么不用 Actor

`Actor` 容易让实现走向“一个 Actor 一个 goroutine”。GSR 学习的是 Skynet 的运行时思想，最终实体应叫 `Service`。

### 为什么不用 Request

`Request` 容易让人联想到 HTTP/RPC。GSR 的同步投递与 Skynet `call` 更接近，因此统一使用 `Call`。

### 为什么不用 PTYPE

Skynet 需要 `PTYPE_LUA`、`PTYPE_RESPONSE`、`PTYPE_SOCKET` 等协议类型，是因为 Lua Runtime 要依赖协议入口完成分发和封装。

GSR 使用 Go。业务入口应由 `CommandID` 和类型化 handler 表达。传输编码由 Transport 负责，Runtime 内部只流转 `Envelope`。

因此 GSR 不暴露 `PTYPE_*`，也不允许业务按协议类型分发。

### 为什么 Battle 不是 Core Runtime 概念

Battle 是游戏业务封装。Core Runtime 只知道 Service 和 Command。Game Layer 可以提供 `CreateBattle`，但它内部必须调用 `CreateService`。

## Codex 约束

实现时必须遵守：

1. 不新增 `Actor`、`ActorRef`、`Spawn`、`Request` 命名。
2. 不让业务持有另一个 Service 的指针。
3. 不把 Battle 写进 Core Runtime。
4. 不把 Cluster 设计成 gRPC Stub。
5. Timer 只能生成 Command，不能直接修改业务状态。
6. 不新增 Skynet 风格的 `PTYPE_*` 公开概念。
7. `ServiceGroup`、热更新、录制回放都属于扩展层，不进入 Core Runtime 最小接口。
8. 登录握手和 `secret` 交换只能由 Login Adapter 负责；LoginService 只接收认证结果和 `SecretRef`。这些能力不能放进 Gateway、Agent、ProtocolMapper 或 Core Runtime。
9. 一份可变的权威状态只能有一个状态 owner；不得以嵌套锁协调两个 Service 的状态。
10. Service 实现不得直接创建 goroutine；异步业务必须通过 Command、Timer 或独立 Service 表达。
