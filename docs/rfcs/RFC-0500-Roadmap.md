# RFC-0500：开发路线图

> 状态：已接受
> 范围：Roadmap
> 依赖：[RFC-0001](RFC-0001-Foundation-Design-Principles.md)、[RFC-0003](RFC-0003-Foundation-RFC-Lifecycle.md)
> 依据：`docs/learn/007-Game-Service-Runtime详细设计与实现.md`

## 目的

本文定义 GSR 从零实现的顺序。

## 总原则

先证明单节点 Runtime 模型正确，再做 Cluster。

实现顺序必须符合三层结构：

```text
Layer 1: Core Runtime
Layer 2: Runtime Tooling
Layer 3: Business Layer
```

内层先稳定，外层再加能力。外层不得反向污染内层。

不要一开始实现：

- 完整 Cluster。
- Discovery 高可用。
- Snapshot 迁移。
- Monitor Console。
- Runtime Tooling 全量能力。
- 业务层完整框架。

## 当前执行状态

截至 2026-07-23，**Core Runtime** 首版已经完成。它覆盖 `RFC-0100` 至 `RFC-0192`、本路线图的 Phase 1 至 Phase 7A，包括错误模型、性能基线、生命周期与任务观测、本地和双节点端到端示例。

历史讨论中的“第一阶段”默认指 Cluster 之前的 Core Foundation，不等同于本路线图狭义的 Phase 1。Phase 5 Cluster Data Plane 后续独立实施，并已完成。

已实现里程碑的工程收口项统一记录在 [`docs/TODO.md`](../TODO.md)，后续新能力仍按本文顺序实施。

首份性能结果见 [`2026-07-17 Core Runtime 性能基线`](../benchmarks/2026-07-17-core-runtime.md)。Phase 7A、最小 Discovery、本地 Monitor、Snapshot、Supervisor、客户端入口、Phase 8 节点观测、Phase 9 ServiceGroup、Phase 10A Visitor lease Registry、Phase 10B Drain Guard、Phase 10C1 受控 Drain Operation 与 Phase 10C2A 节点 Stop 执行已完成。节点恢复、Controller 与 Reconcile 必须以带操作身份推进。

## 后续 RFC 审核结果

2026-07-22 根据已接受 Core RFC、`v0.3.0` 代码和测试完成首轮审核。结果如下：

| RFC | 阶段 | 状态 | 审核结论 |
|---|---|---|---|
| RFC-0210 Snapshot | 7D | 已接受 | 已实现 Capture Command、外部 Store、Revision 冲突保护、Cluster Codec 和组合根恢复。 |
| RFC-0220 Supervisor | 7E | 已接受 | 已实现 panic Decorator、Source/Generation fencing、有界 Runner、恢复预算、两阶段发布和 Snapshot 纵向切片。 |
| RFC-0290 客户端入口 | 7F | 已接受 | 已实现内存 SessionRegistry、SingleSession LoginService、固定 proof 线格式、TCP Login/Gateway Adapter、ProtocolMapper seam 与端到端验收。 |
| RFC-0250 Control Plane | 8 | 已接受 | 已实现可信集群内 NodeAgent 自动 Heartbeat、静态 NodeConfig、缓存 Observed State、可组合 Codec 与双节点验收；Service Desired State、Reconcile 和修改型运维明确后置。 |
| RFC-0260 ServiceGroup | 9 | 已接受 | 已实现独立 DirectoryService、AuthorityEpoch/Revision、CAS、Watch lease、显式快照 Router、三种策略和双节点验收。 |
| RFC-0270 Drain | 10A | 已接受 | 已实现 VisitorRegistryService 的 lease、代际、owner、过期、Codec、双节点 TCP 和本地示例；Drain 编排、回滚、Controller 与 Reconcile 仍需后续独立契约。 |
| RFC-0271 Drain Guard | 10B | 已接受 | 已实现旧实例入口的精确来源 fencing、Mailbox 串行拒绝、不可逆语义、Codec、本地与双节点 TCP 验收；跨节点业务拒绝继续由目标节点 adapter 映射。 |
| RFC-0272 Controlled Drain Operation | 10C1 | 已接受 | 已实现 Gateway+Principal 授权、RequestID 幂等、有界审计、Directory/Guard 未知结果确认、Visitor 刷新、ReadyToStop、本地和双节点 TCP 验收；Stop、NodeAgent 动作和 Reconcile 仍后置。 |
| RFC-0273 Node Stop Execution | 10C2A | 已接受 | 已实现 Gateway+Principal 授权的 StopOperation、精确 Coordinator NodeAgent receipt、Directory 双重再确认、组合根有界 Runner、本地与双节点 TCP 验收；恢复、补偿和 Reconcile 仍后置。 |
| RFC-0274 Manual Recovery | 10C2B | 已接受 | 已实现 Blueprint Runner 创建替代实例、NodeAgent receipt、人工 Confirm、Directory CAS、未知结果 Resolve、本地与双节点 TCP 验收；不恢复旧 Ref 或自动 Reconcile。 |
| RFC-0280 Record/Replay | 11 | 已接受 | 已实现 Handle decorator、有界 Recorder、版本化 Bundle、目录型 JSONArchive、可组合 Codec 与隔离 Replay；Battle 的确定性业务组合留待 Phase 13。 |
| RFC-0300 至 RFC-0370 | 12 | 已接受 | 已实现 game 包的领域边界、Battle/Timeline/Room/Player/Module/Wallet API、RequestID 与异步 LedgerRunner/MemoryLedgerStore；2026-07-24 冻结直接 Send/Call/Reply 与 Context 有效期语义。具体游戏规则和生产 Store 外置。 |
| RFC-0400 示例 | 13 | 已接受 | 已实现 WhackMole 的 Timeline、单次 Kick、结算入口、可执行组合根与隔离 Record/Replay 验收；2026-07-24 补充 Send 启动、Call 命中结果与按 Battle 性能基准。生产房间工厂与持久账本外置。 |

截至 2026-07-24，后续阶段的开放 API、owner、失败收敛与验收已完成文档冻结，均为“待实现”。它们可以进入失败测试和最小实现；任何兼容性外的变更仍必须先修订 RFC。

## Phase 0：文档和术语冻结

输出：

- RFC-0000 术语表。
- RFC-0001 设计原则。
- API 规则。
- 命名规则。
- 冲突裁决记录。

## Phase 1：单节点 Service Runtime

实现：

- `ServiceRef`
- `Service`
- `ServiceSpec`
- `CreateService`
- `LocalRegistry`
- `Mailbox`
- `Send`

验收：

```text
Service A Send Command to Service B
Service B Handle Command
```

## Phase 2：Command 与 Call/Reply

实现：

- `CommandID`
- `Service.Handle` 单一 Command 分发入口；Runtime 不维护 per-Service Command 白名单
- `SessionID`
- `PendingCall`
- `Call`
- `CommandContext.Reply`

验收：

- Call 返回结果。
- Call 超时。
- Reply 两次失败。

## Phase 3：Scheduler

实现：

- ReadyQueue。
- 固定执行许可池。
- Batch。
- 慢 Command 指标。

## Phase 4：Timer

实现：

- `After`
- `Cancel`
- Timer 生成 Command。

## Phase 5：Cluster Data Plane

实现：

- `NodeID`
- `ClusterTransport`
- Handshake。
- WireEnvelope。
- Remote Send。
- Remote Call/Reply。

验收：

```text
Local Send/Call 与 Remote Send/Call 行为一致
```

## Phase 6：Core Runtime 验证

实现：

- 核心错误模型。
- 核心性能基准。
- 生命周期可观测。
- 最小端到端例子。

不实现具体游戏业务。

## Phase 7：Runtime Tooling 基础

Phase 7 拆成六个可独立验收的子阶段，避免一次把所有外层能力压入 Core。

### Phase 7A：Runtime Inspection 与 Core 首版

状态：已完成（2026-07-17）。

实现：

- `Runtime.Inspect` 只读观测边界。
- Service、Mailbox、PendingCall、Timer 和 Runtime Task 视图。
- Core 与 Cluster 文档同步。
- API 冻结 Review。
- `v0.1.0` 标签。

### Phase 7B：最小 Discovery

状态：已完成并通过审查修正（2026-07-22）。

实现 Node Discovery、长期 ServiceName Discovery、AuthorityEpoch 与租约 owner 约束，并通过 Core 节点级名字查询完成稳定 bootstrap。ServiceGroup、Gossip、负载均衡和管理命令不进入本阶段。

### Phase 7C：本地 Monitor

状态：已完成（2026-07-22）。

实现本地 Monitor Report、JSON 输出、Metrics 枚举副本和远程 Call 结果指标。远程 NodeAgent、HTTP/CLI、Prometheus exporter 与管理面查询留到 Phase 8 或独立 adapter。

### Phase 7D：Snapshot

状态：已完成（2026-07-22）。

实现 Capture Command、版本化 State、稳定业务 Key、存储适配器、修订号冲突检查、可组合 Codec 和组合根受限恢复。业务持久化不下沉到 Core，不修改运行中实例。

### Phase 7E：Supervisor

已完成不可变失败通知、Source/Generation fencing、三种策略、尝试与窗口预算、有界 Runner、两阶段 Launcher 和 Snapshot 恢复纵向切片。Supervisor 只创建新实例，不复活旧 `ServiceRef`，不在 panic 后临时抓取状态；Publish 与 committed 之间再次失败也会消耗并隔离已经对外运行的 Generation。

### Phase 7F：客户端入口

状态：已完成（2026-07-22）。

已实现内存 `SessionRegistry`、`Login Adapter`、`LoginService`、最小 TCP `Gateway Adapter` 和 `ProtocolMapper` seam。会话 Generation、proof 线格式、原子绑定、SingleSession 的旧连接关闭，以及 Adapter 失败交接由 RFC-0290 冻结。生产 Handshake、TLS、跨节点或持久化会话不在本阶段。

## Phase 8：节点观测与 Heartbeat

状态：已完成（2026-07-23）。

实现：

- `ClusterObserverService`。
- `NodeAgentService`。
- 自动 Discovery Heartbeat、节点配置 / 缓存 Observed State。
- 节点状态列表、节点详情与单节点刷新。
- 只读 Agent 的 Cluster Codec 与双节点示例。

不实现：

- 远程任意代码执行。
- 外部 Admin API、认证、授权、审计和生产环境危险运维命令。
- Service Desired State、Reconcile、扩缩容、故障迁移、reload、Drain、ServiceGroup 切换和动态 peer 更新。

Phase 10 才能在独立 RFC 中冻结 Service Desired State、Controller、Reconcile 与修改型执行命令；不得把 NodeID 来源检查当作用户身份认证。

## Phase 9：Runtime Tooling 扩展

状态：已完成（2026-07-23）。

实现：

- 独立 `DirectoryService` 和 `ServiceGroup` 事实目录。
- `AuthorityEpoch + Revision` 组成的 `ServiceSetVersion`。
- compare-and-set 完整发布、Get 和带代际的 `WatchServiceGroup` lease。
- `RoutingPolicy`、`Hash`、`RoundRobin`、`Broadcast`。
- 对调用方持有的 ServiceSet 执行 Send/Call 的 Router。
- 可组合 Codec 和双节点 TCP 验收。

约束：

- Tooling 能力不得下沉为 Core 领域概念；只有多个上层共同需要的通用 Runtime 能力，才可先修改 RFC 后进入 Core。
- Discovery 不保存 ServiceSet，也不决定路由策略。
- Router 不按组名隐式查询 Directory，不在 Client 内启动 goroutine 或维护 channel。
- `Direct` 不是 ServiceGroup policy；单个 ServiceRef 继续直接使用 Runtime Send/Call。
- 本阶段不引入 Desired State、Controller、Reconcile、自动注册进组、健康检查或 ServiceGroup 切换编排。

## Phase 10A：Visitor lease Registry

状态：已完成（2026-07-23）。

实现：

- `VisitorRegistryService`。
- `VisitorLease`、显式 `Weak Visitor`、owner / generation / expiry fencing。
- Timer Command 驱动的过期清理、Client 和可组合 Codec。

不实现：

- Drain guard、旧实例 `Stop` 编排、ServiceGroup 切换或回滚。
- Desired State、Controller、Reconcile 与修改型 NodeAgent 动作。

## Phase 10B：Drain Guard

状态：已完成（2026-07-23）。

实现：

- `tooling/drain` 的 Service decorator。
- 受信任协调 Service 发起的 Begin、查询状态和显式外部 Command 拒绝。
- 单个旧实例 Mailbox 内的不可逆切换、可组合 Codec、本地与双节点 TCP 验收。

不实现：

- ServiceGroup 发布、切换、回滚、Visitor 等待、超时轮询或 `Runtime.Stop`。
- Desired State、Controller、Reconcile、NodeAgent 修改型动作与外部 Admin API。

## Phase 10C1：受控 Drain 操作

状态：已完成（2026-07-23）。

实现：

