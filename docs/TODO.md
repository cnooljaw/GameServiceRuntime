# GSR 待办列表

> 更新时间：2026-07-27
>
> 作用：记录已实现里程碑的工程欠账和收口项；尚未开始的新能力仍以 `RFC-0500` 为准。

## 当前结论

**Core Runtime** 已经完成 `RFC-0100` 至 `RFC-0192` 的功能实现，覆盖：

- Service、ServiceRef、ServiceName、Registry 和单一 `Handle` Command 分发入口。
- Mailbox、ReadyQueue、固定执行许可池和串行 Handler。
- Send、Call、Reply、Session、PendingCall 和同步调用环检测。
- Timer 到 Command 的统一投递链路。
- Stop、Close、超时、panic 隔离、任务追踪和基础指标。
- 本地端到端示例及并发、生命周期一致性测试。
- Cluster Router、WireEnvelope、远程 Send/Call/Reply 和跨节点调用环检测。
- TCP 握手、受限长度帧、连接复用、断线通知及双节点端到端示例。

当前状态定义为：**Core Runtime 功能闭环，并发布首个 major 0 基线 `v0.1.0`**。Runtime Tooling 和业务模板属于后续里程碑，不计入 Core 未完成项。

Core API 冻结前的 P1 收口已于 2026-07-17 完成。

2026-07-24 已完成 Phase 10C2B：RecoveryOperation 复用终态 StopOperation 的 RequestID，组合根有界 Blueprint Runner 在目标节点创建新实例，NodeAgent 保存 receipt，Coordinator 只在人工 Confirm 后以 Directory CAS 保留当前成员并追加新 Ref；本地与双节点 TCP 均已验收，旧 Ref 永不恢复。同日完成 Phase 11：`tooling/record` 在 Handle 边界以有界 RecorderService 保存版本化输入，JSONArchive 仅在调用方目录中执行显式 I/O，Replay 强制经 TargetFactory 的隔离目标发送；Timer Command、normal/strict 失败、Codec 和旧 Runtime 隔离均已验收。同日完成 Phase 12–13：`game` 交付 Battle/Timeline、Room、Player/Module 和 Wallet/LedgerRunner 模板，WhackMole 覆盖单次命中、Timeline 及隔离重放。MemoryLedger 仅是测试/示例账本；生产持久化、认证、Controller、Reconcile 与 Desired State 仍不在本轮范围。

Cluster 前的 P2 工程门禁、Phase 5 Cluster Data Plane、Phase 6 Core Runtime 验证和 Phase 7A Runtime Inspection 已于 2026-07-17 完成。Phase 7B 最小 Discovery、Phase 7C 本地 Monitor、Phase 7D Snapshot、Phase 7E Supervisor 和 Phase 7F 客户端入口已于 2026-07-22 完成并通过门禁；客户端入口提供单进程内存 SessionRegistry、SingleSession ticket、固定 proof 线格式、TCP Login/Gateway Adapter 和 ProtocolMapper seam；生产 Handshake、持久化或跨节点会话不在本阶段。Phase 8 已于 2026-07-23 完成 NodeAgent 自动 Discovery Heartbeat、静态 NodeConfig、只读 Observed State、可组合 Codec 与双节点验收。Phase 9 已于 2026-07-23 完成独立 DirectoryService、AuthorityEpoch/Revision ServiceSet、Watch lease、Hash/RoundRobin/Broadcast、显式快照 Router、可组合 Codec 与双节点验收。Phase 10A 已于 2026-07-23 完成 VisitorRegistryService 的 lease、代际、过期清理、Codec 和双节点验收。Phase 10B 已于 2026-07-23 完成不可逆 Drain Guard：受信任 Controller 在旧实例的 Mailbox 内开始 Drain，显式外部 Command 被串行拒绝，内部清理 Command 仍可执行；它有独立 Client、可组合 Codec 和双节点验收。Phase 10C1 已于 2026-07-23 完成 Gateway+Principal 授权、RequestID 幂等、有界审计、ServiceSet CAS、Publish/Guard 未知结果的人工 Resolve、Visitor 强 lease 等待和不执行 Stop 的 ReadyToStop；本地与双节点 TCP 验收均已覆盖。Phase 10C2A 已于 2026-07-23 完成独立 StopOperation、Coordinator 与 Runner 的 Directory 双重再确认、精确 Coordinator NodeAgent receipt、组合根有界 Runner、显式 Resolve 以及本地/双节点 TCP 验收；它不创建新实例、不自动补偿或恢复。下一阶段只应冻结人工恢复与补偿（10C2B），随后才讨论 Controller、Reconcile 与受控 Desired State。

## P1：Core API 冻结前完成

| 编号 | 状态 | 事项 | 完成标准 |
|---|---|---|---|
| CF-001 | 已完成 | 收敛 Runtime 重复关闭语义 | 并发或重复调用 `Runtime.Close` 时复用同一次关闭流程，并返回一致结果；补充成功、超时和调用方取消测试。同步裁决重复 `Runtime.Stop` 是等待已有结果还是稳定返回已关闭错误。 |
| CF-002 | 已完成 | 明确 Init 的取消和超时边界 | 先更新 `RFC-0180`，明确保留无 context 的短初始化约束，或在 API 冻结前引入可取消 Init；覆盖 Init 永久阻塞时 Runtime 按期返回、任务保持可观测，以及 Init 后续退出时任务记录被回收。 |
| CF-003 | 已完成 | 补齐 Timer 投递失败可观测性 | Timer 到期后因目标关闭、Mailbox 满或 Runtime 关闭而投递失败时，按原因记录指标；预期关闭不得产生无意义错误日志。 |
| CF-004 | 已完成 | 建立 Core 性能与泄漏基线 | 增加完整 Send、Call/Reply、批量 Service、批量 Timer benchmark，记录 `allocs/op` 和吞吐；增加 Runtime 反复创建关闭后的 goroutine、任务、Timer、PendingCall 泄漏测试。优化只依据基准结果，不预先引入 Ring Buffer、对象池或 Timer Wheel。 |

## P2：进入 Cluster 前完成

| 编号 | 状态 | 事项 | 完成标准 |
|---|---|---|---|
| CF-005 | 已完成 | 保护 SessionID 回绕和冲突 | `PendingCall` 分配 Session 时跳过 `0`，不得覆盖仍在等待的 Session；在远程 Reply 接入前补充回绕测试。 |
| CF-006 | 已完成 | 建立持续集成质量门禁 | CI 固定执行 `go test ./...`、`go vet ./...` 和 `go test -race ./...`；示例程序至少执行一次。 |
| CF-007 | 已完成 | 拆分超大一致性测试文件 | 按 Scheduler、Lifecycle、Call、Observability、Command Registry 拆分 `runtime/conformance_test.go`，只移动测试和共享 fixture，不改变行为。 |
| CF-008 | 已完成 | 固化“Service 不创建裸 goroutine”规则 | 在工程检查中检测项目自有 Service 实现中的直接 `go` 语句，或提供等价静态分析；Runtime 内部 goroutine 必须继续经过任务追踪或具有明确 Runtime 所有权。 |

## P3：Tooling 阶段承接

| 编号 | 状态 | 事项 | 完成标准 |
|---|---|---|---|
| CF-009 | 已完成 | 向 Monitor 提供只读运行任务快照 | 通过 `Runtime.Inspect` 暴露任务类型、owner、开始时间和超时标记；不公开可变 task registry，不把 Monitor API 放入 Core Service 接口。 |

| CF-010 | 已完成 | 同步教程和 RFC 状态 | 以实现为准回填 `docs/GSR-Book` 的 Core Runtime 章节；完成 API 冻结评审后，把 `RFC-0100` 至 `RFC-0192` 从“草案”更新为明确的已接受状态。 |
| CF-011 | 已完成 | 发布首个 Core 版本 | P1、P2 清零后整理变更说明，执行全量质量命令并建立首个版本标签；标签前不承诺公开 API 稳定性。 |

## P4：业务模板性能基线

| 编号 | 状态 | 事项 | 完成标准 |
|---|---|---|---|
| BIZ-001 | 待开始 | 建立 Battle 热点与广播基线 | 在固定机器上记录单 Battle 连续 Command、多个 Battle 并行 Command、不同参与者数 Broadcast 的 p50/p95/p99、allocs/op、Mailbox 拒绝和队列等待；以数据决定是否新增只读投影、批处理或 owner 拆分 RFC，不为优化提前放开 Service goroutine。 |

## P5：棋牌游戏生产脚手架与完整示例