- Coordinator 保存经 Gateway、Principal 和 RequestID 约束的 Drain Operation 与有界审计。
- Directory CAS、未知 Publish 的只读确认、Guard 幂等确认、Visitor 刷新和 `ReadyToStop`。
- 本地与双节点 TCP 验收；节点级 caller 不能绕过 Gateway 调用 Coordinator。

不实现：

- Runtime Stop、NodeAgent 修改型动作、自动恢复或后台 Reconcile。
- Desired State、扩缩容、放置、外部认证协议或持久化审计。

## Phase 10C2A：节点 Stop 执行

状态：已完成（2026-07-23）。

在 Operation 已经能够审计地给出 ReadyToStop 后，Gateway 以 Principal 发起独立 StopOperation；Coordinator 在创建和每次投递前强确认 Directory，NodeAgent 以精确 Coordinator ServiceRef 保存本地 receipt，组合根的有界 Runner 才能再次确认 Directory 后调用 Runtime.Stop。Gateway 必须显式 Resolve 获取结果，本地和双节点 TCP 都已验收。它不得重新发布已经 Guard 的原旧 Ref，也不得把 NodeID source fencing 充当用户认证。

不实现：

- 创建替代实例、自动恢复、补偿、Desired State 或 Reconcile。
- 任意代码热替换或 Go 进程内危险热补丁。

## 后续 Phase 10C2B：人工恢复与补偿

状态：已完成（2026-07-24）。

已实现 [RFC-0274](RFC-0274-Tooling-Manual-Recovery-Compensation.md)：Gateway + Principal 创建审计化 RecoveryOperation，组合根 Blueprint Runner 创建替代实例，操作者显式 Confirm 后以 Directory CAS 在保留当前成员的基础上追加新 Ref 并发布更高 ServiceSet。本地与双节点 TCP 均已验收；它不得 Resume Guard、重新发布旧 Ref、自动补偿或引入 Desired State/Reconcile。

## Phase 11：Command Record 与 Replay

状态：已完成（2026-07-24）。

实现：

- `tooling/record` 的 Handle decorator、有界 RecorderService、typed Client、版本化 JSON Bundle、目录型 JSONArchive 与可组合 Cluster Codec。
- TargetFactory 创建隔离 Runtime 后的逐条 Decode/Send Replay；旧 Runtime 不会被 Replay 直接寻址。
- 通用 Timer Command 的录制与重放验收；Battle 的随机 seed、Clock、Timeline 与结算结果由后续业务示例提供 Command payload。

约束：

- Record/Replay 只作为 Debug 和测试能力，不是线上流量镜像、持久事件日志或故障恢复。
- JSONArchive 只写调用方提供的目录；保留期、加密、上传和生产对象存储仍由应用 adapter 负责。

## Phase 12：Business Layer 最小封装

状态：已完成（2026-07-24）。

实现：

- `game.CreateBattle`
- `BattleContext`
- `Timeline`
- `Broadcast`
- `PlayerService`
- `PlayerModule`

## Phase 13：业务示例

状态：已完成（2026-07-24）。

实现端到端：

```text
Room -> Battle -> Timeline -> Kick -> Settlement -> Stop
```

可增加玩家类示例：

```text
LoginService -> Gateway Adapter -> ProtocolMapper -> PlayerService -> PlayerModule -> Repository
```

约束：

- 具体业务只放在 Business Layer。
- 不让 Core Runtime 引用 Player/Battle/Room/Wallet。

## Phase 14：棋牌游戏生产脚手架与完整示例

状态：NHSK GameLogic 契约已冻结、待实现（2026-08-01）。迁移顺序为先原位替换 GameLogic，再替换 GameMaster，最后替换 Agent。

本阶段的 NHSK 公开 API、Legacy 范围、状态机、失败语义和验收以
[`RFC-0410`](RFC-0410-Example-NHSK-GameLogic.md) 为唯一权威。以下条目是需求发现过程和
实施背景，只用于追溯；与 RFC-0410 或 `docs/DECISIONS.md` 冲突的早期描述已经失效，
不得据此新增 Command、codec 分支或兼容能力。

目标：

- 第一阶段只替换旧 GameLogic。新 Game 进程启动时主动连接旧 GameMaster，在同一条全双工 TCP 上接收入站 `0x8605`、发送出站 `0x8644`，同时提供可由 Cluster 直接调用的类型化 Command。
- Legacy TCP adapter 只保留 RFC-0410 的 MessageID allowlist。入站解码 `BS_MSG_GM2GL_GAME_MSG` 及其中已确认的 `BS_MSG_GC2GS_RELAY`；旧 `BS_MSG_GAME_BASE_RELAY` 实际落入未知消息，出站 `BS_MSG_GL2GM_GAME_MSG_OLD` 也没有 NHSK 调用点，二者均不实现。所有 Legacy envelope 只存在于 transport/codec 边界，校验和解码后与 Cluster Send/Call 进入同一套类型化 Battle Command。
- Legacy TCP 与 Cluster Send/Call 对 Battle 没有入口优先级。两者完成 adapter 校验后进入同一个 Mailbox，按到达顺序串行执行相同玩法规则；是否仍然有效只由当前业务状态、TurnRevision、VerifyCode 和其他领域 fencing 决定。第一阶段 Cluster 调用者视为受信任内部服务，但玩家动作 Command 必须携带 `UserID`，Battle 仍校验该用户属于本局。
- Cluster Call Reply 只返回该 Command 的类型化成功结果或稳定拒绝原因，不携带通用 BattleRevision。玩法产生的客户端定向回复与广播仍作为 GameOutput 批次，经该 ConnectionGeneration 的 GameOutputService 发往旧 GM；同一批客户端输出不得复制到 Call Reply。GM 链路断开后，尚未隔离的 Battle 进入生命周期收敛，新的 Cluster Send/Call 返回 stopping 或 not found；`Quarantined` 返回隔离错误。Cluster 入口不能绕过断线收敛、把旧牌局绑定到新连接代际或恢复旧局。
- Cluster `Send` 成功只证明 Command 已进入目标 Mailbox，不表示玩法成功；调用者需要当前业务结果时使用 `Call`。普通 NHSK 出牌、托管、重连、亮牌等玩法 Command 不携带通用 RequestID，不在 Battle 内维护幂等键、payload 摘要或结果缓存；它们只依靠 Mailbox 串行、玩法状态、TurnRevision、VerifyCode 和具体 Command 规则。Call 超时只表示 Reply 未及时到达，不取消已经入箱的 Command，也不授权调用方自动重发该动作；调用方先通过权威 Snapshot 判断结果，再决定后续操作。Runtime Call Session 只关联本次 Reply。
- Legacy wire 同样不生成通用 RequestID，重连后通过权威 Snapshot 恢复客户端视图，不重放旧输入或旧输出。只有结算、钱包、受控创建或释放等存在真实跨 Service、持久化或人工重试风险的具体流程，才在各自契约中使用 RequestID、OperationID 或 receipt。Runtime/Transport 的未送达、Call 超时和连接断开与稳定业务结果分层表达；业务结果包括用户不属于本局、阶段错误、过期 Revision、非法动作、Battle stopping 和 quarantined，不能用玩法错误包装底层传输失败。
- Legacy `BS_MSG_GAME_USER_RECONNECT` 映射为 `ReconnectPlayer` 写 Command：校验用户座位后清除 Offline；只有当前小局正在进行时，才退出托管/机器人状态并调用共享的 `RestorePlayerView`。`BS_MSG_GAME_SCENE` 映射为独立的 `RequestGameScene` 写 Command：先校验大局号、小局号和用户座位，不修改 Offline，然后退出托管/机器人并调用同一恢复模块。`RestorePlayerView` 按参考顺序和可见性设置 `ClientReady`，补发 GameInfo、GameScene、当前 AskOutCard，以及亮牌阶段可能再次面向全桌的 ShowCard；目标部署关闭的解说时间与没有目标规则证据的战绩不实现。不存在玩家、前置局状态不满足或 Battle 已停止时，Legacy adapter 保持原有静默忽略，不新增错误包。
- Cluster 观测使用独立的只读 `GetBattleSnapshot` Call。它与恢复模块共用 Snapshot 构造代码，但不修改 Offline、RobotState、ClientReady 或任何玩法状态，也不产生 GameOutput。`Quarantined` Battle 不执行 Legacy 重连/场景副作用，不发送伪造场景、GameOver 或 GameResult；Legacy 请求静默拒绝并记录指标/告警，Cluster 查询返回 `ErrBattleQuarantined`。实现必须分别用 `USER_RECONNECT`、`GAME_SCENE` 参考 golden 和完整场景证明两条路径的前置条件、消息顺序、目标、隐藏信息与状态变化没有偏差。
- “完成最小可玩牌局”与“达到旧 GameLogic 切换条件”是两个里程碑。前者先覆盖标准四人完整流程并验证 Host、Battle、Timeline、输出和 Legacy bridge；后者只要求旧系统真实运行过、目标部署实际启用过、录包出现过或用户明确要求保留的能力进入兼容矩阵，并完成对应实现或明确的有意偏差验收。在达到后者前不得把示例描述为可无损替换。
- AI、回放、约局、投票、道具、偏洗牌、自定义牌堆及各种结算路径必须逐项查证。测试、配置字段、协议常量、接口或完整实现只能证明代码存在，不能单独证明生产使用。确认原系统未使用的能力直接标记“放弃”，首版不写占位实现、adapter、provider 或预留接口；以后出现真实需求时再新增 RFC、golden、失败测试和纵向功能切片。明显有问题但已经对外可观察的旧行为默认兼容，不能在实现中顺手修复；修正必须先记录为有意偏差并取得确认。
- 当前 NHSK 生产参考配置提供了第一批使用事实：`robot_level=2` 并配置外部 RobotTran 地址，`custom_deck.enabled=1` 且有账号白名单，因此外部 AI 和白名单自定义牌堆进入切换范围；三份配置的 `replay_enabled=1`，且旧 BaseGame 对该字段的创建/保存判断实际都被注释，回放生成无条件可达，因此回放同样必须进入范围但该假开关本身删除。`yueju_mode=0`、`match_live_mode=0`，且 watcher 的实际出牌调用被注释，因此约局和旁观放弃。仓库没有证明回放上传、偏置洗牌、惩罚、战绩、投票、GL 骰子定座和 Robot relay 被目标部署使用的 BaseRule、GameRule、动态规则或录包，这些能力同样放弃，不作为切换缺口。
- `INIT` 中的旧 GameRule 只解析当前玩法真实可达的前四项，其中偏置洗牌已按上一条放弃；首版将最小机器人出牌次数、最小机器人出牌比例和单牌换牌数量归一化为 Battle 创建后不可变的 `NHSKConfig`。缺少字段、空字段或解析失败时沿用对应默认值，不使 Battle 创建失败；多余字段忽略。原始规则只以脱敏摘要进入诊断，不在玩法处理中反复解析，也不为已放弃的规则项创建空字段或占位 handler。
- Legacy INIT 已有独立字段的 Fee、MaxGameNum、MaxSubgameNum、ScoreBase/Denominator 和 Match/Game/Round 身份直接成为权威，不再从 BaseRule 重复读取。BaseRule 只向 NHSK 玩法配置提供当前需要且没有独立字段替代的三项：index 1 `OfflineAutoUsesAI`、index 6 `TimeoutAutoMove` 和 index 22 `RobotLevel`。字段缺失或无效时分别沿用 false、true 和进程配置默认值；目标生产进程的 RobotLevel 默认 2。其余 BaseRule 项属于旧 GM 职责或已放弃的战绩、上传、充值、约局、投票、惩罚、流程检测、随机换座等能力，不进入新 GameLogic 玩法配置；仅按后文保留最小的旧回放属性投影。
- BaseRule index 10 的 GL 结算展示延迟不迁移。目标生产配置没有启用证据，旧 GM 已拥有结算展示等待；客户端 GameResult 后，Battle 只等待已确认的回放收敛，随后立即通知 GM，不再叠加第二段 GL 延迟。原始 BaseRule 只保存脱敏摘要供诊断，不进入回放或可变业务状态。
- 每个 Battle 创建时获得独立伪随机源和注入的 `Clock`。生产随机种子只从 `crypto/rand` 生成；随机源失败使本次 Battle 创建明确失败，不使用当前时间、进程号或全局 `math/rand` 降级。测试显式注入固定 seed 和 fake Clock；洗牌、庄家、新手路径及其他玩法随机选择都只能使用本 Battle 的随机源。诊断保存 seed 和有序 Command 事实用于重现，但不改变旧回放 XML。
- 保留 `NEW_GAME.IsNewbie`，但不把它作为恢复 Nacos 偏牌体系的理由。可用自定义牌堆优先并绕过新手调整；否则先由 Battle PRNG 产生普通牌序，再对座位顺序中的第一个非机器人执行与参考 `RandCardListByNewPlayer` 等价的调整，四家都是机器人时不调整。参考 helper 会全局交换四家牌而非只修改目标手牌，首版以固定 seed golden 锁定四家最终牌序，不在迁移中修正。Nacos、每座动态偏牌参数和通用 `BiasedShuffling` 继续放弃；诊断只保存 `IsNewbie`、seed 与最终发牌事实。
- 普通随机发牌继续应用 NHSK 的散牌调整，而不是把“普通洗牌”解释为不带玩法后处理。`GameRule` 第四项在 adapter 边界归一化为不可变 `SingleCountToSwap`：默认 `4`，缺失、空值或非法值沿用默认，`<=0` 关闭。实现保持参考 `SwapSingleCard` 的座位顺序和换牌结果；后续座位可能重新影响先前座位，因此不把“四家最终均不超过阈值”写成错误不变量。固定 seed golden 校验四家完整牌序、104 张总牌多重集合和每座 26 张。自定义牌堆和新手路径按原优先级绕过该普通路径；诊断只记阈值。参考虽把 `SingleCountSwap` 传给 `RecordGameStart`，该入口为空且旧 XML 实际不输出，因此新回放不得新增该属性。该算法属于 NHSK，不进入 Core Runtime 或通用玩法模板。
- 参考 `test_mode_enabled` 在 pro、test 和默认配置中均关闭，也没有 Legacy 或 Cluster 输入；其 `applyTestModeHands` 每座只写 6 张固定牌，不能形成合法 NHSK 牌局。首版删除 TestMode 配置、状态和实现。固定随机行为使用 Battle seed，完整指定牌局通过测试构造注入 `CustomDeckProvider` catalog；测试依赖不进入生产优先级，也不新增线上开关。
- `NHSKConfig` 不是旧 Config struct 的搬运目标。`MsDeal`、`MsContinueDelay`、`TableMultiplier` 只有加载点而没有玩法读取点，直接删除；`MsShowCard`、`MsCommentate`、`TestMode` 已按目标生产路径放弃，`RecordUserAction` 对已确认始终启用的 CARD_ACTION 不再形成开关。RobotTran endpoint 只属于 `AIProvider`，回放根目录只属于 `ReplayWriter` 组合配置，自定义牌堆 enable、数据源和账号白名单只属于 `CustomDeckProvider/Runner`；Battle 只接收对应依赖和当前小局的类型化结果，不读取 URL、目录、Redis key 或外围开关。玩法配置只保留四类行动期限、RobotLevel、离线/超时托管策略、托管结算阈值、`SingleCountToSwap` 及 INIT 中真正权威的身份、局数和计分参数，创建后不可变。
- INIT 的 Legacy codec 仍完整解析固定区和 BaseRule、GameRule、MatchName、RoundUniCode 四个 suffix，但归一化后不保留没有消费者的字段。`BattleIdentity` 是 BattleID、ProductID、MatchID、RoundID、RoundUniCode 的唯一来源；GameID 按玩法类型在 Host 创建边界处理，不进入每桌身份。INIT 只核对已建立的 BattleID、ProductID、MatchID。MaxGameNum/MaxSubgameNum 是不可变进度上限，当前 GameNum/SubGameNum 只由 UPDATE_GAME 更新。Fee 供客户端 GameInfo，ScoreBase/ScoreDenominator 供回放最终分换算。`ReplayMetadata` 只增加 MatchName、GameType、ScoreType、ScoreMode、RoomID、CreatorID，ReplayBuilder 直接复用 Identity、进度上限和计分配置，不维护第二份对应值。MatchKeyInfo 的 TourneyID、MatchTime、PhaseID、BoutID、StageID、TableID 以及 INIT CreateTime 只做 wire 解码后丢弃，不建立领域字段或额外校验。INIT 成功后玩法 Handler 不再重复比较整份初始化身份；后续 Legacy envelope 中真实重复的 ProductID/MatchID 只由 adapter 核对。
- `StartSubgame` 成功时只从 Battle Clock 捕获一次 `SubgameStartedAt`，之后不再为同一小局的回放字段读取当前时间。StartTimestampSec 取该时刻 Unix 秒；UniCode 严格保持参考的无分隔符十进制拼接 `UnixSeconds + CreatorID`。ReplayName 的 `YYYYMMDD/HHMMSS` 和 `FuPan/YYYYMMDD/HH` 使用同一时刻的 UTC+8 表示，避免参考连续多次 `time.Now` 在秒、小时或日期边界产生不一致。RoundUniCode 原样取 INIT。CreatorID=0、同秒潜在碰撞或两个 Unicode 的关系不新增校验，因为它们只服务回放兼容，不替代完整 ServiceRef 或 BattleID。
- NEW_GAME 的 `IsNewNacos` 只属于 Legacy wire。codec 和 golden 保留该位，但归一化创建请求不携带它：参考 GL handler 本就没有把该值写入 RoundInitConfig，Round 的 isUseNacos 始终为 false，BindNacosFile 为空。Host、BattleIdentity、NHSKConfig、Cluster API 和诊断不得出现该字段；动态 Nacos 能力继续按已确认范围放弃。
- START_NEW_GAME 的三个字段不并入 INIT 或 NHSKConfig，而形成独立 pending `RoundContext{SecRoundTotal, SecRoundUsed, RoomInfo}`。Legacy 与 Cluster 均以 `UpdateRoundContext` Send 覆盖最新值；相同内容 no-op，不改变阶段、不启动小局、不产生客户端输出、Reply 或 gameplay Revision。`StartSubgame` 将当时值冻结进当前 ReplayDocument，之后到达的更新只影响下一小局。未收到时沿用旧零值 `0/0/""`；Battle 不用 Clock 推算 SecRoundUsed。ReplayBuilder 的 MatchInfo 与 GameRule 节点读取同一冻结 context，避免回放内部再维护两套对应状态。
- GameID 表示玩法类型。NHSK 组合根持有单一 `GameDescriptor{GameID:82, GameName:"宁海双扣"}`；Legacy NEW_GAME 在创建前必须匹配 82，其他值或 0 回复 Res=0。`.nhsk-game-host` 本身已代表玩法，因此 Cluster 创建不携带 GameID，BattleIdentity 也不保存。ReplayBuilder 根节点的 ID/GameName 和 Legacy AIProvider JSON 的 game_id 都引用该 descriptor。ProductID 仍是每 Battle 的比赛产品身份。宁海麻将等其他 GameID 以后由协调者路由到另一个 Host/Factory；第一阶段不增加多玩法 GameCatalog。
- BaseRule 中不再驱动 NHSK 业务的值如果属于旧回放固定 schema，只在 INIT 边界归一化为不可变 `ReplayRuleSnapshot`：index 49 `TimeOutOver`、index 38 `VoiceMode`、index 15 `RandomSeatRoundStart`、index 11 `GameNumToRandomSeat`，缺失或非法时沿用 0。ReplayBuilder 另复用 `NHSKConfig.TimeoutAutoMove`、固定 PlayerNum=4、MaxGameNum 和当前小局冻结的 RoundContext；`SecRoundTotal=0` 时写 `GameNum=MaxGameNum`，非零时只写 `GameTime=SecRoundTotal`，并始终输出其余旧属性。Snapshot 不进入玩法状态或配置，也不恢复超时解散、语音、随机换座。NHSK 传入空 `RecordGameStart` 的 `BiasedShuffling`、`SingleCountSwap` 没有旧 XML 输出，首版不补造。
- ReplayBuilder 以 SeatID 0..3 依次生成 `Player0..Player3` 和 `Dress/D1..D4`，不复制参考 Go map 遍历造成的随机节点顺序；每项仍保留对应 UserID、座位、初始分、平台/CltID 与不透明 Dress 数据。昵称若已是合法 UTF-8 则原样使用，否则按旧行为从 GBK 转为 UTF-8。Go 标准库不提供 GBK decoder，首版允许仅在回放文本编码 helper 中依赖 `golang.org/x/text`；该依赖不得扩散到 Battle、Runtime 或 Legacy wire codec，也不通过直接依赖旧 `nbgame_core` 获得。
- 综合结算结果成功应用，或 MATCH_STOP 跳过 0x8650 后提交本地结果时，Battle 从注入 Clock 读取一次并冻结 `SubgameEndedAt`。ReplayBuilder 用其 Unix 秒写根级 GameOver.EndTime，不能直接调用 `time.Now`，也不能错误复用 SubgameStartedAt。当前保留的普通和 MATCH_STOP 路径都按成功原因生成：Scale=`Game`、Reason=`Success`、EndReason=0、OverCode=0、OverUserID=0、OverStatus=0、RecordValid=1，并写数值 GameResult 与 `DanKou_1`、`ShuangKou_2` 或 `PingJu`。Chair0..3 严格按座位写 UserID、由 Multiple×ScoreBase/ScoreDenominator 换算的 Score、综合结算返回的 TotalScore、Result、IsSeal、IsBreak、IsAuto、IsWin、CatchScore 和 Multiple。约局/投票/离线等已放弃结束原因不为填字段恢复。
- 回放的 Moves 只记录真实 Deal/动作/分牌等事实，不生成 `GameOver` Move；当前 `RecordGameOver` 与测试明确保持根节点写入，runner 中同时带 `GameStart` 和 `GameOver` Move 的旧 fixture 只用于旧输入兼容，不作为新 writer 的 golden。Summary 依次写四座和一个总计：每座保留 OutCount、AutoOutCount、从每次 Ask 到合法出牌/过牌的毫秒累计、当前小局 TotalGameScore、固定 BuChu/DanZhang/DuiZi/SanZhang/FuLu/ZhaDan 动作项、获胜方 DanKou/ShuangKou PaiXing，以及本局 SumScore/WinCount/Double RoundStat；总计汇总前三项。Other/CardDetail 按座位和实际出牌顺序记录所有炸弹的 Type/Cards/Count。所有耗时来自 Battle Clock；已放弃的 BaseRule 战绩能力不恢复跨小局累计，RoundStat 使用当前小局结果。
- Moves 的生产入口进一步收窄为真实调用：一个 Deal Move 下按 SeatID 0..3 写 D0..D3（ChairID、UserID、Cards），四个子节点在 Moves.Count 中只计一次。有分牌的合法动作先写 CurrentPoint（当前累计分牌 Cards/Point、Actor=系统），再写 OutCard；过牌只写 Cards 为空、CardType=不出的 OutCard。一墩结束在最后 OutCard 后，仅当分值大于 0 时写 CatchPoint（获分 ChairID、Cards、Point、Actor=系统），再写 TurnEnd（逗号分隔的四座 Scores、Actor=系统）。OutCard 另写 ChairID、从 Ask 到该合法动作的 MSec 和中文 CardType；人工、外部 AI、普通或 AI 超时、托管的 Actor 分别是玩家、AI、超时、托管。牌型名称精确保留不出、单张、对子、三张、俘虏和按张数命名的炸弹4..8。
- 明确无业务调用的 PickCard、Offline Move、Reconnect Move 和可绕过约束的任意 AddMove 不进入目标生产 API；道具 Move 只能由已确认的 RecordPropUse 类型化事实生成。最终 XML 使用标准 Header、Tab 缩进、按属性名字典序编码、小写 `0x%02x` 牌值且不追加额外尾部内容；Moves.Count 取实际 Move 节点数。reader 可以继续兼容旧 TurnEnd 的 `Score_0..3` 等输入形状，但 writer 只输出当前逗号 Scores 契约。
- ReplayBuilder 的完整树序固定为根级 Info、Moves、GameOver、Summary、Dress、Other；Info 内依次为 MatchInfo、GameInfo、Players，GameInfo 子节点依次为 GameRule、GameSetting。Dress 在每个完整四人牌局都按座位生成 D1..D4，即使不透明值为空；Other/CardDetail 同样生成 P0..P3，未出炸弹时 DetailCount=0。没有调用方的 PlayersPre helper 不迁移。固定树形是 writer 契约，不能由结构体字段顺序、map 迭代或“空节点优化”决定。
- 客户端 GameResult 成功提交且 GameOver/Summary/CardDetail 全部加入树后，Battle 在同一个 Mailbox handler 中补齐 Moves.Count 和 Summary.Count，并以纯内存编码冻结不可变 `ReplayDocument{Name, RelativePath, XMLBytes}`；所有字符串和字节必须深拷贝，不能向 runner 暴露 XMLNode、builder 或 Battle 指针。文档序列化是有界 CPU 工作，不另起 goroutine；只有 ReplayWriterRunner 执行目录创建、文件创建/写入/关闭等可能阻塞的 I/O。序列化失败不提交 writer，直接作为 ReplayFinalizeFailure 进入与写盘失败、超时相同的收敛：不隔离、不撤销客户端结果，仍使用既定 ReplayName 继续通知 GM。
- 回放文本不能由一个全局“统一转码”策略处理。MatchName/ServiceName 和玩家 UserName 使用同一隔离 helper：已经是合法 UTF-8 时不变，否则尝试 GBK→UTF-8；固定 GameName 直接使用 GameDescriptor。RoundContext.RoomInfo 为空时不创建子节点，非空时只在 MatchInfo 下生成 `RoomInfo Json=<原值>`，不解析 JSON。RoomInfo、Dress 和 PropID 只沿用 Legacy suffix decoder 裁掉尾部 NUL 后的不透明字符串，不做 GBK 转换；标准 XML encoder 负责转义元字符，并按 Go 行为将非法 UTF-8/XML 字符编码为替换字符。任何原始不透明文本不得进入日志。
- Prop Move 仅由 RecordPropUse 事实按 Mailbox 顺序追加，属性固定为 Act=Prop、Count、PropID、TargetID、dwSenderID；TargetID 将输入数组按原顺序用逗号连接，不排序、不去重，空数组写空字符串，且不添加 Actor。该节点不读取或改变玩法状态，编码替换也不成为 Battle 拒绝或隔离原因。
- 每次 StartSubgame 在输出 GAME_START/GAME_STARTED 和 NHSK 消息前，按已提交 Mailbox 状态冻结四座不可变 `ReplayPlayerSnapshot`。字段为 SeatID、UserID、NickName、当时 Score 形成的 InitScore、CltID 形成的 Platform，以及最新 Dress；CntID 不保存。当前 GM 通常不填 CltID 而得到 0，但 Cluster 或未来调用方提供的真实值必须保留，不能在 builder 中硬编码。START 后的 UPDATE_PLAYER 可以更新当前 Battle 玩家资料，却不能修改已经冻结的当前 Players/Dress 节点，只影响下一次冻结。
- 发牌完成后，玩法提交四份最终不可变手牌快照；它们已经包含当前小局命中的自定义牌堆、新手调整或普通散牌调整。Replay Deal D0..D3 和面向各玩家的私有 Deal GameOutput 必须引用同一批牌序，不允许分别重算、重排或再次复制发牌算法。旧 GM 在综合结算中先发送 UPDATE_PLAYER，再发送 0x8650 ACK；因此当前回放 Players.InitScore 始终是开局快照，GameOver Chair.TotalScore 取 ACK 中对应结算值，更新后的 Player.Score 只在下一小局冻结为新的 InitScore。测试和结构不得用一个可变分数字段延迟生成三种语义。
- NHSK 回放文件名前缀固定为 replay package 私有常量 `NHSK`，因为它属于该玩法的文件格式而非 GameID/GameName 身份；不扩充 GameDescriptor，也不增加可变 prefix 配置。ReplayWriter 的应用配置只拥有文件系统根目录。ReplayName 由同一冻结开始事实生成：`NHSK_M<ProductID>R<RoundID>_<YYYYMMDD>_<HHMMSS>_<Seat0UserID>.xml`，Seat0UserID 取 ReplayPlayerSnapshot 的座位 0，日期/时间取 SubgameStartedAt 的 UTC+8 表示。
- 每小局冻结一个独立 `ReplayUID`，值仍为无分隔符的十进制 `SubgameStartedAt.Unix() + CreatorID`。ReplayBuilder 的 Info.UniCode 与 NHSK 客户端 GameResult.ResultDetail.FuPanUID[64] 复用它；后者保持 Go copy 的零填充/截断语义，不额外校验长度。ResultDetail 在玩法本地形成并等待综合结算 ACK，但 FuPanUID 不属于 0x8650 请求 wire。ReplayName 用于 GAME_STARTED、回放文件和后续 GM GAME_OVER，ReplayUID 用于 XML/客户端结果；二者不建立包含、解析或一致性关系。D-047/D-075 已接受的同秒碰撞和覆盖继续不变。
- 不实现 `replay_enabled`、ReplayWriterConfig.Enabled、Noop writer 或 Discard writer。生产组合根必须提供有界 ReplayWriterRunner，测试通过内存 writer 验证；每小局无条件构建并提交 ReplayDocument。相对目录 `FuPan/<YYYYMMDD>/<HH>` 和 NHSK 前缀固定，只有根目录可配。任一小局的序列化、队列、超时或磁盘失败按 D-047 收敛后，下一小局仍重新尝试一次，不自动关闭、熔断或改变 GAME_STARTED/GAME_OVER 线序。
- Legacy GameRule 第 1、2 项不参与机器人或 AI 调度，而归一化为 NHSK 结算字段 `AutoSettlementMinCount`、`AutoSettlementRatioFactor`，默认均为 `-1`。每个合法出牌或过牌都按原代码增加 `MoveCount`；只有非机器人且该动作的 Response 明确为 AI、普通超时、托管或 AI 超时才增加 `AutoCount`，不能根据玩家当前 Auto/Offline 状态追认。两项同时启用时按旧公式 `AutoCount >= MinCount && AutoCount * RatioFactor >= MoveCount`，只启用一项时只判断对应条件；即使 `RatioFactor=50` 也不解释为 50%。成功结算中，失败搭档只有一人被认定托管时，非托管者负分归零，托管者在原负分上额外承担一个本局原始计分单位；两人都托管不修正，非成功结束不修正。`PlayerIsAuto`、综合结算 flags、客户端结果与回放必须由同一认定函数生成。该规则只留在 NHSK。
- `BS_MSG_GM2GL_DRESS` 映射为 `UpdatePlayerDress` Send Command，Legacy 与 Cluster 进入同一 Battle Mailbox，不增加同步 Reply。它只更新已有 UserID 的回放元数据：DressInfo 是旧 codec 解出的不透明字符串，不解析 JSON，允许空值覆盖；相同内容重复 no-op，未知玩家的 Legacy 消息静默忽略并告警。每小局 START 按 Mailbox 顺序冻结当时四人的最新值到回放，因而 DRESS 必须先于 START 入箱才影响该小局。该字段不参与玩法、客户端或重连输出，也不递增 gameplay Revision；8 KiB Legacy 帧上限仍是唯一 wire 大小边界。首版不抽出通用玩家装扮模块。
- 道具库存、价格、权限与使用成功由现有 GM/CMT 负责。GM 成功后转给 GL 的 `BROADCAST_USE_PROP` 映射为 `RecordPropUse` Send Command，Cluster 可投递同一事实；字段只有 SenderID、不透明 PropID、SendCount 和有序 TargetIDs。Legacy adapter 必须核对外层 UserID 与 SenderID 且发送者属于本 Battle；只有当前小局已经开始并存在可写回放时才接受，其他阶段丢弃、告警且不缓冲。记录保持旧 `Prop` Move 的 `PropID/Count/TargetID/dwSenderID` 属性，TargetIDs 不排序、不去重，因此旧 GM 单目标路径产生的重复 ID 也保留。该 Command 无 Reply、客户端输出、玩法副作用和 gameplay Revision；旧 NHSK 从未被容器调用的空 `OnMsgUseProp` 不迁移，也不建立通用道具服务。
- `0x8605 GAME_MSG` 不再作为任意字节进入玩法的通道。Legacy adapter 只解码已确认 allowlist：离线、重连、场景、道具成功广播，以及 `0x7402 GC2GS_RELAY` 内的 NHSK OUT_CARD、CARD_ACTION 和通用 USER_STATE_CHANGE；每项核对冗余身份后映射到独立 Command。未知内层 MessageID 只丢弃、告警和计量。投票解散、骰子定座继续随已放弃配置移除。直接 `protocol.BS_MSG_GAME_BASE (0x7200)` 分支虽然存在于旧 GL，却把整个 wrapper 交给 NHSK 并最终作为未知消息，首版不实现；NHSK 从不调用 `SendOldMsg*`，所以 `0x8655 GAME_MSG_OLD` 同样不实现。正常客户端输出统一走 `0x8644 GAME_MSG`，内部 NHSK MessageID/payload 不变，其中 `0x7611 CARD_ACTION_WATCH` 属于保留输出。
- `ReqGM2GLPlayerLimit` 不进入 codec 或 Command 面。当前 GM 只有 kick 限制时调用发送 helper，但 helper 又把该 GL 控制包封进 `0x8605 GAME_MSG`；旧 GL 的内层 switch 与 controller 均不接收，NHSK 无对应行为，所以旧可观察效果只是 no-op。新 adapter 按非 allowlist 消息丢弃并告警，不“修通”该链路。NHSK 无容器调用点的空 `OnMsgUpdatePlayerInfo` 也删除。将来 GM 替换若需要 kick、continue 或 mini-game 限制，应在 GM 自己的 RFC 中重新定义 owner 和动作；GM 当前直接发给客户端的资料、金额、头像消息不属于 GameLogic。
- 自定义牌堆不把旧 `MakecardConfig` 原样搬入 Battle。示例默认从本地文件加载；生产 Redis adapter 兼容旧 `game:makecard:<ProductID>` 优先、空值回退 `game:makecard:<GameID>` 的键和选择顺序。每个小局开始时，Battle 进入 `Preparing` 并向有界 `CustomDeckRunner` 非阻塞提交一次装载；provider/runner 先按自己的 enable 与账号白名单授权，未授权时不读取数据源，只返回“无可用 catalog”。外部读取、刷新和文本解析同样由 runner/provider 完成，结果通过携带完整 BattleRef 和当前小局身份的 Command 返回。解析 grammar 保留参考行为：牌值 token 只需按十六进制解析为 `uint8`，允许重复和非标准编码；每块不足 104 项忽略，超过 104 项取前 104 项；庄家编号超出 `0..3` 视为未指定，token 或庄家文本无法解析则整次装载失败。Battle 只接受仍匹配当前小局的不可变 `CustomDeckCatalog` 并执行庄家/发牌规则，不读取白名单或数据源配置；下一小局重新装载。读取失败、超时、队列满、空值或无有效块回退普通洗牌，只记录脱敏日志和指标，不隔离牌局；迟到结果忽略，运行中的小局不直接访问 Redis/Nacos/文件，也不观察外部目录的原地突变。
- 回放生成保留参考 XML、文件名和相对目录契约：每个小局使用 `NHSK_M<ProductID>R<RoundID>_<YYYYMMDD>_<HHMMSS>_<Seat0UserID>.xml`，写入 `FuPan/<YYYYMMDD>/<HH>`。综合结算响应成功应用后，Battle 先把客户端 `GameResult` 加入当前已提交输出批次，再冻结不可变回放文档并进入 `FinalizingReplay`；客户端结果不等待磁盘。有界 `ReplayWriterRunner` 在 Battle 外序列化，并以可检查错误的 `MkdirAll/Create/Write/Close` 落盘，不增加原子改名、`fsync` 或自动重试。结果以完整 BattleRef 和当前小局身份 fenced 的 Command 返回。成功、失败或有界超时中最先到达的有效结果才产生面向旧 GM 的 `GameOver` 并进入 `SubgameFinished`；失败、超时或队列满仍使用既定 replay name，记录脱敏告警与指标，不隔离 Battle，迟到结果不得重复发送 GM GameOver。若 DEL_GAME 屏障先到，Battle 不等待或取消已启动 writer，结果 Command 被 fence；磁盘可以留下没有对应 GAME_OVER 的孤立回放，不自动删除、回滚或补偿。已放弃的回放上传不创建 provider、runner 或 HTTP adapter。
- 每个小局的综合结算采用显式单飞状态：`Playing -> AwaitingSettlement -> FinalizingReplay -> SubgameFinished`。进入 `AwaitingSettlement` 前先向全桌输出最终 `ShowCards`，随后只向旧 GM 发送一次 `0x8650` 综合结算请求；等待期间拒绝玩法动作和下一小局推进。结构非法的响应按 Legacy 坏帧规则丢弃；结构有效但局号、玩家或结算内容不匹配时保持等待并记录告警，让 GM 可以发送正确响应。正确响应先产生客户端 `GameResult`，再进入回放收敛；首版不自动重试、不设置结算超时、不增加 RequestID、OperationID 或协议中不存在的 correlation ID。同一有序 TCP 连接每个 Battle 只允许一个在途结算。非等待阶段到达的重复或迟到响应忽略。GM 断开时，未隔离 Battle 仍按连接代际规则停止。
- 当前 NHSK 的 `0x8650` 成功响应必须作为整体通过业务校验后才提交：PlayerData 恰好覆盖当前四名冻结玩家，UserID 唯一，TeamID 唯一且分别等于 SeatID 0..3，TeamCount=4；ResultDetail 只能引用这些 TeamID，零分项忽略，非零 Score 必须为正，同一有向 PayTeamID→GainTeamID 只出现一次。数组顺序不重要。任一条件失败时整包不产生状态、输出或回放变化，Battle 保持 AwaitingSettlement 等待修正响应。`IsSuccess=false` 不消费附带结果，直接废止单飞并按旧可见行为生成 Dissolve(4) 平局零分结果：展示剩余手牌、跳过第二次 0x8650，再按客户端 GameResult、回放、GM GAME_OVER 收尾；内部另记 SettlementFailed 诊断。随后到达的 MATCH_STOP 和任何重复或迟到 0x8650 均稳定 no-op。
- 通过校验后只消费最小权威字段：PlayerData.PlayerID/TeamID 建立身份映射，PlayerData.Flag 提交 IsBreak/IsSeal，ResultDetail 的 PayTeamID/GainTeamID/Score 交易矩阵计算四座有符号结算分。该计算值同时进入客户端 ResultDetail.Score、回放 Chair.TotalScore 和 GL→GM GAME_OVER 玩家 Score。PlayerData.Score 不读取也不与计算值交叉校验；PlayerData.Exp 与 ResultType 只做 wire 解码，玩家 Exp 继续取 ACK 前 UPDATE_PLAYER 的最新值；TeamCount 仅用于等于 4 的结构校验，不进入 Battle 状态。
- `CompleteSettlement` 是应用综合结算的唯一 Battle Command。Legacy adapter 把 `BSAck | 0x8650` 解码后用 Send 投递；Cluster Service 可对同一 BattleRef Send，或用 Call 获取 `{Accepted, Rejection, Revision}`，两者进入同一个 handler。Call Reply 只表达 Command 是否应用，不复制客户端 GameResult、回放或 GAME_OVER；Send 到达时同一 Reply 无副作用。Battle 产生的 `SettlementRequestOutput` 是独立外部请求事实，不是当前 Command Reply，也不复用 Runtime Session。未来结算协调 Service 收到请求、完成外部工作后再投递 CompleteSettlement；Battle handler 不同步 Call 外部结算并阻塞 Mailbox。
- 第一阶段继续由旧 GM 协调下一小局和整个 Battle 的结束。综合结算响应后旧 GM 会发送 `COMMAND CONTINUE`，但 NHSK 当前已完成结算时该命令实际为 no-op；Legacy adapter 必须接受并映射为类型化兼容 Command，Battle 在 `FinalizingReplay` 或 `SubgameFinished` 稳定忽略，Cluster API 不额外公开没有业务效果的 `ContinueBattle`。完成回放与 `GAME_OVER` 输出后，Battle 只进入 `SubgameFinished`，不因本地玩法结果自行 Stop。
- `NEW_GAME ACK` 只证明 Runtime 中已经创建可接收 Command 的 Battle Service 并由 Host 绑定完整 Ref；Host 条目此时可以路由，但 Battle 业务阶段仍是 `AwaitingInit`。Legacy `INIT` 才把 Match/Product/Round、局数上限、计分参数和已确认规则归一化为不可变业务配置。完全相同的重复 INIT 稳定 no-op；身份或配置冲突时告警并拒绝，但不把外部协议错误归类为 Battle 代码缺陷。INIT 前到达的玩家更新、局号准备或启动消息直接拒绝，不缓存、不重排。
- `UPDATE_PLAYER` 是可重复的玩家 upsert，不是第二次 INIT。INIT 完成后且 Battle 未终止时均接受；这覆盖旧 GM 的开局资料、Preparing 随机换座、Playing 中扣费/充值以及 AwaitingSettlement 中 ACK 前分数同步。NHSK 执行 `COMMAND START` 前必须已经存在四个不同的非零 UserID，分别占用 `0..3` 四个不重复座位，并且四人均非 Exited；否则稳定拒绝启动、告警但不隔离，且不产生输出或递增 Revision。后续 UPDATE_PLAYER 重新激活玩家后可以重试 START。从 StartSubgame 起到本小局完成前，UserID 与 SeatID 对应关系冻结；局中换人、换座或占用已冻结座位使整批拒绝并告警但不隔离，其他允许字段仍可更新。AwaitingInit、Preparing 与 SubgameFinished 可以重建座位关系。
- 每个 `UPDATE_PLAYER` 批次先完整校验再原子提交。任一条目具有零 UserID、越界座位、批内重复 UserID 或冲突座位时整批拒绝，不留下部分更新；未出现在批次中的已有玩家保持不变。UserID、SeatID、Score、Exp、AI、PlayerState 和 NickName 是 NHSK 玩家/回放资料；CltID 只供 StartSubgame 冻结回放 Platform，玩法规则不得读取；CntID 解码后丢弃。`PLAYER_EXIT_GAME` 只把已有玩家标为 `Exited`，保留身份和座位并停止向其推送；后续 `UPDATE_PLAYER` 再包含该用户时可以重新进入。旧 wire 中的 `Flag`、`PlayerFlag`、`ScoreChangeReason`、`ScoreChange` 和 `ForceExit` 继续解码但不进入领域状态：当前 GM 只填写 Flag，旧 GL 却只从恒零 PlayerFlag 的 bit 2/4 推导破产/封顶，两套编码没有可用映射。新 Battle 不修补该错位；StartSubgame 将 IsBreak/IsSeal 初始化为 false，当前 0x8650 综合结算 ACK 再按 PlayerData.Flag 的 0x200/0x100 精确赋值。
- NHSK 标准牌组固定为两副不含大小王的 52 张牌，共 104 张，每座 26 张；普通点数顺序为 `3..K < A < 2`。标准玩法只接受单张、对子、三张、三带二和 4～8 张同点炸弹；不增加顺子、连对、飞机等牌型。相同非炸弹牌型比较主点数，炸弹压所有非炸弹，炸弹先比较张数、张数相同再比较点数。自定义牌堆仍按已冻结的宽松调试 grammar 装载，不因此收紧为标准牌集合。
- 每小局普通庄家/首出座位由该 Battle PRNG 随机一次，自定义牌堆可以覆盖。首出不能过；跟牌可以过。三家已过，或已出完座位被跳过并累计到等价条件后，本墩结束，桌面 5/10/K 分牌归最后有效出牌者；若该玩家仍有牌则继续领出，已经出完则由其对家优先开始并继续跳过已完成座位。座位 `0/2`、`1/3` 固定为两组对家；任一组对家都出完时小局结束。
- 名次、抓分、单扣/双扣、胜负组和正负倍数首版逐案例复刻参考 `CalcSuccessResult`，包括当前分支中 `100`、`105` 和 `200` 的不同阈值，不在迁移中擅自统一。以参考测试和补充 golden 表覆盖所有名次排列、阈值边界与异常结束；以后确认规则错误时另立有意偏差。参考 `StartGame` 与 `DoGameStart` 连续两次 `ResetForNextGame` 会无意义消耗两次随机庄家；新实现有意只在每小局初始化一次、只抽取一次庄家随机数，其余规则不变。
- `VerifyCode` 每小局初始化为 1，每次进入询问出牌时增加 2，因此线上序列为 `3,5,7...`。开局输出顺序固定为全桌 `GameInfo`、四份各自只含本人手牌的定向 `Deal`、全桌 `AskOutCard`。玩家非法动作只向本人发送失败 `OutCardResult`，不重发 Ask、不重置当前 ActionDeadline；AI/托管非法候选不发客户端失败包。合法动作不发送成功 ACK：玩家出完时先只向本人展示对家手牌，然后全桌 `OutCardInfo`；若本墩结束再全桌 `TurnEnd`，随后全桌询问下一操作人。小局终局依次为最后 `OutCardInfo`、全桌 `ShowCards`、`0x8650`、综合结算响应、全桌客户端 `GameResult`、回放收敛、面向 GM 的 `GameOver`。所有输出继续由 staged GameOutput 批次在 Revision 提交后交付。
- 上述 NHSK 开局输出之前还存在必须兼容的旧 BaseGame 生命周期线序：先经 `0x8644 GAME_MSG` 向客户端广播无 body 的通用 `0x7205 GAME_START`，再向 GM 发送 `0x8654 GAME_STARTED`，其中 `Res=true` 且 ReplayName 已经固定；随后才运行 NHSK StartGame 并产生 `GameInfo -> Deal -> AskOutCard`。旧 GM 在 GAME_STARTED 后继续发送玩家资料和装扮，新 GL 不吸收这些职责。只有 formatter、没有调用点的 `0x7206 GAME_END` 不实现。
- ClientGameOutput 的投递资格按参考强制发送语义统一：GAME_START 以及 NHSK 的 GameInfo、Deal、Ask、动作、场景恢复和结果等所有玩法输出只过滤不存在或 Exited 的目标，不读取 ClientReady；定向与广播使用同一资格函数，不向领域暴露 force 参数。GL→GM 的 GAME_STARTED、结算请求和 GAME_OVER 等控制输出与玩家投递资格无关。唯一保留的 ClientReady 过滤例外是 D-093 的 ROUND_STAT，它只面向非 Exited 且 ClientReady 的玩家。
- Battle 提交的 ClientGameOutput 必须携带按 SeatID 升序冻结的目标 UserID 列表和不可变玩法 payload。Legacy adapter 把每个目标分别编码为 `0x8644 GLHeader + 0x7400 GameHeader + payload`；外层与内层 UserID 相同，GameInnerID、MatchID、ProductID 来自 Battle 身份，CntTID、CltTID、Reserved2 为零，不生成 UserID=0 广播。GM 根据 UserID 与自身 Player.proxyAddr 继续完成 Agent 路由；Cluster/未来 Agent 同样由自身 SessionRegistry 解释 UserID，Battle 不保存客户端连接编号。GM→GL 输入仍使用 `0x8605 + 0x7402 GameHeader + payload`，不能混淆两个方向的内层 MessageID。
- 普通小局在客户端 `GameResult` 和回放收敛后，先向每名未退出且 ClientReady 的玩家定向发送相同的通用 `0x7246 GAME_ROUND_STAT`，再发送面向 GM 的 `0x8641 GAME_OVER`。BaseRule index 5 控制的跨小局战绩模块已经放弃，因此首版 ROUND_STAT 保持参考关闭状态下的 PlayerCount=0，不用本局 Replay Summary 统计填充，也不实现无主流程调用点的 `PushRoundStat` helper。GAME_OVER 的 PlayerCount 固定为 4，PlayerDatas 必须按 SeatID 0..3 编码，因为 wire 元素只有 Score/Exp/Auto、旧 GM 按数组下标关联 SeatList；Score 取最终结算分，Exp 取 ACK 前最新 UPDATE_PLAYER，Auto 取本局最终托管认定。四座不完整是内部不变量缺陷，按既有策略隔离当前 Battle，不能发送错位数组。`IsGameOver` 保持参考 NHSK 的 0，Reason 为正常/MATCH_STOP 的 Success(0) 或结算失败的 Dissolve(4)，ReplayName 使用本局冻结值，约局字段为零。旧 GM 根据 `IsGameOver || SubGameNum == MaxSubgameNum` 及大局计数判断下一局或结束 Round，新 Battle 不复制这套协调判断。`0x864E NOTICE_ROUND_OVER` 只在 `MATCH_STOP` 强制收尾时于 GAME_OVER 后发送；强制 NHSK 路径展示牌、跳过 `0x8650` 综合结算，再生成客户端结果、回放、ROUND_STAT 和 GAME_OVER。正常大局结束不发送 NOTICE。
- ReplayName 只属于当前小局：StartSubgame 冻结后供 GAME_STARTED、回放文件与同局 GAME_OVER 共用，下一小局以新值替换。新 Battle 不复制旧 BaseGame 只有追加、没有读取者的 `replayNames` 列表，也不补偿旧 GM 只在真实大局结束时上报最后一次名称的行为。GAME_OVER 的 GameOver/Reason 继续在 Legacy DTO 中编码既定值，但旧 GM 实际不消费该参数，因此它不进入 Battle 的额外领域状态或控制流。未来新 GM 若需要整场回放列表，另立有消费证据的回放索引契约。
- `MATCH_STOP` 的业务边界按旧 BaseGame `IsPlaying` 而不是按“是否仍可出牌”判断。新 Battle 在 `Playing` 或 `AwaitingSettlement` 接受它：取消行动 Timeline；若已有 `0x8650` 单飞则使其失效并 fence 后续响应；随后使用 `GAME_OVER_REASON_SUCCESS(0)`，展示牌、本地计算结果并跳过综合结算。目标生产 `MsShowCard=0`，不新增展示 Timer。`AwaitingInit`、`Preparing`、`FinalizingReplay` 和 `SubgameFinished` 稳定 no-op。若该强制流独立完成，线序为客户端 GameResult、回放、GAME_OVER、NOTICE_ROUND_OVER；但旧 GM 随后发送的 `DEL_GAME` 仍按停止屏障立即收敛，保留已提交输出并 fence 尚未完成的回放结果，不等待磁盘、不补发生命周期消息，也不把参考 TurnEngine 与删除之间的竞态固化为可靠投递承诺。
- 同一 Legacy 连接连续到达 MATCH_STOP、DEL_GAME 时，adapter 先把 MATCH_STOP 投递到目标 Battle，再让 Host 将精确条目标为 Stopping 并向该 Battle Mailbox 投递删除屏障。屏障前提交的状态与 GameOutput 不撤回；屏障后取消 Timeline、禁止新输出并 fence 外部结果。已交给 ReplayWriterRunner 的文档允许继续写盘，但完成结果若晚于屏障只被丢弃；不等待 I/O、不删除可能形成的孤立文件、不补发 GAME_OVER/NOTICE。Runtime Stop 真实完成前 Host 不删除绑定、不释放 BattleID，迟到结果携带的旧 BattleRef 与小局身份也不能命中复用后的实例。
- Legacy `CARD_ACTION` 映射为类型化 `PreviewCardSelection`，Cluster 可调用同一 Command。它只在 `Playing/WaitingForAction` 接受当前操作玩家，修复参考仅比较残留 OutingCardSeat 而可能在结算阶段误广播的问题；成功后按原 MessageID 向全桌广播 UserID 与客户端提供的选牌列表，并按成功 Command 提交 Revision。为保持当前可观察行为，首版只校验 wire 结构和固定数组上限，不校验牌是否属于手牌、重复次数、牌型或能否压过上家，允许空选择；真正 `OUT_CARD` 继续执行完整权威校验。当前 NHSK 默认始终启用此能力，不迁移实际未接入的 BaseRule index 2 开关。
- NHSK 的本地自动策略不搜索可压牌：领出时按逻辑点数选择手中最小单张，跟牌时直接过牌；外部 AI 继续作为独立 provider。普通玩家第一次超时且 BaseRule `TimeoutAutoMove=true` 时进入托管，先产生全桌状态通知，再立即按本策略完成本次操作；后续轮次使用托管期限。玩家提交有效人工动作时先退出托管再处理动作；重连和场景恢复沿用相同取消规则，旧 AI 结果由 VerifyCode 与 TurnRevision 拒绝。
- 玩家主动切换托管时，非当前操作人只改变状态。当前操作人开启托管且普通期限剩余大于 100ms 时，取消该期限并立即投递自动操作；剩余不超过 100ms 时保留即将到期的原 Command，避免边界重复执行。托管状态变化按参考行为产生一次全桌通知和一次请求者定向确认。BaseRule 明确设置 `TimeoutAutoMove=false` 时，普通玩家期限到达不代替出牌，Battle 继续停留在当前操作机会并等待人工输入；这是业务配置允许的无期限等待，不触发 Host watchdog。字段缺失时示例默认 `true`。
- 小局启动的真实必需顺序是 `UPDATE_GAME -> COMMAND START`。`UPDATE_GAME` 携带权威 `GameNum/SubGameNum`，映射为 `PrepareSubgame`，校验当前 Battle 和局号推进后进入 `Preparing`；`COMMAND START` 映射为 `StartSubgame`，只在初始化完成、四座位有效且当前小局准备完成后进入 `Playing`。`START_NEW_GAME` 只携带比赛总时长、已用时长和 `RoomInfo`，映射为可选的 `UpdateRoundContext`，不改变业务阶段，也不是 START 的必要前置。GM 自动推进下一小局时只发送 `UPDATE_GAME -> START`；MatchServer 对已在运行的 Round 再次下达 CreateGame 时，才会在此前额外发送 `START_NEW_GAME`。Legacy 与 Cluster 共用这三个类型化 Command，不增加合并启动 API；乱序、缺少准备、重复启动或旧局号都不启动玩法。
- `MATCH_STOP` 与 `DEL_GAME` 不合并。`MATCH_STOP` 是进入 Battle Mailbox 的业务停止 Command，只结束或强制收尾当前玩法，不调用 Runtime Stop，也不释放 `BattleID`。`DEL_GAME` 才是正常 Battle 的权威删除请求：Host 先把条目标为 `Stopping`，再经生命周期 runner 向同一 Battle Mailbox 投递停止屏障；屏障前已经入箱的 `MATCH_STOP` 等 Command 先执行，屏障后的玩法一律拒绝。屏障处理只取消 Timeline、停止接受输出并 fence 迟到 AI、自定义牌堆和回放结果，不为了删除补做结算或等待外部工作；屏障完成后 runner 才调用 Runtime Stop，真实返回后 Host 删除绑定并释放编号。连接断代使用同一停止路径。`Quarantined` 条目仍优先保留：`DEL_GAME` 只记录外部结束事实，不投递屏障、不 Stop、不释放容量。
- Legacy `GLHeader.GameInnerId` 是旧 GM 从有限编号空间分配的 Round 编号。`BattleID` 冻结为 `uint32`，`0` 无效；第一阶段直接使用 `GameInnerId`，不转换为字符串、不增加来源前缀，也不维护独立的 `legacywire.GameID <-> BattleID` 映射。BattleID 只要求在同时进行的 Battle 中唯一；实例完全结束后可回收复用。Cluster 调用方先从稳定名字 `.nhsk-game-host` 取得 `NHSKHostService`，再按 `BattleID` 解析具体 `BattleRef`，之后直接对该 Battle `Send`/`Call`。两条入口最终进入同一个 `BattleService` 和玩法 Handler。
- `NHSKHostService` 持有 `BattleID -> BattleRef`、创建请求和 Battle 生命周期；Legacy bridge 只转换旧协议字段并解析当前 `BattleRef`；`BattleService` Mailbox 持有牌局权威状态；连接 adapter 只持有 socket、frame、codec 与有界 I/O 队列。
- `BattleID` 由创建调用方从有限编号空间分配：第一阶段直接使用旧 GM 的 `uint32 GameInnerId`，后续由新的 GSR GameMaster/协调 Service 分配空闲编号。Host 成功创建 Battle 后返回对应的完整 `BattleRef`，不生成编号。相同编号只有在旧 Battle 已完全收敛后才能复用；同一节点进程内的新实例必须返回新的 `{NodeID, ServiceID}`。节点断线或重启后旧 Battle 整体失效，长期名字重新解析，业务不得继续使用旧 Ref。
- 每局动态创建新的 BattleService 和 ServiceRef；结束后由组合根拥有的生命周期 runner 调用 Runtime Stop，Host 只在 Stop 完成后删除最终绑定并允许编号回收。第一阶段不预创建 `MaxBattleID` 个空闲 Service，也不重置已完成 Service 承载下一局。Host 以 `MaxActiveBattles` 限制同时活动实例数；ServiceRef 的 `uint64 ServiceID` 只标识 Runtime 实例，不是内存地址，停止后 Registry、Timer、PendingCall 和 Mailbox 必须收敛。
- 旧 GM 代码表明 `0` 表示未绑定游戏，`RoundMaxInnner` 当前配置为 `10000`，设计意图是编号超过上限后从 `1` 重新使用；删除 Round 时会删除 `roundsInner` 索引。新 GSR GameMaster 的编号池默认范围冻结为 `1..10000`，上限可配置。参考实现的回绕条件错误地检查 `curRound.InnerId` 而不是递增计数器，实际不会按预期回绕；新实现只继承有限编号池语义，不复制该实现。它必须跳过仍在活动集合中的编号，并在没有空闲编号时明确拒绝创建。
- 正常创建契约遇到活动 `BattleID` 冲突时返回 `ErrBattleIDInUse`，不覆盖牌局。旧 GameLogic 的 `CreateNewRound` 会在异常同号 `NewGame` 到达时清理旧 Round 并创建新 Round；第一阶段仅对 `Active` Legacy 条目保留这一可观察兼容行为：记录告警，先通过生命周期 runner 完全停止旧 Battle，再用同一编号和新的完整 `BattleRef` 创建实例。若同号条目已经是 `Quarantined`，Legacy bridge 记录告警并回复 `Res=0`，不得停止或替换。该策略不进入 Host 的普通 Cluster 创建 API；后续新 GameMaster 不得主动触发它。
- Battle 异常必须按原因分类。普通 Handler 或 Timer Handler 的 panic、状态机不变量破坏和 Handler/Stop 超时属于程序缺陷，不自动 Stop、不自动重建、不回收 `BattleID`。GM 连接断开可以使尚未隔离的 Battle 进入 Stop 并在真实返回后回收，但不能覆盖已经进入 `Quarantined` 的条目。隔离条目收到 `DEL_GAME` 时只幂等记录外部已结束事实，不 Stop、不释放容量：首次观察保存由注入 Clock 产生且之后不可变的时间，重复消息只更新最近观察时间、观察次数和本次 ConnectionGeneration；同号 `NewGame` 按上一条返回 `Res=0`。这是相对参考实现的有意偏差：后续正常协议消息不能销毁缺陷证据。参考实现的 `OnTimer` 会 recover、记录日志并返回 `false`；新实现不复制该行为，因为 panic 发生时业务状态可能已部分更新。
- 程序缺陷只使对应 Host 条目进入 `Quarantined`。该条目继续占用活动容量，针对该 Battle 的普通 Resolve、Send、Call 和同号创建均被拒绝；GM 连接随后断开也不覆盖隔离状态，不自动 Stop、释放或让新代际覆盖同号条目。其他活动 Battle 继续运行，不断开 GM 连接，不触发整个 GameLogic 回切。节点通过健康状态和 Inspection 报告 `Degraded`，但隔离不会伪装成业务结束，也不会向旧 GM 发送成功结算。`Degraded` 节点继续接受不同 `BattleID` 的 `NewGame`，直到所有非终态条目总数达到 `MaxActiveBattles`。
- Host 容量计算包含 `Creating + Active + Stopping + Quarantined`。隔离条目不会因长期存在而绕过容量；容量耗尽时新创建明确失败，已有健康 Battle 不受影响。局部释放隔离条目后才归还对应容量。
- 首版诊断导出使用组合根持有的本地文件 exporter，Core Runtime、Host 和 Battle Handler 不直接执行文件 I/O。exporter 把每份材料写入配置根目录下的独立临时目录，至少包含 `manifest.json`、`snapshot.json`、`commands.jsonl`、`panic.txt` 和 `runtime-inspection.json`；最后稳定 Snapshot、输入 Record、随机种子、Clock、失败 Command、Command Record Sequence、TurnRevision、TimelineRevision、stack、连接代际和 Runtime Inspection 等材料完整写出后，依次同步所有文件与临时目录、原子改名为 receipt 目录并同步父目录。只有这些步骤全部成功才生成不透明诊断 receipt。receipt 必须绑定 `BattleID`、完整 `BattleRef` 和导出材料摘要；它只证明该实例的现场已经可靠保存，不自动改变 Host 状态或触发 Stop。运维人员拿到诊断材料后，显式提交携带这三项的 `ReleaseQuarantinedBattle`；Host 精确校验成功后才通过生命周期 runner 停止仍存在的实例并释放条目。写入、同步或改名失败不得生成 receipt，旧实例 receipt 也不能用于释放复用相同编号的新实例。首版不依赖 MySQL、Redis 或对象存储；后续可替换 exporter，但不能改变 Host 与释放契约。
- Battle 第一次进入 `Quarantined` 后自动向组合根有界诊断 runner 提交一次导出。提交不得阻塞 Host Mailbox；队列满时记录 `ExportPending`，写入失败或有限自动重试耗尽时记录 `ExportFailed`，两者都保留隔离状态和容量占用，并允许运维显式重试。成功签发 receipt 后记录 `Exported`。释放 Battle 不删除 receipt 目录；诊断材料清理是与 Battle 生命周期分离的人工操作。
- 首版诊断运维面只提供节点本地管理 CLI：列出隔离项、发起或重试导出、携 receipt 释放以及独立清理诊断目录。它不复用 Legacy GM TCP、不增加旧 MessageID，也不开放普通 Cluster Service API。远程运维必须等完整身份认证与授权契约明确后另行设计。
- 隔离前后必须保存诊断事实：`BattleID`、完整 `BattleRef`、连接代际、最后成功 Command、导致失败的 Command 元数据、Command Record Sequence、Subgame identity、TurnRevision、Timeline ID/Revision、最后稳定 Snapshot、固定随机种子、Clock、输入 Record、panic stack/稳定错误、Runtime Inspection 和相关日志。业务 Handler 不执行文件 I/O；组合根的有界诊断 runner 使用现有 Record/Snapshot seam 导出本地诊断包。诊断材料不得包含 token、secret、proof 或完整身份凭据。
- Runtime 发生 panic 时可能已经隔离并移除 Service 实例，因此“保留现场”不能依赖继续持有可变业务对象。可复现依据是最后稳定 Snapshot、输入 Record、随机种子、Clock 和 panic stack。取证确认后，操作者可以用精确的 `BattleID + 完整 BattleRef` 和诊断 receipt 局部释放隔离条目；若实例仍存在则先经生命周期 runner Stop。该操作不释放其他 Battle，也不触发整体回切。第一阶段不提供自动 Release；代码修复通过正常部署流程重新上线。
- Host 不定义 `BattleMaxLifetime`、`LastProgressAt` 扫描或通用停滞 Watchdog。出牌、托管、结算和结束等待时间由具体玩法配置，并通过 Battle Timeline 安排；Timer 绑定安排时的完整 ServiceRef，只投递带 Timeline Revision 的 Command，BattleLogic 在 Mailbox 内决定自动操作、继续、失败或结束。单纯运行时间长不触发隔离。Timeline 投递失败、过期 Command 被错误接受或业务到期处理违反不变量时，才按明确技术错误进入诊断路径。
- 宁海双扣首版为实际启用的玩法阶段建立 Timeline 调度。每个 `TurnRevision` 只能有一个有效 `ActionDeadline`。普通玩家使用 `MsFirstOutCard`/`MsOutCard`，到期产生 `AskRespTimeOut`；托管/机器人在 `MsOutCardRobot > 0` 时使用专用期限并产生 `AskRespAuto`，外部 AI 在 `MsAITimeout > 0` 时使用硬期限并产生 `AskRespAITimeOut`。专用期限为 `0` 时回退到普通期限与 `AskRespTimeOut`，不会形成无期限回合。有效 AI 结果到达后，真实机器人立即以 `AskRespAI` 完成；离线托管玩家在生产 `MsOutCardRobot=1000ms` 最小延迟尚未到达时，先取消 AI 硬期限，再用剩余最小延迟替换唯一的 `ActionDeadline`，保存的候选到期后应用。同一时刻不得同时保留硬期限与最小延迟 Timer，也不复制参考实现并行启动 `OutCard` 与 `OutCardRobot`/`OutCardAI` 的双 Timer。玩家响应、AI 完成、当前期限到期或回合推进都会使旧 Revision 失效。Timer 投递失败或 Handler panic 进入该 Battle 的诊断隔离。`Deal`、`ContinueDelay` 虽然有配置与处理函数，但没有启动点，首版不主动调度；`StartTimer` 的可选参数也没有生产调用，首版不公开 timer-key 参数。参考代码中的 `TimerGameOver` 只有枚举值和统一取消调用，没有启动点或 handler，冻结为未使用遗留项：首版不实现、不分配 CommandID。
- 当前 GM 没有 PAUSE 发送点，GM 的暂停入口仍是 TODO，NHSK 暂停方法只有直接测试且没有被旧 Round 正确接通。第一阶段因此不实现暂停/恢复 Command、状态或 Timeline 分支；`COMMAND` 只覆盖当前实际发送的 START、CONTINUE 和 MATCH_STOP。D-027 被 D-045 取代，未来出现真实暂停需求时再重新定义时间语义。
- 第一阶段保留旧 GameLogic 的外部机器人能力，但只作为可替换的兼容 adapter。Battle 在 Mailbox 内产生不可变、类型化的 `AIRequest`；请求保存完整 ServiceRef、UserID、SeatID、TurnRevision、VerifyCode 和请求起始时间，组合根拥有固定容量、可关闭并等待真实返回的 `AIRunner`，通过 `AIProvider` 执行请求，再将结果 Command 投回原 Ref。RobotTran 响应中的 MatchID、RoundID 和内层 VerifyCode 不成为权威身份；返回 SeatID 必须匹配请求，Battle 再对候选牌执行与玩家出牌相同的手牌、牌型、压制和回合校验。默认本地 provider 不访问网络；可选 Legacy HTTP provider 使用标准库向生产 URL POST `application/json; charset=utf-8`，精确编码 `game_id + data` JSON，其中 game_id 来自进程级 GameDescriptor；base64 `data` 内是小端 `ASK_MOVE_WITH_SCENE` envelope、MatchKey、Scene/Move Suffix、`moveMS=MsOutCardRobot`、AI Scene 和 AskOutCard。生产 `moveMS=1000ms` 与 Battle `MsAITimeout=6000ms` 硬期限分开。响应保持 `code/message/data` JSON 和 base64 二进制 envelope。HTTP 超时、连接失败、非成功状态、响应格式错误、Runner 队列满和非法候选只记录脱敏日志与指标，不隔离 Battle，也不改变当前硬期限；有效结果按上一条最小延迟规则完成。旧 Ref 或错误请求上下文的迟到结果忽略。请求、响应、完整场景和手牌不得写日志。已配置 provider 可以报告 `Degraded`，但不拒绝新牌局。
- 宁海双扣规则层不直接写 TCP，也不在规则处理中调用通用 Broadcast 发送客户端玩法输出。它为当前 Command 计算候选状态与不可变 `GameOutput` 批次；专用 NHSK BattleService 确认规则成功后先提交状态，再按批次顺序交给 `GameOutputService`。规则拒绝或提交前 panic 时整批未交付输出丢弃。该 seam 不依赖通用 BattleRevision。组合根为每个 GM TCP 连接代际创建一个 GameOutputService，该代际上的所有 Battle 保存其 Ref 并共享该 Service。单一 Mailbox 保持批次内部顺序，并按不同 Battle 的批次进入 Mailbox 的顺序交给当前 connection sink；只有 `LegacyGMConnection` 的单 writer 执行 socket I/O。GameOutputService Mailbox/Service 拒绝、sink 队列满或 TCP 写失败都属于连接级故障：不回滚已提交 Battle，关闭当前连接代际，丢弃该代际未发输出，不跨重连重放；旧 GM 按既有逻辑结束该连接关联的全部 Round。该局部 staged-output seam 不改变通用 BattleContext 的 Reply、Send、Broadcast、Timeline 直接语义，也不宣称跨 Service 事务。
- GameOutputService 只有在输出 Batch 已进入其 Mailbox 后才能收敛 sink/write 故障。专用 NHSK BattleService 对该 Ref 的异步 Send 若同步返回 Mailbox 满、Service 已关闭等拒绝，必须调用应用层 `ConnectionFailureReporter.FailConnection(generation, stableKind)`。Reporter 由 `LegacyGMConnection` 生命周期 owner 实现，必须非阻塞、并发安全、幂等，并且只取消仍匹配的当前连接代际；旧代际迟到报告不关闭新连接。它只接收连接代际和稳定失败类别，不接收 Battle 可变对象，不修改牌局、不重连、不重放，也不进入 Core 或通用 `game` 包。
- 每次物理 GM TCP 连接建立后立即分配新的 ConnectionGeneration，包括最终在握手阶段失败的连接。连接 owner 先完整写出与旧 `NewBSProtocol(BS_CONNECTIONTYPE_GAME_LOGIC)` 相同的本方 origin frame，再读取并校验 GameMaster 回发的 `BS_CONNECTIONTYPE_GAMESVR` origin；随后由组合根创建该代际唯一的 GameOutputService。参考 transport 的主动端与接受端最终都调用 `CreateClient`，而 `CreateClient` 会调用各自 `BSProtocol.ConnectedEvent`，所以实际协议交换双向 origin。状态顺序固定为 `Connected -> OriginVerified -> OutputReady -> Ready`。只有 Ready 才开放普通业务 frame、允许 `NEW_GAME` 进入 Host，并让节点的 GM-link readiness 为 true。OriginVerified 到 OutputReady 期间 reader 不解码或投递业务 frame；底层 TCP/buffered reader 可以暂存字节，但应用层不建立待处理消息队列。origin 写入、读取或校验失败，OutputService 创建失败或连接提前关闭都直接关闭该代际。断开收敛顺序为：禁止新输出，停止该代际 GameOutputService，关闭/取消连接，等待 reader 与 writer 真实返回；未发送输出丢弃。
- GameLogic 初次连接失败或已建立连接断开时不退出进程，GM-link readiness 保持 false。只有连接 owner 的状态机执行拨号和建立流程：以有上限的指数退避持续重试到进程关闭，`Send` 不触发连接，同时最多一个 Dial/建立流程。默认初始退避为 1 秒、倍数为 2、上限为 30 秒、jitter 为 ±20%；Dial 与 origin 握手超时均为 5 秒。连接连续 Ready 60 秒后才把下一次退避重置为 1 秒；更早断开按失败继续增长。以上参数属于 `LegacyGMConnection` 应用配置，均可调整，但 jitter 必须大于 0 且小于 1，不能关闭。每次重试成功都进入新的 ConnectionGeneration。旧代际停止接受输入和输出，并通过生命周期 runner 收敛其 `Creating`、`Active`、`Stopping` Battle；已经处于 `Quarantined` 的 Battle 保持隔离并继续占用容量，直到精确的人工局部释放。不缓存、重放、迁移或恢复旧代际状态；新代际只承载旧 GameMaster 后续创建的新牌局，也不能覆盖隔离编号。
- Battle 创建与停止不在 Host Handler 内调用 Runtime。Host 校验创建编号、容量和连接代际后，保存带 OperationID/BattleID 的 `Creating` 条目并非阻塞提交组合根拥有的有界 `BattleLifecycleRunner`。Runner 执行 `Runtime.CreateService` 和 Init，把成功或稳定错误以及完整 ServiceRef 以 Command 返回 Host；Host 只在结果仍匹配当前 OperationID、BattleID 和连接代际时绑定 `BattleRef` 并转为 `Active`。Legacy `BS_MSG_GL2GM_NEW_GAME` ACK 必须在该状态转换完成后发送：`Res=1` 表示 Battle 已可解析和接收后续 Command，Runner 仅接收任务不能提前成功。队列满、Create/Init 失败时 Host 删除 `Creating` 并发送 `Res=0`。若 Service 已创建但完成结果无法交回 Host，Runner 必须 Stop 孤儿实例。Host 确认 `Active` 前收到的 INIT/START/玩家消息不缓存、不提前路由。同一连接代际、同一 BattleID 在 `Creating` 中再次收到相同 payload 时合并到当前 Operation，不重复 Submit，最终只发送一次 ACK；payload 不同则记录两份脱敏摘要并立即发送 `Res=0`，不取消当前创建、不并行创建、不保存待替换队列。Legacy 同号强制替换只作用于 `Active`。Cluster 在 `Creating` 或 `Active` 都返回 `ErrBattleIDInUse`；其创建同样返回创建操作并通过结果 Command 或显式查询取得最终 `BattleRef`，不阻塞 Service Handler。
- 玩法产生类型化 `GameOutput`。第一阶段由 Legacy egress 编码成 `0x8644 GLHeader + BSREQ_GS2GC_RELAY_HEADER + NHSK Suffix`；未来替换 GameMaster 时改为 Cluster sink，玩法代码保持不变。
- 为棋牌游戏应用提供结构化日志、MySQL 持久化、Redis 快速存储、配置、启动、健康检查和优雅关闭脚手架。
- 在现有 `tooling/entry` 基础上装配 Gateway、Login 和第三方 Auth；首个生产 provider 实现微信认证，同时提供无需外部平台即可完成测试的开发认证 provider，并保留其他 provider 的窄扩展 seam。
- 开发认证使用 `account + shared token`，不提供注册流程，只在显式开发配置下启用，优先保持示例的外部依赖最小。
- Gateway、Login、Auth、Agent 和 Game 均可作为独立进程与 Cluster 节点部署，通过 `ServiceRef`、Command 和现有 Cluster Transport 协作。
- Agent 节点承载多个 AgentService 实例，每个已登录玩家会话对应一个实例。Gateway 拥有 socket；AgentService 拥有会话路由和断线保留窗口；PlayerService 拥有玩家长期状态；BattleService 拥有牌局权威状态。
- 无活跃牌局的离线 AgentService 保留 2 分钟；活跃牌局中的实例保留到牌局终局后 2 分钟；所有离线保留受 10 分钟绝对上限约束。重连签发新会话代际并替换旧连接。
- 游戏服能力以 Command 为唯一业务入口。集群内 Service 直接使用 `Send`/`Call`；认证后的客户端继续使用原有 Legacy TCP 二进制消息。参考实现中的 `0x7701` 出牌和 `0x7702` 动作记录均映射为 `Send`，不制造同步 `Reply`；结果、拒绝和广播继续使用原异步下行 MessageID。
- 用宁海双扣跑通登录、认证、4 人入桌、发牌、出牌/过牌、抓分、单扣/双扣结算和断线重连，再从第二个玩法验证哪些能力值得进入通用模板。首版权威状态保存在 Service 内存，不承诺进程崩溃恢复。
- MySQL、Redis 首版只提供连接、配置、健康检查和生命周期工具模块，不接管业务权威状态，也不是宁海双扣示例的启动依赖。