本阶段采用渐进替换：先原位替换 GameLogic，再替换 GameMaster，最后替换 Agent。当前只冻结第一步，不要求旧 GameMaster、Agent 或客户端改协议。执行顺序以 [`RFC-0500` 的 Phase 14](rfcs/RFC-0500-Roadmap.md#phase-14棋牌游戏生产脚手架与完整示例) 为准，详细底稿见 [`2026-07-27 棋牌游戏生产脚手架与示例计划`](superpowers/plans/2026-07-27-gsr-card-game-scaffold.md)。

| 编号 | 状态 | 事项 | 完成标准 |
|---|---|---|---|
| CARD-001 | 已确认待实现 | 冻结渐进替换目标 | 第一阶段只替换 GameLogic，保留旧 GameMaster/Agent/客户端和双向 TCP；以后依次替换 GameMaster、Agent。`nhsk`、`gamelogic`、`gamemaster`、`gamecore`、`protocol`、`baison_middle/protocol`、`nbgame_core` 是不修改业务源码、不直接依赖的只读知识来源；允许维护 `.codegraph/` 索引元数据。每个切片完成后必须按参考核对门禁记录一致、遗漏或有意偏差。 |
| CARD-017 | 已完成 | 收敛最小 ServiceRef 与业务版本模型 | `ServiceRef` 保持 `{NodeID, ServiceID}`；ServiceID 在单个 Runtime 进程内单调分配且不复用，节点重启后允许重新编号。断线或重启后丢弃旧 Ref 与名字缓存，长期 Service 重新解析，临时 Service 由业务重建；Core 不增加 RuntimeEpoch、随机 ID 起点或跨重启旧地址防串代保证。已删除 BattleEpoch，未增加通用 BattleRevision；保留 TimelineRevision 及未来玩法拥有的 TurnRevision、VerifyCode、小局身份等窄 fencing，诊断使用 Command Record Sequence。公开契约门禁和节点同号重连重新 query 测试已覆盖。 |
| CARD-018 | 进行中 | 统一 Legacy 与 Cluster 玩法入口 | Legacy TCP 与 Cluster Send/Call 都转换为同一套类型化 Command，进入同一个 Battle Mailbox。当前已完成 relay、GM 控制面、Host Resolve、Battle Mailbox、代际 OutputService、普通/MATCH_STOP 终局和 DEL_GAME 正常停止屏障；只剩隔离状态收口。 |
| CARD-019 | 已确认待实现 | 保持最小 Send/Call 语义 | `Send` 成功只表示进入 Mailbox，需要当前业务结果时使用 `Call`。普通 NHSK 玩法 Command 不增加 RequestID、幂等表、payload 摘要或结果缓存，依靠 Mailbox、状态和领域 Revision 判断有效性。Call 超时不取消已入箱 Command，也不自动重发；调用方查询权威 Snapshot 后决定下一步。Session 只关联 Reply。结算、钱包、受控创建和隔离释放等真实可重试流程按各自契约使用 RequestID、OperationID 或 receipt。Runtime/Transport 错误与稳定业务拒绝分层表达。 |
| CARD-020 | 进行中 | 保持 Legacy 重连并提供只读 Snapshot | 当前已完成 `0x7208/0x720D` 固定布局解码与映射、Reconnect/Scene 的 Offline/托管副作用差异、ClientReady 资格表、`GameInfo -> GameScene -> 当前 AskOutCard` 最小恢复线序，以及请求者出完牌时显示固定对家手牌。仍需补齐完整 Scene 的 GameRecords/亮牌阶段输出和 golden；Cluster `GetBattleSnapshot` 仍是独立纯查询，不改状态、不输出。无效 Legacy 请求保持静默；隔离 Battle 不执行副作用或伪造输出。 |
| CARD-021 | 进行中 | 建立 NHSK 切换范围门禁 | 已建立能力矩阵、Legacy MessageID 矩阵和逐切片参考核对记录；当前已补齐双向 origin、普通 relay、GM 控制 codec、NEW_GAME ACK、代际输出、强制结束 GAME_OVER→NOTICE、ROUND_STAT 空投影、Reconnect/Scene 恢复、ClientReady 资格、对家出完牌/SHOW_CARDS、结算矩阵与 Flag 门禁、基础牌型比较、当前墩抓分与 TurnEnd 线序测试。剩余门禁是 ROUND_STAT 结算时序、带玩家数据的完整 GAME_OVER/综合结算、全规则与断线隔离的代表性整包 golden。 |
| CARD-022 | 进行中 | 隔离自定义牌堆数据源 | 已实现外部 `ProvideCustomDeck` Command、Battle 在 Preparing 接收并按 BattleID/局号 fencing，本地文件/Redis 兼容 bridge、宽松旧 grammar、账号白名单、可配置有界 `CustomDeckRunner`、失败回退和自定义发牌。Redis adapter 仍使用标准库 RESP GET，保持 `game:makecard:<ProductID>` 优先、空值回退 `<GameID>`；后续仍需真实 Redis 联调与完整 Legacy Deal/Replay golden。 |
| CARD-023 | 已完成 | 兼容并隔离本地回放落盘 | 已保留完整 NHSK 回放 XML、名称和 `FuPan/<date>/<hour>` 目录规则。综合结算响应后先广播客户端 GameResult，再冻结文档并进入 `FinalizingReplay`；进程持有的有界 writer 在 Battle 外执行可检查错误的 `MkdirAll/Open/Write/Close`，不增加原子改名、fsync 或自动重试。成功、失败或超时后才产生 ROUND_STAT 与 GameOver；完整 Ref 和小局身份拒绝错配、重复及迟到结果。只有文件系统根目录可配，回放上传继续放弃。DEL_GAME 不等待在途 I/O 的屏障由 CARD-028 收口。 |
| CARD-024 | 进行中 | 隔离 Battle 随机数与时间 | NHSK Battle 已注入独立 PRNG 与 `NHSKClock`；生产 seed 只来自 `crypto/rand`，读取失败则创建失败，不使用全局随机或时间降级；测试可注入固定 seed/fake Clock，普通洗牌、庄家抽取、期限起点、场景剩余时间、Ask→合法动作毫秒数和回放结束时间已只使用 Battle 依赖。诊断材料时间仍待隔离切片；旧回放 XML 不增加字段。 |
| CARD-025 | 进行中 | 归一化实际可达的 NHSK 规则 | Legacy `INIT_GAME` 已解码连续 `BaseRule/GameRule/MatchName` suffix；当前只把 GameRule 的最小机器人出牌次数、比例和单牌换牌数量投影到不可变 `NHSKConfig`，出牌期限保留参考默认并允许直接 Cluster 配置。缺失、空值或坏值保持默认，多余字段忽略。偏置洗牌及其他已放弃能力不建立字段、handler 或占位接口。普通散牌、`NEW_GAME.IsNewbie` 新手调整和自定义牌堆优先级已接入；仍需更完整的期限矩阵和其他独立 provider。 |
| CARD-026 | 已完成 | 建立综合结算单飞状态机 | Legacy `0x8650` 两段 suffix、整包原子门禁、Flag 和失败 Dissolve 均已完成；writer 结果以完整小局身份 fencing，错配、重复、迟到、MATCH_STOP 替换和 DEL_GAME 屏障均不重复输出。 |
| CARD-060 | 已完成 | 统一综合结算双入口 | Legacy BSAck Send 与 Cluster Send/Call 共用同一 CompleteSettlement 矩阵门禁。已删除无 wire 依据的早期 `Scores[4]` fallback；MATCH_STOP 可废止在途结算并跳过外部请求。 |
| CARD-027 | 已确认待实现 | 保持 GM 两阶段推进下一小局 | 第一阶段 Battle 完成结算、回放和 `GAME_OVER` 后只进入 `SubgameFinished`。旧 GM 决定是否继续及展示等待；每次以 `UPDATE_GAME -> PrepareSubgame` 冻结 `GameNum/SubGameNum` 并进入 `Preparing`，再由 `COMMAND START -> StartSubgame` 进入 `Playing`。可选 `START_NEW_GAME -> UpdateRoundContext` 只更新下一小局回放的 SecRoundTotal/SecRoundUsed/RoomInfo，不改变阶段。Legacy 和 Cluster 共用这些 Command，不增加合并 API。结算后的 `COMMAND CONTINUE` 继续兼容 no-op，Cluster 不公开无意义接口。Battle 不自行开始下一局或释放实例。 |
| CARD-029 | 已确认待实现 | 建立 AwaitingInit 与 NHSK 开局门禁 | `NEW_GAME ACK` 只在 Battle Service 创建、Host 绑定完整 Ref 后成功，Battle 仍处于 `AwaitingInit`。INIT 一次性冻结业务配置；相同重复 no-op，冲突告警拒绝但不隔离，INIT 前消息不缓存。`UPDATE_PLAYER` 可重复 upsert；START 前必须有四个不同非零 UserID 和 `0..3` 四个不重复座位。乱序、缺少玩家、旧局号或重复 START 不进入 Playing。 |
| CARD-030 | 已确认待实现 | 原子更新玩家并删除伪路由状态 | INIT 后各存活阶段接受 UPDATE_PLAYER；整批校验后原子 upsert，坏 UserID/座位或批内冲突整批拒绝，省略玩家不删除。Preparing 前后可按真实 GM 路径换座；StartSubgame 至本局完成冻结 UserID/SeatID，局中结构变更整批拒绝但不隔离。CltID 只供回放 Platform，CntID 解码后丢弃，二者不参与 ClientGameOutput 路由。PLAYER_EXIT_GAME 只标记 Exited，后续更新可重新进入。Flag、PlayerFlag、ScoreChangeReason、ScoreChange、ForceExit 只解码兼容；StartSubgame 清空破产/封顶，本局综合结算 ACK 再赋值。 |
| CARD-031 | 已完成 | 复刻 NHSK 核心牌规与结算 | 牌型、发牌、自定义牌堆、逐墩抓分、托管/AI 来源、对家出完和 MATCH_STOP 本地收尾已完成；终局按参考 `100/105/200` 分支计算名次、单双扣、胜组、倍数和结算矩阵。 |
| CARD-032 | 进行中 | 固化操作 fencing 与输出线序 | 已实现 VerifyCode 3/5/7、开局 GameInfo→四份私有 Deal→Ask，以及玩家/托管/超时/AI 共用牌规校验；坏 AI 候选和迟到结果保持当前硬期限，合法动作无成功 ACK，三家过牌后保持 TurnEnd→下一 Ask。仍需代表性整包 golden 收口。 |
| CARD-033 | 已完成 | 保持 NHSK 托管与超时语义 | 每个 TurnRevision 只有一个有效行动期限。本地自动领出最小单张、跟牌直接过；首次普通超时在 TimeoutAutoMove=true 时广播进入托管并立即操作，false 时清除期限等待人工动作。人工动作/重连/场景恢复取消托管；主动托管当前操作人在期限余量大于 100ms 时立即替换为自动动作，否则沿用即将到期 Command。机器人/离线托管可经进程拥有的 AI runner 请求，合法离线 AI 结果若早于 1s 最小延迟则用唯一替换期限等待；6s 硬超时回退本地行动。默认 Local provider 不访问网络，可选 Legacy HTTP provider 精确兼容旧 RobotTran JSON/base64 二进制契约。 |
| CARD-034 | 进行中 | 最小化 BaseRule 归一化 | INIT 独立字段权威；BaseRule 的 `OfflineAutoUsesAI`、`TimeoutAutoMove`、`RobotLevel` 已在 Legacy adapter 投影到不可变 `NHSKConfig`，缺失/坏值沿用 false、true、进程默认 2。其余 GM-owned 或已放弃索引不建字段；GL 结算展示延迟不迁移，不与 GM 等待叠加。原始规则只留脱敏诊断摘要仍待补。 |
| CARD-035 | 已确认待实现 | 兼容宽松 CARD_ACTION 预览 | PreviewCardSelection 只允许当前操作玩家在 WaitingForAction 使用，广播 UserID 与最多 26 张客户端选牌，不修改权威状态。按原行为不校验手牌归属、重复、牌型或压牌，允许空选择；真正 OUT_CARD 继续完整校验。能力默认开启，不实现无效 BaseRule 开关。 |
| CARD-036 | 进行中 | 新手发牌兼容切片 | 已保留 `NEW_GAME.IsNewbie` 并由 Host 传入 Battle；无可用自定义牌堆时，Battle 在普通确定性洗牌后按座位顺序选择首个非 `Automated` 玩家，执行旧 `RandCardListByNewPlayer` 的三张/四张重试，全自动玩家跳过；固定 seed 测试锁定四手牌结果。自定义牌堆可用时优先并绕过新手调整。未引入 Nacos、每座偏牌配置或通用偏洗牌。 |
| CARD-037 | 已确认待实现 | 普通发牌散牌调整 | GameRule 第四项归一化为默认 4 的 SingleCountToSwap，<=0 关闭；仅普通随机发牌执行。固定 seed 锁定旧 SwapSingleCard 的顺序和四家结果，并校验总牌集合与每座数量；不把算法下沉到 Runtime 或通用模板。 |
| CARD-038 | 已完成 | 托管结算认定与负分修正 | 已按合法玩家/托管/普通超时/AI/AI 超时来源累计 MoveCount/AutoCount/MoveMS，真实 Automated 玩家不计托管次数。GameRule 前两项保留 -1、单项/双项和原乘法公式；成功结算按参考把单个托管失败队员的搭档负分转移给托管者，SettlementRequest、客户端 GameResult 与回放 Summary 共用认定。 |
| CARD-039 | 已完成 | DRESS 回放元数据 | Legacy/Cluster UpdatePlayerDress Send 通过 Battle Mailbox 覆盖已有玩家的不透明装扮字符串；空值允许，无 Call/Reply、客户端输出或 gameplay Revision。START 按顺序冻结四座最新值到当前小局，START 后更新只影响下一局。 |
| CARD-040 | 已完成 | 道具成功事实回放 | `0x7218 BROADCAST_USE_PROP` 已精确解码两段 suffix 并映射为 RecordPropUse Send；核对外层 UserID、SenderID 和 Battle 玩家后，仅在 Playing/AwaitingSettlement 原样追加旧 Prop Move。TargetIDs 顺序、重复值和空列表保留；无库存/权限、玩法效果、Reply、输出或 Revision。 |
| CARD-041 | 已确认待实现 | GAME_MSG 内层 allowlist | 仅解码离线、重连、场景、道具广播，以及 0x7402 中 OUT_CARD、CARD_ACTION、USER_STATE_CHANGE；未知 ID 丢弃告警。正常输出统一走 0x8644。不得实现未工作的 0x7200 输入、0x8655 输出、投票或骰子分支。 |
| CARD-042 | 已确认放弃 | PLAYER_LIMIT 与空玩家信息入口 | 不实现错误嵌套且旧 GL 实际忽略的 PLAYER_LIMIT，也不迁移无调用点的 OnMsgUpdatePlayerInfo。旧 GM 输入按未知 allowlist ID 丢弃告警；未来限制能力由 GM 重新定义。 |
| CARD-043 | 已完成 | 小局生命周期输出兼容 | 开局严格输出 GAME_START→GAME_STARTED→GameInfo→四份私有 Deal→Ask；普通和 MATCH_STOP 终局均在客户端结果和回放后发送空 ROUND_STAT 与四座 GAME_OVER，MATCH_STOP 再发送 NOTICE_ROUND_OVER。不实现无调用点的 GAME_END。 |
| CARD-061 | 已确认待实现 | 删除回放名累计与无用 Reason 状态 | 每小局只冻结当前 ReplayName，供 GAME_STARTED、文件和同局 GAME_OVER 共用；下一局替换，不建立 replayNames 列表或历史拼接。Legacy GAME_OVER 继续编码既定 Reason，但不进入额外领域状态或控制流；不替旧 GM 补偿整场回放索引。 |
| CARD-062 | 已确认待实现 | 统一玩法投递资格并收紧 START | 所有 ClientGameOutput 只过滤不存在或 Exited 的目标，忽略 ClientReady，不暴露 force/non-force 两套领域 API；ROUND_STAT 单独要求 ClientReady。START 除四座完整外要求四人均非 Exited，异常稳定拒绝且可由 UPDATE_PLAYER 修正后重试。 |
| CARD-063 | 已确认待实现 | 逐用户展开 Legacy ClientGameOutput | Battle 冻结按座位排序的目标列表和单份 payload；Legacy adapter 为每个目标编码独立 `0x8644 + 0x7400`，双层 UserID 相同，GameInnerID/MatchID/ProductID 有值，CntTID/CltTID/Reserved2 为零，不使用 UserID=0 广播。Cluster 由 SessionRegistry 按 UserID 路由。 |
| CARD-064 | 已确认待实现 | 冻结 NHSK CommandID 命名空间 | 公开 Host/Battle/玩法/结算 Command 分配 `0x041001xx..0x041004xx`，runner 结果使用包内 `0x0410f0xx`。Legacy bridge 只用显式 MessageID→CommandID 表，不复用或算术转换旧编号。 |
| CARD-044 | 已完成 | MATCH_STOP 替换在途玩法或结算 | Playing/AwaitingSettlement 收到 MATCH_STOP 时递增 TurnRevision、取消行动并废止 0x8650，按 Success 展示牌、本地倍数结算并跳过外部结算；其他阶段 no-op。线序为 ShowCards→GameResult→回放→ROUND_STAT→GAME_OVER→NOTICE；紧随 DEL_GAME 时屏障只保留此前已提交输出。 |
| CARD-045 | 已确认放弃 | 固定六张牌 TestMode | 不实现所有环境均关闭且无 wire 入口的 test_mode_enabled/TestMode/applyTestModeHands。固定 seed 覆盖随机确定性，完整指定牌局使用测试注入的 CustomDeckProvider；不建立生产测试牌开关或第三种发牌优先级。 |
| CARD-046 | 已确认待实现 | 收紧 NHSKConfig owner 边界 | 删除只加载未消费的 MsDeal/MsContinueDelay/TableMultiplier，以及已放弃的 MsShowCard/MsCommentate/TestMode 和恒真 RecordUserAction。NHSKConfig 只留玩法实际读取值；AI 地址、回放根目录、自定义牌堆数据源/白名单分别归外围 provider/runner 配置，Battle 只接收类型化依赖和结果。无效 replay_enabled 按 CARD-059 删除。 |
| CARD-047 | 已完成 | INIT 单一来源归一化 | Legacy codec 已完整解码固定体与四个 suffix；Battle 只冻结 BattleIdentity、进度上限、Fee/ScoreBase/Denominator、ReplayMetadata、ReplayRuleSnapshot 与 NHSKConfig。默认 Rules 与显式默认值归一化为同一 INIT，整份重复请求幂等，任一冲突字段拒绝。未消费 MatchKey/CreateTime 不进入领域状态。 |
| CARD-048 | 已完成 | 单次冻结小局开始时间 | StartSubgame 只读一次 Battle Clock；同一 SubgameStartedAt 生成 Unix StartTimestamp 来源、旧式 ReplayUID、UTC+8 ReplayName 日期/时间和 FuPan 日期/小时目录。RoundUniCode 原样冻结；不增加碰撞或相互一致性校验。 |
| CARD-049 | 已确认放弃 | NEW_GAME IsNewNacos | codec 为旧 wire/golden 解码该位，但不传入 Host/Battle/Cluster/诊断。旧 GL 实际未把它写入 Round，Nacos 绑定也是空方法，不补造功能。 |
| CARD-050 | 已完成 | START_NEW_GAME 回放上下文 | UpdateRoundContext Send 只覆盖 pending SecRoundTotal/SecRoundUsed/RoomInfo，不改阶段或 Revision。StartSubgame 冻结当前上下文；之后更新只影响下一局。首次默认 0/0/空，不按 Clock 推算 Used。 |
| CARD-051 | 已完成 | 以 GameDescriptor 选择 NHSK 玩法 | 组合根固定 GameID=82/GameName=宁海双扣；Legacy NEW_GAME 只接受 82，其他值在 adapter 边界拒绝并 ACK Res=0。Cluster 通过 NHSK Host 完成玩法选择，不传 GameID。BattleIdentity 不含 GameID；后续 ReplayBuilder 与 AIProvider 复用同一 descriptor。 |
| CARD-052 | 已完成 | 隔离回放规则与文本兼容投影 | ReplayRuleSnapshot 只从 BaseRule 投影 TimeOutOver/VoiceMode/RandomSeatRoundStart/GameNumToRandomSeat，玩法 TimeoutAutoMove 来自不可变 NHSKConfig；完整 Info builder 已实现 SecRoundTotal 分支、稳定 Players/Dress，并只在 MatchName/UserName 边界使用 UTF-8-or-GBK 转换。 |
| CARD-053 | 已完成 | 冻结回放终局与统计树 | 结算提交时由 Battle Clock 单次生成 SubgameEndedAt；根 GameOver 与 Chair0..3、Summary/S0..S4、动作/托管/耗时/牌型/本局统计总计以及 Other/CardDetail 炸弹均已从不可变终局快照生成。Moves 不生成虚构 GameOver，不恢复跨局战绩模块。 |
| CARD-054 | 已完成 | 固化回放 Moves 与序列化 | Battle-owned `ReplayMove` 已接入完整 ReplayBuilder：Deal 单事件/D0..D3、CurrentPoint→OutCard、OutCard→CatchPoint→TurnEnd、MoveMilliseconds、中文牌型和来源均确定性序列化；XML Header、Tab、字典序属性、小写牌值、真实 Count 与边界 `Actor` 值有测试。未实现无调用的 PickCard/Offline/Reconnect/任意 AddMove。 |
| CARD-055 | 已完成 | 冻结不可变 ReplayDocument | Battle 在结果树完成后按 Info→Moves→GameOver→Summary→Dress→Other 固定顺序纯内存序列化，深拷贝终局、名称、路径和 XML bytes；Dress/CardDetail 固定四座，无 PlayersPre。Writer 只接收不可变 Artifact，不接收 builder 或 Battle 指针。 |
| CARD-056 | 已完成 | 按来源编码回放文本 | MatchName/UserName 使用隔离的 UTF-8-or-GBK helper，GameName 来自 descriptor；RoomInfo 空值省略、非空原样写 Json，Dress/RoomInfo/PropID 不全局转码，XML 使用标准转义。Prop TargetIDs 按输入顺序输出且保留重复。 |
| CARD-057 | 已完成 | 冻结每局玩家与发牌快照 | `ReplayDocument` 在 StartSubgame 深拷贝 Seat/User/NickName/InitScore/CltID(Platform)/Dress/Automated、RoundContext、最终四手牌和起始时间；客户端私有 Deal 与回放 Deal 复用同一最终手牌，START 后 UPDATE_PLAYER 只影响下一局。终局再深拷贝 Exp、Flag、结果、统计和炸弹明细；CntID 不进回放。 |
| CARD-058 | 已完成 | 分离 ReplayName 与 ReplayUID | START 分别冻结 NHSK ReplayName 与 Unix秒+CreatorID ReplayUID；GAME_STARTED、文件和 GAME_OVER 复用名称，完整 XML Info 与客户端 FuPanUID[64] 复用 UID。两者不互相推导，不增加防碰撞机制。 |
| CARD-059 | 已完成 | 删除无效回放开关 | 未实现 replay_enabled/Enabled 或 Noop writer；生产组合根始终装配有界 FileReplayWriter，测试可注入 ReplaySubmitter。只有根目录可配，FuPan 子目录和 NHSK 前缀固定；单局失败不熔断，下一局继续尝试。 |
| CARD-028 | 已完成 | 以 DEL_GAME 停止屏障回收正常 Battle | 正常 Battle 的 Host 精确 Ref 先进入 Stopping；固定 runner Call 同一 Battle 的 Mailbox 屏障与诊断快照，屏障递增 TurnRevision、清除 deadline、禁止输出并 fence迟到结果，再 Runtime Stop。成功后才删除绑定；Stop timeout 转入 Quarantined。隔离条目的 DEL_GAME 只累计外部结束观察，不 Stop 或释放。 |
| CARD-012 | 进行中 | 原位替换 GameLogic | 已实现 NHSK 进程组合根、单条主动 GM 连接、完整主要控制/玩法 wire、Host/Battle/GameOutput、核心牌规、托管/AI、综合结算、回放、终局、停止屏障和隔离诊断。剩余代表性连接整包 golden、100,000 次 churn 与真实旧 GM 联调门禁完成后才能声明原位替换验收。 |
| CARD-013 | 已完成 | 隔离并保留 Battle 缺陷现场 | Battle 边界在委派前复制完整输入、成功后更新最后稳定 Snapshot；Handler/Timer panic 和不变量失败先上报证据再由 Core 注销实例，Stop timeout 使用删除前捕获转入隔离。Host 保留编号/Ref/容量，GM 断线、同号创建和 DEL_GAME 都不能覆盖；DEL_GAME 只记录首末时间、次数和代际。固定 runner 补入 Runtime Inspection，本地 exporter 以 fsync+rename 发布六类材料和 SHA-256 receipt；队列满/失败可通过节点本地 Unix CLI 重试。只有精确 receipt 可释放，材料清理由独立显式操作完成。健康状态可报告 Degraded。Core 不抢占仍未返回的 Go Handler；SlowCommandThreshold/Inspect 负责观测，避免引入无法安全停止的 watchdog goroutine。 |
| CARD-014 | 已完成 | 每个出牌机会只保留一个行动期限 | 每个 `TurnRevision` 只有一个有效 `ActionDeadline`。普通、托管/机器人和外部 AI 使用各自已确认期限；离线 AI 早到时以剩余最小延迟替换硬期限。旧 Revision 无副作用，Timer Command panic 由统一 Battle 隔离边界处理。当前 GM 不发送 PAUSE，首版未实现暂停。 |
| CARD-015 | 已完成 | 隔离 Legacy 外部 AI | 固定 `AIRunner` 装配默认 Local provider 与可选 Legacy HTTP RobotTran adapter；请求冻结完整 Ref/Seat/Turn/VerifyCode/起点并深拷贝隐藏信息，响应候选回到同一 Mailbox 复核。HTTP/队列/格式/候选失败不隔离且不改变硬期限，日志不包含请求响应或手牌。 |
| CARD-016 | 已完成 | 异步收敛 Battle 创建 | Host 先保存 `Creating` 再非阻塞提交固定有界 lifecycle worker；Create/Init、删除屏障和 Runtime Stop 都不在 Service Handler 内执行。结果携带 OperationID/BattleID/完整 Ref/连接代际并回到 Mailbox，Host 只接受当前匹配结果；同请求合并、冲突请求拒绝，断代迟到实例作为孤儿 Stop。非零连接代际只接受本地 Legacy adapter 来源；Legacy 活动同号先完全 Stop 旧 Ref，再以同一 Operation 创建新 Ref，Cluster 和 Quarantined 均不能替换。单 worker、重复请求、来源防伪、断代、孤儿和同号替换 race 测试已覆盖。 |
| CARD-002 | 待规划 | 建立应用组合根与配置脚手架 | 每类进程具有显式配置、启动顺序、健康检查、优雅关闭和依赖装配；Runtime、Adapter、外部 worker 的生命周期 owner 清晰，关闭会等待真实返回。 |
| CARD-003 | 待规划 | 建立结构化日志和关联字段 | 统一日志字段、级别、脱敏和输出 seam；登录、认证、连接、Agent、牌桌、结算可按 `RequestID`、玩家、会话和对局关联，token、secret、proof 与第三方凭据不进入日志。 |
| CARD-004 | 待规划 | 提供 MySQL 工具模块 | 首版只提供配置、连接池、健康检查、超时、关闭和真实 MySQL 集成测试，不定义业务 schema，不接管 Service 内存中的权威状态。 |
| CARD-005 | 待规划 | 提供 Redis 工具模块 | 首版只提供配置、连接、健康检查、超时、关闭和真实 Redis 集成测试，不定义业务 key 或 SessionRegistry，不成为示例启动依赖。 |
| CARD-006 | 待规划 | 扩展第三方认证 | 定义稳定 `AuthProvider` seam、错误分类和凭据脱敏；实现微信 provider 与显式开发配置启用的 `account + shared token` provider，不提供注册流程；以 fake provider 验证其他平台无需修改 Login/Gateway/Core。 |
| CARD-007 | 待规划 | 冻结并实现 Agent 边界 | 每个已登录玩家会话对应一个 Agent；Gateway 拥有 socket，Agent 拥有会话路由和重连窗口，PlayerService 拥有玩家长期状态，BattleService 拥有牌局状态。无牌局离线保留 2 分钟；牌局中保留到终局后 2 分钟，绝对上限 10 分钟；新会话代际替换旧连接。 |
| CARD-011 | 待规划 | 兼容 Command 与 Legacy TCP 调用 | 入站分层兼容客户端 `0x7402 + Suffix`、Agent 补 `GameHeader`、GameMaster 再补 `0x8605 GLHeader`；外层 GameInnerId/UserId 是权威路由身份，内层重复身份只在 Legacy adapter 核对，随后丢弃并转成规范化 Command，Battle 不感知多层 envelope。出站兼容 GameLogic 的 `0x8644 GLHeader + BSREQ_GS2GC_RELAY_HEADER + Suffix`，GameMaster 将 Relay header 与 Suffix 发给 Agent。新实现不依赖旧 module，只以本地最小 codec 和参考 `.bin` golden 兼容；嵌套长度精确且无尾部字节，不建立通用 Serial RPC。`0x7701`、`0x7702` 均映射为 `Send`，不产生同步 `Reply`。 |
| CARD-008 | 待规划 | 补齐棋牌游戏通用模板 | 根据首个玩法验证 Match、Lobby、Table/Seat、Turn、Ready、托管、断线重连、结算等状态归属；只把至少两个真实玩法共同需要的能力提升为通用模板。 |
| CARD-009 | 待规划 | 交付完整可运行的宁海双扣示例 | 在独立可部署的 Cluster 节点中跑通微信或开发认证、Gateway 认证、Agent 建立、4 人入桌、发牌、出牌/过牌、抓分、单扣/双扣结算和断线重连；首版权威状态保存在 Service 内存，不承诺进程崩溃恢复。行为知识来自只读的 `nhsk`，容器知识来自 `gamelogic`/`gamecore`，外围协议知识来自 `protocol`。 |
| CARD-010 | 待规划 | 建立本地依赖与端到端门禁 | 提供可重复启动的 MySQL/Redis 测试环境、schema migration、seed、健康检查和端到端命令；全量 `go test`、`go vet`、race、泄漏与故障场景通过。 |

## 验收命令

每次关闭待办前至少执行：

```bash
go test ./...
go vet ./...
go test -race ./...
go run ./examples/local-runtime
go run ./examples/cluster-runtime
go run ./examples/discovery-runtime
go run ./examples/monitor-runtime
go run ./examples/snapshot-runtime
go run ./examples/supervisor-runtime
go run ./examples/servicegroup-runtime
```