进入实现前必须先冻结：

1. 单条 GameLogic → GameMaster 主动连接的 frame 细节和 origin golden bytes；双向 origin 的生成、发送和消费路径已由 `nbgame_core` 源码确认。断线已确定不跨代际缓存或重放，GameMaster 结束回合并删除链接；新 GameLogic 停止旧代际本地 Battle，并持续退避重连，新代际只接新局。
2. GameLogic 必须兼容的 GM2GL/GL2GM 容器 MessageID 全集，以及首个宁海双扣切片允许明确拒绝的消息。
3. `0x7402`、`0x8605`、`0x8644` 双向分层封包的精确字段宽度、长度含义和 golden bytes。
4. 宁海双扣首版规则范围、最小可玩路径和旧/新 GameLogic 对拍语料。
5. GameLogic 切换脚本、健康门禁和失败回切步骤。部署已确定为一个 GameMaster 只连接一个 GameLogic；新旧 GameLogic 不同时接入。切换或回切都会由 GameMaster 结束断开连接上的现有 Round，不迁移或恢复进行中的内存牌局。
6. 后续 MySQL、Redis、微信、Gateway 和 Agent 目标；这些不阻塞第一阶段 GameLogic 替换。

分阶段方向：

```text
14A GameLogic 双向 TCP 契约与 golden corpus
  -> 14B GameHost/Battle Command 与宁海双扣纵向切片
  -> 14C 新旧 GameLogic 离线对拍、单实例切换与失败回切
  -> 14D 日志、MySQL/Redis 工具和运维脚手架
  -> 14E 替换 GameMaster，Legacy egress 改为 Cluster sink
  -> 14F 替换 Agent，并接入 Auth/Login/Gateway
  -> 14G 第二玩法、通用模板回收和容量验收
```

边界：

- Core Runtime 不引用 MySQL、Redis、微信、客户端协议或棋牌游戏领域类型。
- 第一阶段不要求旧 GameMaster、旧 Agent 或客户端理解 GSR，也不要求它们修改协议。
- 单个 `LegacyGMConnection` 属于 Game 应用 adapter，不进入 Core；它主动连接 GameMaster并拥有同一 socket 的 reader、单 writer、有界队列和连接代际。它可拥有受生命周期管理的 I/O goroutine，但不得在 Handler 外修改 Service 状态。
- 连接 origin 由两端各自的 `NewBSProtocol` 自动发送。主动 GameLogic TCP client 创建时，`BSProtocol.ConnectedEvent` 发送 `BS_CONNECTIONTYPE_GAME_LOGIC` origin；GameMaster TCP server 默认启用 `CheckOrigin`，首包不是 `BSMsgOrigine` 时不创建 controller，并把首包的 `Origine` 传给 `ClientCreator.CreateClient` 选择 `GameController`。服务端读取首帧后通过 `AcceptClient -> CreateClient` 包装 accepted socket；该 `CreateClient` 同样调用 GameMaster 的 `BSProtocol.ConnectedEvent`，因此向 GameLogic 回发 `BS_CONNECTIONTYPE_GAMESVR` origin。新 adapter 必须复现并校验这个双向握手；golden test 冻结两端精确字节和 origin 先于业务 frame 的时序。
- GameMaster 断线回调会结束所有绑定断开 GL 地址的 Round、删除 connection，之后不再向该地址推送。新 GameLogic 同步收敛旧 ConnectionGeneration 关联的本地 Battle，不增加跨连接缓存、重放、迁移或旧回合恢复；连接状态机继续退避重连，新连接只接收新牌局。
- `NHSKHostService` 只拥有 `BattleID -> BattleRef`、创建请求和 Battle 生命周期，不能成为第二个牌桌状态 owner。Cluster 调用方解析到 `BattleRef` 后直接投递玩法 Command，不让所有牌桌消息串行经过 Host；Legacy bridge 用规范化后的 `BattleID` 定位同一个 Battle。牌局的所有状态变化仍只在 BattleService Mailbox 内发生。
- 第一阶段冻结旧 GameMaster 的现有职能。它继续承担玩家/旧 Round/GL 之间的协调、Legacy `GameInnerId` 分配与回收、生命周期消息和双向 Relay；新 GameLogic 直接把这个数值视为 `BattleID`，不要求旧 GM 理解 `BattleRef`。后续替换 GameMaster 时再把它改为 GSR 协调者：由它从有限编号空间分配 `BattleID`，请求 Host 创建 Battle，接收并保存 Host 返回的 `BattleRef`，在实例完全结束后回收编号，再直接 `Send`/`Call`；该改造不进入第一阶段。
- 旧 TCP 状态变更消息映射为 `Send`。允许返回当前处理结果的 Command 只实现一个 Handler：状态变化和异步输出提交后调用 `Reply`，经 `Send` 到达时 Reply 无副作用，经 `Call` 到达时调用方取得结果。`BSAck | 0x8650` 同样只映射 CompleteSettlement Send；Cluster 对该 Command 的可选 Call 只取得是否应用与 Revision。Timer 等纯事件保持 Send-only；任何情况下都不能把 `0x8644` 异步输出、SettlementRequestOutput 或后续回放/GAME_OVER 解释成 Reply。
- 回复分成两条语义通道：`CommandResult` 只回复直接 `Call` 的调用者；`GameOutput` 承载玩家定向消息、广播、拒绝和结算，第一阶段经 Legacy TCP 发给 GameMaster。玩法不得按 TCP/Cluster 来源分叉规则，也不得用 Command Source 推断输出目标。
- Service Handler 不执行阻塞数据库、Redis 或第三方 HTTP 调用；外部 I/O 由组合根拥有的有界 adapter/runner 执行，结果经 Command 回到状态 owner。
- 首版所有业务权威状态均由对应 Service 内存持有。MySQL、Redis 只交付未接入业务的工具模块；未来接管数据前必须新增 RFC 明确 owner、丢失与重建语义。
- 日志、Snapshot 和 Record/Replay 不记录 token、secret、proof、微信凭据或完整个人身份材料。
- 不因一个玩法的便利性提前增加通用模板；至少以首个完整玩法证明边界，再用第二个玩法验证复用价值。
- 宁海双扣第一阶段作为 GSR 具体业务纵向切片放在 `examples/nhsk`。组合根、GameHost、Battle factory、玩法 Logic、Command、输出模型和 Legacy bridge 在该切片内按职责分文件，不新增 `cmd/`、`app/`、`adapter/`、`auth/` 或具体玩法顶层目录。只有不依赖玩法类型的 Legacy 字节协议与连接实现进入 `examples/nhsk/internal/legacywire`；第二个真实调用方出现前，不把这些实现上移到 `game`、`tooling` 或 `transport`。
- `/Users/lijiawang/Documents/cocos/laya/nhsk`、`/Users/lijiawang/Documents/cocos/laya/gamelogic`、`/Users/lijiawang/Documents/cocos/laya/gamemaster`、`/Users/lijiawang/Documents/cocos/laya/gamecore`、`/Users/lijiawang/Documents/cocos/laya/protocol`、`/Users/lijiawang/Documents/cocos/laya/baison_middle/protocol` 和 `/Users/lijiawang/Documents/cocos/laya/nbgame_core` 共同作为宁海双扣的只读知识来源。Phase 14 默认不修改这些目录，不直接依赖其 module，不把旧容器接口当作 GSR 公开契约。
- 宁海双扣实现必须使用 GSR 的 Service、Command、Mailbox、Timer、生命周期和 adapter 约定，不按旧目录或旧容器接口仿写。每完成一个可验证功能切片，都必须回查上述参考目录的对应入口和测试，并在核对记录中逐项标记“已一致”“有意偏差”“发现遗漏”。有意偏差必须链接已接受 RFC；发现未裁决的偏差或遗漏时，该切片不能宣告完成，必须先更新 RFC 再继续实现。参考目录的原业务代码、配置和资源保持不变；允许写入 `.codegraph/` 等分析工具元数据。
- `nhsk` 用于提取具体规则、游戏消息布局、重连和结算；`baison_middle/protocol` 用于冻结底层 BSHeader、Suffix 和共享 Relay wire，`protocol` 用于提取 GameLogic 上层消息和 MessageID；`gamelogic`、`gamecore` 用于理解旧运行容器与玩法接口；`gamemaster` 用于确认连接登记、双向消息、Round 绑定和断线收敛；`nbgame_core` 用于确认 Legacy transport 的连接、握手、frame 和生命周期语义。
- 旧 `Round` 与 `BaseGame` 的多层队列不得照搬。新实现由 BattleService Mailbox 单点串行化牌桌状态；玩法 factory、Timer、输出路由、持久化和外部调用分别进入组合根、Runtime Timer、Agent Command、Repository/Ledger runner 或窄 adapter。
- Legacy TCP 只兼容认证后的宁海双扣游戏消息，`BS_MSG_GAME = 0x7600`。Login、ticket、proof 和 Gateway 认证继续使用 GSR 入口契约，不复刻旧项目的登录协议。
- Legacy codec 以固定 golden bytes、字段宽度、字节序、Header 长度和 suffix offset 验证兼容。
- Legacy BS frame 固定使用 24 字节小端 Header，字段依次为 `Magic uint32`、`Serial uint32`、`Origine uint16`、`Reserve uint16`、`Type uint32`、`Param uint32`、`Length uint32`；首版保持旧 transport 的 8 KiB 单帧硬上限，不增加放大配置。`Length < 24`、`Length > 8192` 或其他无法可靠恢复下一帧边界的错误直接关闭当前 ConnectionGeneration。边界完整但 MessageID 未注册时告警并丢弃当前帧，连接继续；已知 MessageID 的固定区、内层 Header 或 suffix 非法时同样只告警、计量并丢弃当前帧，不进入 Battle、不回复也不关闭连接。每个 suffix 必须满足 `offset >= 所属固定区长度` 且 `offset + size <= frame length`；不得复制旧 helper 将越界 suffix 静默解释为空数据的行为。
- 双向 origin 必须是长度恰好为 24 的独立首帧，校验 `Type=0x600` 和方向对应的 `Origine`：GameLogic 为 107，GameMaster 为 100。Magic、Reserve、Serial 和 Param 不提升为新的认证字段；出站 origin 按参考构造保持其零值。握手前后不得把业务帧与 origin 合并解释，也不得在 origin 校验完成前向 Battle 投递。
- Legacy 入站按三段链路兼容：客户端到 Agent 为 `0x7402 + NHSK Suffix`，Agent 到 GameMaster 补 `GameHeader`，GameMaster 到 GameLogic 再补 `0x8605 GLHeader`。这些 envelope 只提供路由和身份上下文，不映射为 Battle Command。
- Legacy adapter 以外层 `GameInnerId` 作为 BattleID、以外层 `UserId` 作为玩家动作身份。玩家消息的内层 `TGameHeader.UserID` 必须与外层一致；Battle 完成初始化后，内层 MatchID 和 ProductID 也必须与该 Battle 已冻结的值一致。`UserId=0` 只允许参考代码明确使用零值的控制或广播消息，不能用于玩家动作。身份不一致属于边界完整但非法的已知帧，按 D-049 丢弃当前帧、保持连接。上述比较只存在于 Legacy adapter：校验成功后立即丢弃冗余 envelope，向 Host/Battle 只传递规范化的 BattleID、UserID 和类型化 Command；Battle 不保存或重复比较多套 Legacy 身份。
- Legacy 出站从 GameLogic 到 GameMaster 为 `0x8644 GLHeader + BSREQ_GS2GC_RELAY_HEADER + NHSK Suffix`。其中 `BSREQ_GS2GC_RELAY_HEADER` 依次包含 `BSHeader(Type=0x7400)`、`TGameHeader` 和 `BSSUFFIXIDX SuffixMsg`；GameMaster 将该 Relay header 与 Suffix 发给 Agent。`0x7402` 仅用于反方向客户端请求。`0x8644` 只承载异步业务输出，不代表 GSR `Reply`；Agent 到客户端的最终裁剪继续以对应参考代码冻结。
- Legacy codec 必须一次性写出最终长度：外层 `HeaderLen=34`，外层 Header.Length 等于完整 frame，内层 Header.Length 等于去掉 34 字节外壳后的内层 packet 长度，且不接受尾部多余字节。旧 formatter 先留下外层 `Length=0`、再由 connection `PackDetect` 补写的行为只是实现中间态，不是允许在线上传输零长度 Header 的 wire 契约。
- 新 GameLogic 不直接依赖任何参考 module。Phase 14 只在 `examples/nhsk/internal/legacywire` 实现当前保留消息所需的最小 Header、常量和 codec；由 `baison_middle/protocol v1.0.5`、上层 `protocol` 与真实参考 formatter 生成并人工核对 `.bin` golden fixture。生产业务 Command、Battle 和输出模型不得导入旧协议类型。
- Magic、Reserve、Serial 和 Param 不进入玩法 Command。出站 Magic、Reserve 保持零；异步 GameOutput 的 Serial 为零；Param 只在具体保留消息的旧契约明确使用时编码。Legacy bridge 不建立通用 Serial request/reply 关联，也不把异步生命周期 ACK 或 `0x8644` 输出解释为 GSR Reply。
- `0x7701` 和 `0x7702` 都使用 `Send`。`0x7601`–`0x7612` 是异步业务输出；`0x7609` 只表达人工出牌拒绝，参考实现没有成功 `Reply`。
- TCP adapter 只做 frame/codec、身份绑定和 Command 映射。Battle、Player、Agent 不接收原始字节，也不判断调用来自 TCP 还是集群 `Send`/`Call`。
- 同一 Command 的业务语义、校验和状态变化只能实现一次。TCP 错误包和 `Reply` encoder 只能转换结果，不能复制规则。

详细工作底稿见 [`2026-07-27 棋牌游戏生产脚手架与示例计划`](../superpowers/plans/2026-07-27-gsr-card-game-scaffold.md)。

## Commit 建议

提交信息尽量中文：

```text
feat(runtime): 创建 Service 生命周期
feat(runtime): 实现 Mailbox 和 Send
feat(runtime): 增加 Call Reply Session
feat(runtime): 增加 Scheduler
feat(runtime): 增加 Timer Command 管道
feat(cluster): 增加远程 Service Transport
feat(cluster): 增加集群控制面服务
feat(cluster): 增加 ServiceGroup 路由策略
feat(runtime): 增加 Drain 访问者追踪
feat(debug): 增加 Command 录制回放
feat(game): 增加 PlayerModule 业务组合
feat(game): 实现打地鼠 Battle 示例
```
