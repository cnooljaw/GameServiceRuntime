# GSR 棋牌游戏生产脚手架与完整示例实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` task-by-task. 本文当前是需求澄清版；只有对应决策门关闭、RFC 被接受后，才可执行该纵向切片。

**Goal:** 先用 GSR 原位替换旧 GameLogic，在旧 GameMaster、Agent 和客户端不变的前提下跑通宁海双扣；随后按 GameMaster、Agent 的顺序继续替换，并逐步补齐生产脚手架。

**Architecture:** 第一阶段的新 Game 进程保持旧 GameLogic 的 TCP 入站和出站线格式，Legacy adapter 只负责 frame、codec、连接和 Command 映射。旧 GM 的数值 `GameInnerId` 直接作为 `BattleID`；`NHSKHostService` 持有 `BattleID -> BattleRef` 和生命周期，接受调用方提供的编号，并返回新建实例的 Ref。BattleID 只在活动集合内唯一，实例完全结束后可回收复用；`ServiceRef{NodeID, ServiceID}` 在当前节点进程内定位运行实例。节点断线或重启后旧 Ref 整体失效，长期名字重新解析，临时业务关系重新建立。Battle 不增加通用 Revision，迟到消息使用 TurnRevision、TimelineRevision、VerifyCode 和小局身份等窄 fencing。Cluster 调用方按 `BattleID` 解析具体 `BattleRef` 后直接 `Send`/`Call`。`BattleService` Mailbox 持有牌局权威状态，两条入口最终进入同一批玩法 Handler。

**Tech Stack:** Go 1.23.3、标准库、GSR Runtime/Game、Legacy TCP binary codec、TDD、Race Detector。MySQL、Redis、微信认证属于后续脚手架，不阻塞 GameLogic 替换；引入客户端库前需在 RFC 中记录版本和必要性。

---

## 0. 当前迁移目标

当前只替换旧 GameLogic。旧 GameMaster、旧 Agent 和旧客户端继续运行，且不需要理解 GSR Command。

迁移顺序已经确认：

```text
第一步：替换 GameLogic
  -> 第二步：替换 GameMaster
  -> 第三步：替换 Agent
  -> 后续：替换更靠近客户端的入口并接入 Auth/Login/Gateway
```

第一步完成时必须同时成立：

- 新 Game 进程可以替代旧 GameLogic 的部署地址或注册身份。
- GameMaster 发来的原 TCP 二进制流可以原样进入，不要求 GameMaster 改协议。
- 新进程发给 GameMaster 的 TCP 二进制流与旧 GameLogic 保持兼容。
- 同一业务能力同时公开类型化 Command；现阶段旧 TCP 映射为 `Send`，新查询能力可用 `Call`。
- Legacy TCP 与直接 `Send`/`Call` 不得维护两套宁海双扣状态机、校验或结算逻辑。
- GameMaster 与 GameLogic 一一对应；切换时新旧 GameLogic 不同时接入。连接断开后 GameMaster 结束该连接上的 Round；失败回切只恢复后续新牌局的服务，不迁移或恢复已结束的内存牌局。

### 参考实现核对门禁

宁海双扣示例必须按 GSR 的 Service、Command、Mailbox、Timer、生命周期和 adapter 边界实现。旧目录只提供行为知识，不决定新代码结构。

每完成一个可验证功能切片，都要更新 `docs/reviews/nhsk-reference-reconciliation.md`：

1. 列出本切片对应的参考入口、MessageID、状态变化、Timer、输出和生命周期路径。
2. 对每项标记 `已一致`、`有意偏差` 或 `发现遗漏`。
3. `有意偏差` 必须链接批准该差异的 RFC 和决策编号，并写明不照搬旧实现的原因。
4. `发现遗漏` 会阻止切片完成；先更新 RFC、测试和计划，再补实现。
5. 使用 CodeGraph 定位，再以参考源码和测试复核。不得修改参考目录的原业务代码、配置或资源；允许写入 `.codegraph/` 等分析元数据。

该记录只保存核对证据，不定义第二份业务契约；行为仍以 RFC 和 golden tests 为准。
Task 2、Task 3、Task 8 以及后续任何新增的 NHSK 功能 Task，都必须在各自提交前完成本门禁，不能只在 Phase 末统一补记。

## 0.1 进程内边界

```text
旧 GameMaster
  <-> LegacyGMConnection（GameLogic 主动连接；单条全双工 TCP）
      -> LegacyIngressCodec（0x86xx frame -> Command）
  -> LegacyBridgeService（GameInnerId == BattleID -> 当前 BattleRef）
  -> BattleService（唯一牌局 Mailbox）
  -> NHSKBattleLogic（规则、状态变化、类型化输出）
  -> GameOutputService（输出顺序和目标）
      -> LegacyEgressCodec（GameOutput -> 0x864x frame）
  -> LegacyGMConnection

未来新 GameMaster
  -> ResolveRemote(".nhsk-game-host")
  -> ResolveBattle(BattleID) -> BattleRef
  -> 直接 Send/Call BattleService
  <- 类型化 GameOutput
```

| 模块 | 负责 | 不负责 |
|---|---|---|
| `LegacyGMConnection` | 启动时主动连接旧 GameMaster；持有单条连接、连接代际、reader、单 writer、有界 FIFO、关闭和真实返回等待 | 牌局状态、规则、跨连接重放旧输出 |
| `ConnectionFailureReporter` | 非阻塞接收连接代际和稳定失败类别；只关闭仍匹配的当前代际，重复或旧代际报告幂等忽略 | Battle 状态、重连、输出缓存、Core 错误策略 |
| `LegacyIngressCodec` | frame、长度校验、`0x8605` 等消息解码、转换为类型化 Command | 连接重连、牌局状态、规则 |
| `NHSKHostService` | `.nhsk-game-host` 稳定入口、`BattleID -> BattleRef`、创建请求和 Battle 生命周期；接受调用方提供的 ID，供 Cluster 调用方解析当前 Ref | 生成 `BattleID`、手牌、轮次、牌型、TCP 连接、代理所有 Cluster 玩法调用 |
| `LegacyBridgeService` | 校验 `GameInnerId` 范围并直接作为 `BattleID`；解析当前 Battle，把旧 GM 输入映射到目标 Battle，把 Battle 输出映射回旧 GM | 字符串转换、第二套 ID 绑定、通用 Battle 索引、NHSK 规则、socket I/O |
| `BattleService` | Mailbox 串行、Timer Command、Snapshot、玩法 Command 分发 | 原始字节、`GLHeader`、socket |
| `NHSKBattleLogic` | 宁海双扣规则、状态变化、稳定错误；成功时返回当前 Command 的不可变 `GameOutput` 批次 | `0x8605`/`0x8644` 封包、网络重连、直接 Send/Broadcast 客户端玩法输出 |
| `GameOutputService` | 每个 GM 连接代际一个；该代际所有 Battle 共用。接收已提交 Revision 对应的完整输出批次，以单 Mailbox 保持批次和连接线序，再投递到该代际的有界 sink | 编码玩法规则、回滚已提交 Battle 状态、直接写 socket、跨连接代际重放 |
| `LegacyEgressCodec` | 把类型化输出编码为 `0x8644` 等 Legacy frame | 修改 Battle 状态、直接写 socket |

`BattleID` 所有权规则：

- 类型冻结为 `uint32`；`0` 无效。协调者编号池默认范围为 `1..10000`，上限通过应用配置调整。
- 创建调用方从有限编号空间分配 `BattleID`。第一阶段直接使用旧 GM 分配的 `uint32 GameInnerId`；Legacy mapper 只做范围校验，不转换表示。
- Host 接受 `BattleID`，创建成功后返回 `BattleRef`。Host 不生成业务 ID。
- `BattleID` 只在同时活动的 Battle 集合中唯一。Battle 完全结束并从 Host 删除后，协调者可以回收该编号。
- 每局创建新的 BattleService 和完整 `BattleRef`；结束后通过组合根的生命周期 runner 调用 Runtime Stop。相同编号的新 Battle 仍使用新的 ServiceRef。旧 Ref 的迟到创建、停止、Timer、输出或玩法消息不得作用于新实例。
- 创建和停止都通过同一个组合根 `BattleLifecycleRunner` 异步收敛。Host Handler 只校验、保存 `Creating`/`Stopping` 操作并执行非阻塞 Submit；Runner 执行 `Runtime.CreateService/Stop`，以带 OperationID、BattleID、完整 ServiceRef 和连接代际的 Command 返回结果。创建成功但结果无法交回 Host 时，Runner Stop 孤儿 Service。Cluster 调用方通过异步结果或操作查询取得最终 Ref，不在 Service Handler 内等待 Runtime。
- `Creating` 期间的 Legacy 重入按连接代际、BattleID 和规范化 payload 指纹判定：相同 payload 合并到当前 Operation，不重复 Submit，最终只产生一次 ACK；不同 payload 记录脱敏摘要并立即 `Res=0`，当前创建继续，不取消、不并行创建、不排队替换。该规则不扩大 Legacy 强制替换：只有条目已经 `Active` 才执行停止旧实例后重建；Cluster 在 `Creating`/`Active` 一律 `ErrBattleIDInUse`。
- 后续新 GSR GameMaster 分配空闲 `BattleID`，调用 Host 创建 Battle，保存 Host 返回的 `BattleRef`，并在结束确认后回收编号。
- Phase 14 实现前，先以失败测试保护无效零值、Map 索引、排序、Snapshot 和 Timeline fence，再把通用 `game.BattleID` 从当前 `string` 修改为已冻结的 `uint32`；不为 Legacy 兼容增加十进制字符串转换。
- 旧 GM 使用 `0` 表示未绑定游戏，`RoundMaxInnner` 常量为 `10000`，删除 Round 时移除 `roundsInner` 索引。其 `AddRound` 回绕判断误用了尚未赋值的 `curRound.InnerId`，不能作为新分配器实现。后续新 GameMaster 的编号池必须跳过活动编号；编号耗尽时明确拒绝创建，不能覆盖仍在运行的 Battle。
- Host 的普通创建请求遇到活动编号时返回 `ErrBattleIDInUse`。旧 GameLogic 会对异常同号 `NewGame` 执行 `ClearRound + NewRound`；为保持第一阶段可观察兼容，Legacy bridge 只对 `Active` 条目编排“告警、停止旧实例、等待生命周期结果、以新完整 Ref 创建同号实例”。`Quarantined` 是明确例外：记录告警并回复 `Res=0`，不能停止或替换。不要给 `CreateBattleRequest` 增加通用 `ReplaceExisting` 或 `CollisionPolicy`，也不要让 Cluster 调用者获得覆盖活动 Battle 的入口。
- Host 配置 `MaxActiveBattles`，达到上限时拒绝创建并保持现有实例不变。第一阶段不预创建固定 BattleService 池，也不复用 ServiceRef；`ServiceID` 是单个 Runtime 进程内单调且不复用的 `uint64` 运行实例号，而非内存地址，Runtime Stop 后实例从 Registry 删除。只有创建/停止基准和长时间 churn 测试证明 Runtime 生命周期成为瓶颈时，才另写 RFC 评估固定槽位 Service。
- 正常结束与代码缺陷分流：普通 Handler 或 Timer Handler 的 panic、不变量失败和 Handler/Stop 超时只把对应条目置为 `Quarantined`。Timer Handler 不复制参考 `OnTimer` 只 recover、记录日志并返回的行为。GM 断线使尚未隔离的 Battle 进入生命周期 runner，但不覆盖隔离状态、不自动 Stop 或释放；新连接不能覆盖同号条目。隔离条目收到 `DEL_GAME` 时只幂等记录外部已结束事实：首次观察时间来自注入 Clock 且之后不可变，重复消息更新最近观察时间、观察次数和本次 ConnectionGeneration；收到同号 `NewGame` 时回复 `Res=0`。二者都不能 Stop、替换或释放，这是相对参考实现的有意偏差。Host 只拒绝该 Battle 的后续业务路由和同号创建；其他 Battle 继续运行，不断 GM，不整体回切。节点报告 `Degraded`，但继续接受不同编号的 `NewGame`，直到 `Creating + Active + Stopping + Quarantined` 达到 `MaxActiveBattles`。
- 诊断 seam 保存最后稳定 Snapshot、输入 Record、随机种子、Clock、失败 Command 元数据、Timeline ID/Revision、panic stack、连接代际和 Runtime Inspection。可变 Battle 对象不是可靠诊断档案；Runtime panic 后实例可能已被隔离。组合根的有界诊断 runner 负责导出，不在 Handler 内写文件。
- 首版诊断材料由组合根持有的本地文件 exporter 写入可配置根目录；Core Runtime、Host 和 Battle Handler 不直接做文件 I/O。每份导出先进入独立临时目录，至少写出 `manifest.json`、`snapshot.json`、`commands.jsonl`、`panic.txt` 和 `runtime-inspection.json`，全部写完后依次同步文件与临时目录、原子改名为 receipt 目录并同步父目录。只有所有步骤成功才生成不透明 receipt，并把它绑定到 `BattleID`、完整 `BattleRef` 和材料摘要。receipt 只表示“该实例现场已经可靠保存”，不自动 Stop 或释放。运维人员拿到材料后显式调用 `ReleaseQuarantinedBattle`；Host 精确校验三项，实例仍存在时先由生命周期 runner Stop，再只释放该条目。写入、同步或改名失败不能产生 receipt，复用编号后的新实例也不能接受旧 receipt。首版不依赖 MySQL、Redis 或对象存储；后续 exporter 不改变 Host 契约。代码修复走正常发布，不因单 Battle 缺陷回切整个节点。
- 首次进入 `Quarantined` 自动向有界诊断 runner 非阻塞提交导出。队列满时为 `ExportPending`；有限自动重试仍失败时为 `ExportFailed`；成功原子发布并签发 receipt 后为 `Exported`。所有状态都继续隔离并占用容量，失败不反向阻塞 Host，节点本地运维可以显式重试。释放 Battle 不删除诊断目录；材料删除是单独的人工操作。
- 首版只提供节点本地管理 CLI 执行 `ListQuarantined`、`Export/Retry`、携 receipt 的 `ReleaseQuarantinedBattle` 和诊断目录清理。CLI 通过进程内窄管理 adapter 驱动 Host/runner，不增加 Legacy MessageID，不复用 GM TCP，也不注册普通 Cluster Service API；远程管理留待运维身份认证与授权契约。
- Host 不维护通用的最长牌局时间、最后进展时间或停滞扫描。每个玩法在自己的规则配置中声明出牌、托管、结算和结束等待时间，并用 Battle Timeline 投递绑定完整 ServiceRef、带 Timeline Revision 的 Command。运行时间长本身不是故障；只有 Timeline 投递失败或到期处理产生明确技术错误时才进入诊断流程。

旧参考实现与现网说明共同确认 GameMaster 主链路只有一条连接：

- GameLogic 启动时读取 `connections.gameMaster`，以 `BS_CONNECTIONTYPE_GAME_LOGIC` 主动连接 GameMaster。
- `GameMasterController` 挂在这个 client connection 上，从同一连接接收 `0x86xx`。
- `GameMasterService` 的 `TcpPusher` 使用同一个 connection 发送 `0x864x`。
- GameMaster 的 TCP server 根据连接 origin `BS_CONNECTIONTYPE_GAME_LOGIC` 创建 `GameController`；`OnConnected` 将同一 connection 按远端地址登记到 `GameLogicServer`。
- GameMaster 创建牌局后把这个 GL 地址保存进 Round/Game，后续 GM2GL 消息通过 `PushByAddr` 写回原 connection。
- 旧工程的通用 TCP server 不是 GameMaster 主链路；第一阶段不因它存在而复制第二条 GM 连接。

新实现由一个 `LegacyGMConnection` owner 管理同一条全双工 TCP。reader 和 writer 可以是连接 adapter 的受控 goroutine，但共享同一代际、取消和关闭入口。

连接来源握手已由 `gamelogic`、`gamemaster` 和 `nbgame_core` 源码确认，但仍需 golden bytes：

- GameLogic 使用 `NewBSProtocol(BS_CONNECTIONTYPE_GAME_LOGIC)` 创建连接协议；构造函数预生成 `BSHeader{Origine: connectionType, Type: BSMsgOrigine, Length: BSHeaderLength}`。
- TCP client 创建后调用 `Protocol.ConnectedEvent`，`BSProtocol.ConnectedEvent` 将预生成的 origin frame 发到连接；因此 GameLogic 中无需再手工发送 `BSMsgOrigine`。
- GameMaster TCP server 默认启用 `CheckOrigin`。未绑定 controller 时，它要求第一帧类型为 `BSMsgOrigine`，再把该帧的 `Origine` 传给 `ClientCreator.CreateClient`；`BS_CONNECTIONTYPE_GAME_LOGIC` 对应 `GameController`。
- GameMaster 接受端先消费首个 origin frame，并据此创建 `GameController`。底层再通过 `AcceptClient -> CreateClient` 包装 accepted socket；这个 `CreateClient` 同样调用 GameMaster `BSProtocol.ConnectedEvent`，向 GameLogic 回发 `BS_CONNECTIONTYPE_GAMESVR` origin。
- 新 adapter 必须在连接建立后、任何业务 frame 前完整写出本方 origin，并读取和校验 GameMaster origin。实现时用 golden test 冻结两端精确字节、字节序和发送时序。

连接状态机冻结为：

```text
Connected（已分配新的 ConnectionGeneration）
  -> OriginVerified（已完整写出本方 origin，并校验 GameMaster origin）
  -> OutputReady（该代际 GameOutputService 创建成功）
  -> Ready（开放业务读取并贡献 readiness）
  -> Closing
  -> Closed
```

每次物理连接都使用新 Generation，即使它最终在握手阶段失败。OriginVerified 到 OutputReady 期间不解码、缓存或投递业务 frame；字节可以留在 TCP 或 buffered reader 中。origin 写入、读取或校验失败，OutputService 创建失败或提前断线直接进入 Closing。断开先禁止新输出并停止该代际 GameOutputService，再关闭连接，最后等待 reader/writer 真实返回。

初次连接失败或断开不会结束 GameLogic 进程。只有连接状态机可以发起 Dial；`Send` 只报告当前代际不可用，不执行连接。状态机同时最多保留一个 Dial/建立流程，并以有上限的指数退避持续重试到进程关闭。应用配置冻结为：

```go
type ConnectionConfig struct {
	DialTimeout       time.Duration
	OriginTimeout     time.Duration
	InitialBackoff    time.Duration
	MaxBackoff        time.Duration
	BackoffMultiplier float64
	JitterRatio       float64
	StableResetAfter  time.Duration
}
```

默认值依次为 5 秒、5 秒、1 秒、30 秒、2、0.2 和 60 秒。每次等待在 `[base*(1-JitterRatio), base*(1+JitterRatio)]` 内取值；`JitterRatio` 必须大于 0 且小于 1。Dial、origin 写入/读取/校验或 OutputService 创建失败，以及 Ready 未满 60 秒即断开，都会推进下一档基础退避；连续 Ready 满 60 秒才把下一次失败后的基础退避重置为 1 秒。关闭必须立即取消 Dial、握手或退避等待。每次成功连接都分配新 Generation。旧代际的 `Creating`、`Active`、`Stopping` Battle 通过生命周期 runner 收敛，不恢复到新代际；`Quarantined` 保持隔离、继续占用容量并阻止同号新建，直到人工局部释放。

断线沿用旧行为，不增加业务补偿：

- 不跨连接代际缓存或重放 `GameOutput`。
- writer 失败后关闭当前连接代际，丢弃该代际尚未发送的 frame，并报告连接不可用。
- GameMaster `OnDisconnected` 遍历所有 `Game.GetGLAddr()` 等于断开地址的 Round 并调用 `RoundOver()`，随后从 `GameLogicServer` 删除 connection。
- 删除后 `PushByAddr` 找不到该地址，只记录警告，不再向断开 connection 推送。
- GameLogic 停止旧代际关联的本地 Battle，不恢复旧回合；后续新连接只承载 GameMaster 新分配的牌局。

## 0.2 Command 设计

不建立一套新的 RPC façade，也不分别实现 `FooSend`、`FooCall`。调用方直接使用 GSR `Send`、`Call`，CommandID、payload 和可选 result 是公开契约：

- 容器级 Command：创建 Game、初始化、更新玩家、开始小局、删除 Game、结算结果确认。
- Battle 控制 Command：离线、重连、场景请求、Timer 到期。
- 宁海双扣 Command：`NHSKOutCard`、`NHSKCardAction` 及后续玩法输入。
- 查询 Command：Battle snapshot、Game 是否存在、当前路由；这些只读 Command 必须使用 `Call` 才能取得结果。

规则：

- 会改变业务状态的旧消息映射为 `Send`；旧协议没有同步 Reply，adapter 不能等待 GSR `Call` 再伪装成旧回包。
- 对允许直接调用者取得当前处理结果的 Command，Handler 只实现一次：完成状态变化、生成异步输出，再 `Reply(CommandResult)`。通过 `Send` 到达时 Reply 无副作用；通过 `Call` 到达时调用方取得同一结果。
- 普通玩法 Command 不因允许 `Call` 就增加 RequestID 或通用幂等缓存。Call 超时表示结果未知且不会取消已经入箱的 Command；调用方先查询权威 Snapshot，再按当前状态决定后续动作，不能自动重发原动作。
- Timer、离线通知和纯业务事件等 Send-only Command 不承诺 result；调用方不得对它们使用 `Call`。
- `Call` 不替代 `GameOutput`。例如直接 `Call(NHSKOutCard)` 可以取得本次校验结果，但出牌广播、下一手询问和结算仍走与 Legacy 相同的异步输出。
- Legacy envelope `0x7402`、`0x8605`、`0x8644` 不分配业务 CommandID。
- adapter 先把字节解成类型化 payload，再投递 Command；直接 Cluster 调用者构造同一 payload。
- 玩法 Handler 只按 CommandID 和 payload 行为，不读取“来源是 TCP 还是 Cluster”的标记。

创建请求不用含糊的通用 `Spec`。宁海双扣切片使用明确的类型化字段，例如：

```go
type CreateBattleRequest struct {
	BattleID     game.BattleID
	Participants []game.Participant
	Rules        NHSKRuleConfig
}
```

第一阶段由 Legacy mapper 从 `NewGame` 组装该请求；`GameInnerId` 直接填写 `BattleID`。后续新 GameMaster 直接构造同一请求。Host 成功创建后返回 `BattleRef`，并保留调用方提供的 `BattleID`。

### 入口识别与回复

Runtime 能识别来源和调用模式，但玩法不以此分叉：

- TCP reader 把 frame 投递给固定的 `LegacyBridgeService`；该 Service 校验数值范围后直接把 `GameInnerId` 作为 `BattleID`，从 Host 解析当前 `BattleRef`，再 `Send` 到 Battle，因此 `CommandContext.Source()` 是 bridge 的 `ServiceRef`。
- Cluster 调用方先通过 `.nhsk-game-host` 解析 `BattleRef`，再直接 `Send`/`Call` Battle；`CommandContext.Source()` 是真实调用 Service 的 `ServiceRef`，Host 不代理该玩法调用。
- 两种入口不设优先级，按进入同一 Battle Mailbox 的顺序串行处理；状态、TurnRevision、VerifyCode 等领域 fencing 决定后到 Command 是否仍有效。第一阶段 Cluster source 视为受信任内部服务，但玩家动作 Command 仍必须携带 `UserID`，Battle 校验其属于本局。
- Runtime Call Session 只关联 Reply。普通 NHSK 出牌、托管、重连和亮牌等玩法 Command 不携带通用 RequestID，也不维护幂等键、payload 摘要或结果缓存；只有结算、钱包、受控创建或释放等真实可重试流程按自己的契约使用 RequestID、OperationID 或 receipt。
- `Send` 成功只代表 Command 已进入 Mailbox，不代表玩法成功；需要当前业务结果的 Cluster 调用者使用 `Call`。Call 超时不会取消已进入 Mailbox 的 Command，也不触发自动重试；调用方先取得权威 Snapshot，再决定下一步。Runtime/Transport 的未送达、超时、断线与稳定业务拒绝分层返回，不把网络失败编码成玩法错误。
- Source 只用于授权、审计和日志。`NHSKBattleLogic` 不允许根据 Source 选择另一套规则或输出。
- GSR 不要求 Handler 查询“这是 Send 还是 Call”。Handler 对有 result 的 Command 统一调用 `BattleContext.Reply(result)`：Call 会返回结果；Send 路径的 `ErrReplyUnavailable` 由 Battle helper 吸收。
- Legacy TCP 不等待 GSR Reply。Cluster Call 只取得类型化处理结果和稳定拒绝原因，不携带通用 BattleRevision 或客户端输出。客户端拒绝、定向回复、广播和结算始终表现为 `GameOutput`，只经同一条 GameMaster TCP 发出。
- `GameOutput` 显式携带目标玩家或广播语义，不能通过 `CommandContext.Source()` 猜测该发给谁。
- GM 连接断开后，尚未隔离的 Battle 进入停止收敛，新的 Cluster Send/Call 返回 stopping 或 not found；隔离 Battle 返回隔离错误。Cluster 入口不能绕过 ConnectionGeneration 把旧牌局恢复或迁移到新连接。

## 0.3 输出设计

玩法不直接生成 `0x8644`：

1. `NHSKBattleLogic` 产生类型化 `GameOutput`，包含 `BattleID`、目标玩家/广播语义、稳定 `OutputKind` 和类型化 payload；它不包含 Legacy `GameInnerID`，也不填写 Legacy MessageID。
2. 组合根为每个 GM 连接代际创建一个 `GameOutputService`，该代际上的所有 Battle 保存其 Ref。Battle 每次只提交一个完整批次；Service 保持批次内部顺序，并按批次进入 Mailbox 的顺序交给该代际 sink。
3. 第一阶段 sink 是 `LegacyEgressCodec + LegacyGMConnection`；codec 把 `OutputKind` 映射为 `0x7601`–`0x7612`，编码 `0x8644 GLHeader + BSREQ_GS2GC_RELAY_HEADER + NHSK Suffix`，connection 的单 writer 顺序发送。
4. 第二阶段替换 GameMaster 后，sink 改为 Cluster Command；玩法代码不变。

`LegacyGMConnection` 是 socket 唯一 I/O owner，使用有界 FIFO 和单 writer。它可以拥有 reader/writer goroutine，但必须支持取消、关闭并等待真实返回；Service 和玩法逻辑不得创建 goroutine。

GameOutputService 被拒绝、sink FIFO 满或 writer 失败时，连接 owner 关闭当前连接代际，丢弃该代际未发送的 frame，并通过状态事件启动该代际 Service 的收敛；不把输出缓存到下一次连接。旧 GameMaster 随后结束该连接关联的全部 Round。

其中，Batch 已进入 GameOutputService 后的 sink/write 失败由 GameOutputService 报告。若 Battle 对 GameOutputService 的 `Send` 在进入 Mailbox 前同步失败，专用 NHSK BattleService 调用：

```go
type ConnectionFailureKind string

type ConnectionFailureReporter interface {
    FailConnection(ConnectionGeneration, ConnectionFailureKind)
}
```

Reporter 必须立即返回、并发安全且幂等。只有 generation 等于当前连接代际时才取消该连接；旧代际报告静默忽略。它属于 Legacy 应用 adapter，不进入 Core 或通用 `game` 包。

## 0.4 第一阶段验证

- 对旧 GameMaster 的真实入站包建立 golden bytes：逐层解码后得到预期 CommandID 和 typed payload。
- 对每个类型化 `GameOutput` 建立出站 golden bytes：编码结果与旧 GameLogic 的 `0x8644` 包逐字节一致。
- 将同一输入分别通过 Legacy TCP 和直接 `Send` 投递，断言 Battle snapshot 与输出序列一致。
- 将允许返回结果的同一 Command 分别通过 `Send`、`Call` 和 Legacy TCP 投递：三者状态与异步输出一致，只有 `Call` 调用方收到不含客户端输出的 `CommandResult`。同局不同入口竞争时按 Mailbox 顺序处理，并由 TurnRevision/VerifyCode 拒绝迟到动作。
- 使用录制的完整一局输入流同时驱动旧、新 GameLogic，比较输出 MessageID、目标、payload 和结算；随机数、Clock 使用固定注入。
- 切换时同一个 GameMaster 只连接新、旧 GameLogic 中的一个。连接切换使 GameMaster 结束断开 GL 上的 Round；新 GL 只承载切换后创建的牌局。新 GL 失败时断开并切回旧 GL，旧 GL 只承载回切后创建的牌局，不恢复新 GL 的内存状态。

## 0.5 GSR 目录与包边界

第一阶段是一个具体业务纵向切片，不按旧系统进程角色或通用技术层拆顶层目录：

```text
examples/nhsk/
  main.go                    # 最薄入口，只调用 run
  node.go                    # 组合根：Runtime、Service、连接和关闭顺序
  config.go                  # 当前 GameLogic 节点配置
  commands.go                # NHSK/容器类型化 Command
  outputs.go                 # 协议无关的 CommandResult 与 GameOutput
  host_service.go            # BattleID -> BattleRef 与生命周期 owner
  battle_factory.go          # 组合根拥有的 Battle 创建/停止边界
  bridge_service.go          # GameInnerId 规范化、Host 解析与 Legacy 路由
  legacy_mapper.go           # Legacy payload、BattleID 与 Command/Output 的显式映射
  cards.go                   # 牌与牌面值对象
  rules.go                   # 牌型和比较规则
  state.go                   # NHSK BattleLogic 私有状态
  logic.go                   # game.BattleLogic 实现
  ai_provider.go             # 类型化 AI seam 与默认本地 provider
  ai_runner.go               # 组合根拥有的有界外部工作 runner
  ai_legacy_http.go          # 可选 RobotServerAddrs 兼容 adapter
  *_test.go                  # 同包规则、Service、场景和兼容测试
  testdata/                  # 完整牌局录制和对拍语料
  internal/legacywire/
    header.go                # BS/GL/Game/Relay wire header
    packet.go                # transport 控制帧与容器 frame
    codec.go                 # 有界小端编解码
    connection.go            # 单条全双工 GM 连接及生命周期
    *_test.go
    testdata/                # 字节级 golden corpus
```

依赖只能向内：

```text
main/node
  -> host/bridge/factory/logic（同一 nhsk 纵向切片）
  -> game + runtime
  -> internal/legacywire

internal/legacywire
  -> Go 标准库
```

约束：

- 不创建 `cmd/nhsk-*`、`app/nhsk`、`adapter/legacygm`、`examples/nhsk/game`、`examples/nhsk/protocol` 或 `examples/nhsk/assembly`。这些目录按旧角色或技术层切分，会把一次业务变化散到多处。
- `examples/nhsk` 根包使用 `package main`，沿用 `examples/whackmole` 的 GSR 示例方式。内部类型默认不导出；只有测试或真实跨包调用需要时才扩大接口。
- `internal/legacywire` 只理解字节、长度、握手和连接生命周期，不 import `game`，也不定义 NHSK Command。
- Host、Bridge、Factory、Logic 以职责明确的文件留在同一业务包；它们不是新的通用框架层。
- MySQL、Redis、Auth、Gateway、Agent 和新 GameMaster 不在第一阶段创建目录。进入对应阶段时先写 RFC，再决定是否属于 `tooling`、`game` 或新的具体 example；不预建空包。
- 第二个玩法证明同一复杂边界可复用后，才从 `examples/nhsk` 上移到 `game`。Legacy GM wire 只服务迁移，不上移到 GSR `transport`。

## 1. 当前基线

仓库已经具备：

- `tooling/entry`：内存 `SessionRegistry`、`LoginService`、TCP `LoginAdapter`、TCP `GatewayAdapter`、SingleSession ticket、proof 和 `ProtocolMapper`。
- `game`：Player、Room、Battle、Timeline、Wallet、外部 `LedgerRunner` 与示例用 `MemoryLedgerStore`。
- `tooling/servicegroup`：显式 `ServiceSet`、Directory 与路由策略，可承载多个游戏 Service 实例。
- `examples/client-entry`：从登录票据到 Gateway Command 的内存纵向切片。
- `examples/whackmole`：Room/Battle/Timeline/Settlement/Record/Replay 的最小游戏示例。

当前明确缺少：

- 生产第三方认证、持久化 Session、MySQL Repository/LedgerStore、Redis adapter。
- 统一应用配置、结构化日志、secret 脱敏、依赖健康检查和多进程组合根。
- Agent 的公开契约、棋牌游戏 Lobby/Match/Table/Seat/Turn/Reconnect 领域裁决。
- 从真实登录到完整棋牌游戏结算落库的端到端示例。

## 1.1 已确认决策

- 首个完整示例是宁海双扣。
- 当前第一交付目标是只替换 GameLogic；GameMaster、Agent、客户端以及它们之间的协议暂时保持不变。
- 以下目录共同构成宁海双扣的只读知识来源。本项目默认不修改它们，也不依赖它们运行；规则、协议和容器行为先从参考代码提取，再以 GSR RFC 和测试重新冻结：
  - `/Users/lijiawang/Documents/cocos/laya/nhsk`：宁海双扣规则、状态机、Legacy 游戏消息和结算行为。
  - `/Users/lijiawang/Documents/cocos/laya/gamelogic`：承载玩法的旧运行容器、消息入口、回合管理和外围能力装配。
  - `/Users/lijiawang/Documents/cocos/laya/gamemaster`：GameLogic 连接登记、GM2GL 推送、GL2GM dispatch、Round 绑定和断线收敛行为。
  - `/Users/lijiawang/Documents/cocos/laya/gamecore`：旧容器与玩法之间的 `Game`、`GameFactory`、`GameLogicAPI` 等接口知识。
  - `/Users/lijiawang/Documents/cocos/laya/protocol`：外围消息、Relay、Header 和共享 MessageID 的协议知识。
- 长期目标中 Gateway、Login、Auth、Agent 和 Game 均可独立部署；第一阶段只有新 Game 进程使用 GSR，旧 GameMaster 仍通过 TCP 与它协作。
- 旧 GameMaster 当前是玩家、Round、GameLogic 和 Agent 之间的协调/粘合节点：它分配 Legacy `GameInnerId`、保存 Round 到 GL 的关系、发送生命周期与游戏消息并转发 GL 输出。第一阶段冻结这些职能，只替换 GameLogic；GameMaster 的 GSR 化和职责拆分留到下一迁移阶段。
- Auth 除微信认证外，还要提供无需微信环境即可由开发者独立完成登录的简单认证。
- Gateway 拥有客户端 socket。Agent 节点承载多个 AgentService 实例，每个已登录玩家会话对应一个实例；AgentService 断线后保留一段时间，使玩家能回到通常 5 分钟内结束的原牌局。
- 重连以 BattleService 的权威快照为准。Agent 不把客户端未确认的旧消息缓冲重新当作业务事实。
- Legacy TCP 二进制协议、结构布局和 MessageID 全部保留，但只覆盖认证后的宁海双扣游戏消息；不新增 Protobuf。
- 同一游戏 Command 可由其他 Service 直接 `Send`/`Call`，也可由 Legacy TCP adapter 解码后投递；两种入口共享同一 Handler 和规则实现。
- 开发认证使用 `account + shared token`，不提供注册流程，只在显式开发配置下启用。
- Agent 无活跃牌局时离线保留 2 分钟；活跃牌局中保留至终局后 2 分钟；离线保留绝对不超过 10 分钟。重连的新会话代际替换旧连接。
- 首个纵向切片的权威业务状态全部保存在对应 Service 内存中。MySQL、Redis 先交付独立工具模块，不接管 Session、玩家、牌桌或结算状态，也不作为宁海双扣示例启动的前置依赖。

## 1.2 宁海双扣参考基线

只读参考项目当前提供以下可验证行为：

- 4 名玩家，座位 `0/2` 与 `1/3` 分属两队。
- 使用 104 张牌，每人 26 张；牌型至少包含单张、对子、三张、三带二和 4–8 张炸弹。
- 对局流程为 `GameStart -> Deal -> AskOutCard -> OutCard -> GameOver -> ProcessResult`。
- 出牌请求带递增校验码；非当前座位、旧校验码、手牌不包含、非法牌型或不能压过上一手都会被拒绝。
- 三家过牌后由上一位有效出牌者获得本轮 5、10、K 分牌；若该玩家已经出完，由其对家取得下一轮先手。
- 同一队两名玩家都出完时结束，按名次、抓分和托管修正计算单扣或双扣。
- 离线只改变玩家在线状态。Legacy `USER_RECONNECT` 进入 `ReconnectPlayer`：校验座位后清除 Offline，仅在小局 playing 时退出托管/机器人并恢复玩家视图。`GAME_SCENE` 进入独立 `RequestGameScene`：先校验有效 game/subgame 和座位，不清除 Offline，再退出托管/机器人并恢复视图。两者共用 `RestorePlayerView`，按参考顺序和可见性设置 ClientReady，恢复 GameInfo、GameScene 和当前 AskOutCard；亮牌阶段保留可能再次全桌广播 ShowCard 的行为。目标部署关闭的解说时间与没有目标规则证据的战绩不实现。前置条件不满足或已停止时 Legacy 静默忽略。
- Cluster `GetBattleSnapshot` 是独立纯查询，与重连共用 Snapshot 构造模块，但不修改 Offline、RobotState、ClientReady，也不产生 GameOutput。`Quarantined` 不执行重连副作用、不伪造场景或结束包；Legacy 静默拒绝并记录指标/告警，Cluster 返回 `ErrBattleQuarantined`。
- 参考实现包含 AI、托管、偏洗牌、自定义牌堆、约局、回放和框架结算能力；这些不自动全部进入首版示例，需按最小可玩路径逐项确认。
- 标准四人最小可玩切片只用于尽快验证 GSR 纵向架构，不等于已经满足旧 GameLogic 切换条件。切换前必须建立完整能力矩阵：只有旧系统真实运行过、目标部署实际启用过、录制消息出现过或用户明确要求保留的 AI、回放、约局、投票、道具、偏洗牌、自定义牌堆和结算路径，才必须实现兼容或形成已确认的有意偏差。测试、配置字段、协议常量、接口或未接入实现不能单独证明生产使用；确认未使用的能力直接标记“放弃”，不写占位、adapter 或预留接口。
- 已经对外可观察的旧行为即使明显不优雅，也默认先保留，不能在业务搬迁中顺手修改；确需修正时先更新 RFC、golden 和参考核对记录，取得有意偏差确认。未完成能力矩阵和全部必需项验收前，不得宣称新 NHSK 可无损替换旧 GameLogic。
- 参考实现的业务 Timer 枚举包含 `Deal`、`OutCard`、`OutCardRobot`、`OutCardAI`、`ShowCard`、`Commentate` 和 `ContinueDelay`。默认配置依次为 `0ms`、`10000ms`、`0ms`、`0ms`、`0ms`、`0ms`、`0ms`，首次出牌单独使用 `MsFirstOutCard=10000ms`；运行配置可覆盖这些值。参考生产路径会为托管/机器人并行启动 `OutCardRobot + OutCard`，为外部 AI 并行启动 `OutCardAI + OutCard`；这会使配置和调度顺序影响 `AskResp`、超时统计、托管与惩罚，首版明确不复制。新实现每个 `TurnRevision` 只保留一个有效 `ActionDeadline`：普通玩家使用首次/普通出牌期限与 `AskRespTimeOut`；托管/机器人和外部 AI 的专用期限大于 `0` 时分别使用 `AskRespAuto`、`AskRespAITimeOut`，为 `0` 时回退到普通期限与 `AskRespTimeOut`。真实机器人 AI 结果立即以 `AskRespAI` 完成；离线托管结果早到时取消硬期限，并以剩余 `MsOutCardRobot` 最小延迟替换唯一期限，到期后再以 `AskRespAI` 应用候选。Timer 投递失败或 Handler panic 进入单 Battle 隔离。`Deal`、`ContinueDelay` 虽然有配置与处理函数，但没有启动点，首版不主动调度。`StartTimer` 的可选参数没有生产调用，首版不公开 timer-key 参数。`TimerGameOver` 只在枚举和 `StopAllTimers` 中出现，没有启动点或 `OnTimer` handler，已确认是首版不实现的遗留定义；不为它分配 Timeline CommandID。
- 当前 GM 没有 PAUSE 发送点，GM 的 PauseGame/ContinueGame 是 TODO，NHSK 的暂停方法只有直接测试且没有被旧 Round 正确接通。按 D-045，首版不实现暂停/恢复 Command、状态或 Timeline 分支；未来出现真实业务入口时再单独设计。
- 外部 AI 是第一阶段必须保留的 Legacy 兼容能力，不是推荐的长期业务边界。Battle 只产生类型化 `AIRequest`，不组 HTTP、不等待网络；请求保存完整 BattleRef、UserID、SeatID、TurnRevision、VerifyCode 和起始时间，组合根有界 `AIRunner` 调用 `AIProvider` 并把结果以 Command 投回原 Ref。响应 MatchID、RoundID 和内层 VerifyCode 不作为权威，SeatID 必须匹配请求，候选牌重新通过完整规则校验。默认本地 provider 保证示例独立运行；Legacy HTTP provider 精确复现生产 POST、`game_id + data` JSON、base64 和小端 `ASK_MOVE_WITH_SCENE`，其中 `moveMS=MsOutCardRobot=1000ms`，与 `MsAITimeout=6000ms` 硬期限分开。真实机器人有效结果立即应用；离线托管结果若早到，则取消硬期限并以剩余最小延迟替换唯一 Timer。失败不隔离、不改变硬期限；请求响应和隐藏手牌不写日志。
- Legacy 服务端消息偏移为 `GAME_INFO +0x001`、`DEAL +0x002`、`ASK_OUT_CARD +0x003`、`OUT_CARD_INFO +0x004`、`TURN_END +0x005`、`SHOW_CARDS +0x006`、`GAME_RESULT +0x007`、`GAME_SCENE +0x008`、`OUT_CARD_RESULT +0x009`、`COMMENTATE_TIME +0x010`、`CARD_ACTION_WATCH +0x011`、`GAME_SCENE_FOR_AI/ROBOT_RELAY +0x012`；客户端请求为 `OUT_CARD +0x101`、`CARD_ACTION +0x102`。
- Legacy wire 使用小端 `BSHeader{Type uint32, Length uint32}`、固定宽度数组与 suffix offset/size。`BS_MSG_GAME = 0x7600`，因此宁海双扣服务端消息从 `0x7601` 开始，客户端 `OUT_CARD` 为 `0x7701`。

不照搬的实现形态：

- 不复用参考项目的 `gamecore.GameLogicAPI`、`TurnManager`、`TimerManager` 或协议依赖。
- 不把整桌状态暴露为可被外部任意调用的共享 `Game` 对象。
- 不使用直接 goroutine 驱动牌局流程；所有权威状态变化经 BattleService Mailbox Command。
- 不把框架日志、AI HTTP、配置中心或战绩回调直接塞进 Battle Handler。
- 自定义牌堆使用 `CustomDeckProvider`：示例默认本地文件，生产 Redis adapter 兼容旧 `game:makecard:<ProductID>` 优先、空值回退 `<GameID>`。每个小局开始时只由有界 `CustomDeckRunner` 异步装载一次；Battle 在 `Preparing` 接受与当前小局匹配的不可变 catalog，下一小局重新装载。保留旧调试 grammar，不把牌值收紧为标准两副牌集合：任意 `uint8` 十六进制值均可，允许重复和非标准编码；不足 104 项忽略，超过 104 项截取，越界庄家视为未指定。失败、超时、队列满或空值回退普通洗牌，不隔离 Battle；迟到结果忽略。
- 回放保留旧 XML、`NHSK_M<ProductID>R<RoundID>_<YYYYMMDD>_<HHMMSS>_<Seat0UserID>.xml` 名称与 `FuPan/<YYYYMMDD>/<HH>` 目录。Battle 只记录回放事实并在小局结束冻结不可变文档；有界 `ReplayWriterRunner` 在 Battle 外执行 `MkdirAll/Create/Write/Close`，Battle 在 `FinalizingReplay` 等待成功、失败或超时后继续结算。首版不增加原子改名、`fsync`、自动重试或回放上传；失败仍使用既定 replay name，只告警/计量，不隔离。

旧容器能力在 GSR 中按责任重新归位：

| 旧知识来源中的职责 | GSR 目标责任 |
|---|---|
| `roundmanager.Round` 与 `BaseGame` 的串行队列 | 合并为 `BattleService` 的单一 Mailbox；不得在牌桌内再建第二条业务队列 |
| `GameFactory.Create`、插件或本地工厂加载 | 应用组合根中的玩法注册与 Battle factory；Core Runtime 不认识宁海双扣类型 |
| `GameLogicAPI.SendMsg*` / `SendOldMsg*` | Battle 产生类型化业务输出，经 Command 发给 Agent；连接 adapter 最后编码并写回客户端 |
| `GameLogicAPI` 的 Timer 能力 | Runtime Timer 只投递 Command，由 Battle Handler 串行处理超时 |
| 结算、配置、机器人等已确认外部能力 | 窄接口 adapter 或独立 Service；阻塞工作由有界 runner 执行 |
| `BS_MSG_GM2GL_GAME_MSG` / `BS_MSG_GAME_BASE_RELAY` / 内层 `BS_MSG_GC2GS_RELAY` | Legacy Command Bridge 完整兼容输入；adapter 校验并剥离外围 envelope 后映射到同一类型化 Command |
| `BS_MSG_GL2GM_GAME_MSG` / `BS_MSG_GL2GM_GAME_MSG_OLD` | GameOutput adapter 按原业务路径组装的两类兼容输出；Battle 不感知外围 envelope |
| 玩法 `OnMsg(userID, []byte)` | 类型化 Command Handler；原始字节只存在于传输与 codec 边界 |

参考优先级：

- Legacy 外围 Header、Relay 和共享 MessageID 以 `protocol` 为知识依据。
- 宁海双扣的 MessageID 偏移、payload 布局、规则、重连场景和结算以 `nhsk` 为知识依据。
- `gamelogic` 与 `gamecore` 用于理解旧系统必须提供的可观察能力，不作为新 GSR 模块边界或公开 API。
- 出现差异时先记录样本和行为，再在 GSR RFC 中裁决；不得通过直接 import 参考仓库来绕过裁决。

## 1.3 Legacy 双向分层封包

参考代码和现网链路共同确认，玩法请求在进入旧 GameLogic 前被逐层增加路由信息：

```text
Client -> Agent
  BSHeader(Type=0x7402) + NHSK Suffix

Agent -> GameMaster
  BSHeader(Type=0x7402) + GameHeader + SuffixIndex + NHSK Suffix

GameMaster -> GameLogic
  GLHeader(Type=0x8605, GameInnerID, UserID)
    + BSHeader(Type=0x7402)
    + GameHeader
    + SuffixIndex
    + NHSK Suffix
```

GameLogic 的业务输出沿反向链路发送：

```text
GameLogic -> GameMaster
  GLHeader(Type=0x8644, GameInnerID, UserID)
    + BSHeader(Type=0x7402)
    + GameHeader
    + SuffixIndex
    + NHSK Suffix

GameMaster -> Agent
  BSREQ_GS2GC_RELAY_HEADER {
    Header:     BSHeader(Type=0x7402)
    GameHeader: TGameHeader
    SuffixMsg:  BSSUFFIXIDX
  }
    + NHSK Suffix
```

其中：

- `0x7402` 是 `BS_MSG_GC2GS_RELAY` envelope，不是宁海双扣业务 Command。
- `0x8605` 是 `BS_MSG_GM2GL_GAME_MSG` envelope，不是宁海双扣业务 Command。
- `0x8644` 是 `BS_MSG_GL2GM_GAME_MSG` envelope，只承载 GameLogic 到 GameMaster 的异步游戏输出，不是同步 `Reply`。
- GameMaster 向 Agent 发送 `BSREQ_GS2GC_RELAY_HEADER`。`SuffixMsg` 描述同一 packet 中宁海双扣消息的 offset 和 size；偏移基准与长度口径继续用 golden bytes 冻结。
- 真正的玩法 MessageID 位于 `NHSK Suffix` 自身的 `BSHeader.Type`。
- Agent 最终如何裁剪并发送给客户端，继续以对应参考代码核实，不在没有证据时按对称链路推断。
- 新 GSR 实现用分层 codec 兼容上述线格式，但在身份、`BattleID` 和玩家路由解析完成后只产生一次类型化 Command。反向输出先形成类型化业务消息，最后由连接 adapter 编码。`BattleService` 不接收或产生 `GLHeader`、`GameHeader` 和原始字节。

## 1.4 MessageID 与输出映射

参考实现的玩家请求是异步投递。`Game.OnMsg` 和内部处理函数的 `bool` 返回值没有沿 Agent、GameMaster、GameLogic 链路返回客户端。因此 Legacy MessageID 不映射为 GSR `Call`，也没有同步 `Reply` 包。

| 方向 | MessageID | 参考语义 | GSR 调用模式 | Reply、错误与业务输出 |
|---|---:|---|---|---|
| C2S | `0x7402` | `BS_MSG_GC2GS_RELAY`；客户端包只携带玩法 Suffix，Agent 转发时补 `GameHeader` 和 suffix 索引 | 不直接映射；adapter 解包 | envelope 畸形时在 adapter 拒绝并记录，不能进入 Battle |
| C2S | `0x7701` | `BSID_NHSK_OUT_CARD`，出牌或过牌 | `Send(NHSKOutCard)` | 无同步 Reply；稳定玩法拒绝向请求玩家发送 `0x7609`；成功后异步广播 `0x7604`，并按流程产生 `0x7603`、`0x7605`、`0x7606`、`0x7607` |
| C2S | `0x7702` | `BSID_NHSK_CARD_ACTION`，记录/旁观出牌动作 | `Send(NHSKCardAction)` | 无同步 Reply；合法时广播 `0x7611`；参考实现对关闭记录、解码失败或座位不匹配不回错误包 |
| Control | 公共 `BS_MSG_GAME_USER_STATE_CHANGE` | 设置或取消托管状态 | `Send(SetPlayerAutoState)` | 不定义宁海双扣同步 Reply；是否进入首个切片随托管范围一起冻结 |
| Control | Offline / Reconnect / Scene | 旧 GameMaster 驱动的离线、重连和场景请求 | `Send` 到 Agent/Battle | 重连通过定向异步输出恢复 `0x7601`、`0x7608`，必要时补 `0x7610`、`0x7606` 或 `0x7603` |

`0x7701` 的参考错误兼容细节：

- 非当前座位：`0x7609 / OutCardSeatError(2)`。
- 校验码过期：`0x7609 / OutCardSeatVertifyCodeError(3)`。
- 出牌数量非法：`0x7609 / OutCardCountError(1)`。
- 手牌不包含、首手为空、牌型非法或不能压过上一手：`0x7609 / OutCardTypeError(4)`。
- 牌局暂停：`0x7609 / OutCardTypePause(5)`。
- 包解码失败、阶段不接收、强制 AI 已接管等参考分支只记录或丢弃，不回错误包。
- 参考实现没有在成功时发送 `OutCardNoError(0)`；成功事实由后续 `0x7604` 等业务输出表达。

全部服务端宁海双扣输出都是异步消息：

| MessageID | 名称 | 参考目标 |
|---:|---|---|
| `0x7601` | `GAME_INFO` | 开局广播；重连时定向 |
| `0x7602` | `DEAL` | 按玩家裁剪手牌后定向 |
| `0x7603` | `ASK_OUT_CARD` | 正常流程广播；重连时向当前行动者定向 |
| `0x7604` | `OUT_CARD_INFO` | 广播 |
| `0x7605` | `TURN_END` | 广播 |
| `0x7606` | `SHOW_CARDS` | 终局广播，或按规则向特定玩家定向 |
| `0x7607` | `GAME_RESULT` | 广播 |
| `0x7608` | `GAME_SCENE` | 重连/场景恢复定向 |
| `0x7609` | `OUT_CARD_RESULT` | 仅向被拒绝的人工出牌玩家定向 |
| `0x7610` | `COMMENTATE_TIME` | 广播或重连定向 |
| `0x7611` | `CARD_ACTION_WATCH` | 广播 |
| `0x7612` | `GAME_SCENE_FOR_AI` / `ROBOT_RELAY` | AI/机器人路径，首版客户端兼容暂不使用 |

内部 Service 可以为“查询权威快照”等新 API 定义 `Call`，但这种 `Call` 没有 Legacy MessageID，也不能改变上述兼容语义。

## 1.5 首版权威状态

首版不从参考项目推导不存在的数据库设计。权威状态按 owner 留在内存：

| 状态 | 首版 owner | MySQL / Redis |
|---|---|---|
| socket、frame 读取与写队列 | Gateway connection adapter | 不保存 |
| 登录票据、会话代际和绑定状态 | Login/SessionRegistry Service | 不接管 |
| 玩家到 Agent、Battle 的路由与离线截止时间 | AgentService | 不接管 |
| 玩家档案和本局需要的玩家数据 | PlayerService | 不接管 |
| 匹配、桌位和准备状态 | 对应 Match/Table Service | 不接管 |
| 手牌、轮次、计时、抓分、名次和结算过程 | BattleService | 不接管 |
| 首版结算结果、Record/Replay | 对应 Service 内存及非权威导出 | 不接管 |

进程退出后这些状态可以丢失；首版不承诺跨进程崩溃恢复。MySQL、Redis 工具模块只验证配置、连接生命周期、健康检查、超时、关闭和可替换 seam，等后续 RFC 明确数据 owner 后再接入具体 Repository、Ledger 或 SessionRegistry。

## 2. 第一轮决策门

以下决策会改变模块边界，未确认前不写实现 RFC：

| 决策 | 需要确认的具体问题 | 影响 |
|---|---|---|
| G1：首个玩法 | **已确认：宁海双扣。** 外部 AI、托管、回放、自定义牌堆和当前综合结算进入首版；约局、旁观及没有生产证据的遗留能力放弃 | Table/Seat/Turn、发牌、结算、随机数与回放契约 |
| G2：部署拓扑 | **已确认：Gateway、Login、Auth、Agent、Game 均可独立部署为 Cluster 节点。** 继续确认单个二进制多角色还是每个角色独立二进制 | 组合根、配置、发现、会话路由、故障边界 |
| G3：Agent owner | **已确认：每登录玩家会话一个 Agent，Gateway 拥有 socket，Player/Battle 分别拥有长期玩家/牌局状态；离线 2 分钟、活跃牌局终局后 2 分钟、绝对上限 10 分钟。** | Agent 生命周期、Player 关系、Redis 数据模型 |
| G4：数据分类 | **首版已确认：业务权威状态全部由 Service 内存持有；MySQL、Redis 仅提供未接入业务的工具模块。** 后续接入持久化前另行冻结数据 owner | Repository、事务、TTL、恢复与一致性 |
| G5：客户端协议 | **已确认：只兼容认证后的 Legacy TCP 宁海双扣消息，不新增 Protobuf；`BS_MSG_GAME = 0x7600`。** MessageID、Send/Call/Reply 和输出映射以已建立矩阵及 reference golden 为门禁 | Gateway adapter、Command bridge、兼容测试 |
| G8：Legacy 外围封包 | **已确认：第一阶段在线复刻旧 GM↔GL 完整中间包。** 入站覆盖 `0x8605` 普通/旧 relay，出站覆盖 `0x8644` 普通/旧 relay；外围只存在于 adapter，Cluster 不携带旧 envelope | frame codec 所在进程、兼容层深度、golden bytes 范围 |
| G6：微信流程 | 小程序、公众号、App 开放平台或小游戏登录；服务端拿到的是 `code`、access token 还是平台签名材料 | `AuthProvider` 请求、HTTP 调用、测试替身和错误分类 |
| G7：规模目标 | 单 Gateway 连接数、单桌人数、并发桌数、每秒消息量、目标延迟和部署平台 | worker/连接上限、ServiceGroup、基准和容量门禁 |

已关闭的协议与认证决策：

- 游戏业务以 Command 为唯一入口；集群内可直接 `Send`/`Call`。
- 认证后的 Legacy TCP 消息由 adapter 映射到同一 Command；结果通过 `Reply` encoder 或异步业务输出映射回原 MessageID。
- Legacy 登录和 Gateway 握手不在兼容范围内。
- 不定义 Protobuf 协议。
- 开发认证为 `account + shared token`，无注册流程，不依赖数据库。

## 3. 目标模块图

最终边界需经 RFC 冻结；当前只确定依赖方向：

```text
Client
  -> GSR Login/Gateway authentication
  -> Legacy TCP Game Frame/Codec
  -> Command Bridge
  -> Agent Service
  -> Player / Lobby / Match / Table(Battle) / Wallet Service

Cluster Service
  -> Send / Call
  -> the same Agent / Battle Command handlers

Login Adapter
  -> AuthProvider Runner -> WeChat HTTP Adapter / Development Auth
  -> LoginService
  -> in-memory SessionRegistry

Optional Storage Tooling
  -> MySQL / Redis connection + health + lifecycle seams

Composition Root
  -> config + logger + Runtime + adapters + runners + health + shutdown
```

约束：

- Gateway 只拥有连接与传输状态，不拥有玩家、房间或牌局权威状态。
- Agent 只通过 `ServiceRef` 和 Command 访问其他 Service，不持有对象指针。
- Auth、MySQL、Redis 的阻塞 I/O 不进入 Service Handler。
- 首版 MySQL、Redis 不保存业务数据；未来接入时必须先声明 owner、丢失、过期和重建行为。
- 日志通过 `RequestID`、会话代际、`PlayerID` 和 `BattleID` 关联，但不输出敏感凭据。
- MessageID 只定义一次，Command bridge 维护显式的 MessageID、CommandID、调用模式和 reply/output encoder 映射。
- Legacy adapter 不实现游戏规则；它只完成字节校验、身份绑定、Command 投递和结果编码。

## 4. 计划切片

### Task 0：收敛 ServiceRef 与 Battle 版本模型

**Files:**

- Modify: `game/types.go`
- Modify: `game/battle.go`
- Modify: `game/timeline.go`
- Modify: `examples/whackmole/service.go`
- Modify: `examples/whackmole/service_test.go`
- Test: `runtime/resolve_remote_test.go`
- Test: `runtime/cluster_test.go`

- [x] 保持现有 `ServiceRef{NodeID, ServiceID}`、`NewRuntime(Config) *Runtime`、TCP handshake v1 和 WireEnvelope v1，不新增 RuntimeEpoch、随机 ServiceID 起点或构造随机源失败分支。契约锁定 `ResolveRemote` 使用 `{Node, ID:0}` 节点端点查询名字。
- [x] 写双节点断线/重连测试：旧 PendingCall 在断线时失败；稳定名字在重连后重新 `ResolveRemote`。测试明确允许节点重启后的 ServiceID 与旧地址重合。
- [x] 删除 `game.BattleEpoch` 及其在 Battle、Timeline、WhackMole 中的传播，不增加 BattleRevision。Timer 使用当前 BattleRef + TimelineRevision；玩法迟到输入分别使用 TurnRevision、TimelineRevision、VerifyCode 和小局身份。
- [x] 写 Battle 公开契约测试，证明 BattleConfig、BattleSnapshot、BattleContext 不暴露通用 Epoch 或 Revision，`game` 不导出 BattleEpoch/BattleRevision。
- [x] 运行 `go test ./...`、`go vet ./...`、`go test -race ./...` 和 `git diff --check`。
- [x] 提交：`1fd1e2f`（`重构：删除无消费者的 Battle 代际`）。

### Task 1：冻结 Phase 14 RFC 集

**Files:**

- Create: `docs/rfcs/RFC-0410-Example-NHSK-GameLogic.md`
- Create: `docs/reviews/nhsk-capability-matrix.md`
- Create: `docs/reviews/nhsk-legacy-message-matrix.md`
- Create: `docs/reviews/nhsk-reference-reconciliation.md`
- Modify: `docs/SUMMARY.md`
- Modify: `docs/DECISIONS.md`
- Modify: `docs/rfcs/RFC-0500-Roadmap.md`
- Modify: `docs/TODO.md`

- [x] 把已关闭的 G1、G5、G8、BattleRef 直连和 GM↔GL 单实例切换决策写入 RFC；认证、持久化和 Agent 决策继续留在后续阶段。
- [x] 为 `RFC-0410` 写明 owner、公开 seam、生命周期、失败语义、RequestID、Legacy 兼容与验收。
- [x] 创建参考核对记录，固定列为功能切片、参考入口/测试、核对项、结论、RFC/决策链接和证据；文档开头声明它不是契约。
- [x] 审核 RFC 与 `RFC-0290`、`RFC-0300`、`RFC-0370` 的边界，确保不把应用能力压入 Core。
- [x] 运行 `go test ./runtime -run 'TestRFC' -count=1`，文档规则全部通过。
- [x] 提交：`6a4eae5`（`文档：冻结宁海双扣替换契约`）。

### Task 2：建立应用配置、日志与生命周期

**Files:**

- Create: `examples/nhsk/config.go`
- Create: `examples/nhsk/config_test.go`
- Create: `examples/nhsk/logging.go`
- Create: `examples/nhsk/logging_test.go`
- Create: `examples/nhsk/node.go`
- Create: `examples/nhsk/node_test.go`
- Create: `examples/nhsk/config.example.json`

- [x] 先写失败测试，覆盖缺失必填配置、非法地址/容量/超时、环境覆盖、未知字段拒绝和 secret 不出现在格式化错误中。
- [x] 定义进程角色、节点、Runtime、MySQL、Redis、微信、日志、关闭超时与 Legacy GM 连接配置字段；连接默认值为 Dial 5 秒、origin 5 秒、初始退避 1 秒、最大退避 30 秒、倍数 2、jitter 0.2、稳定重置 60 秒。配置加载只解析和校验，不创建资源；拒绝非正超时、初始值大于上限、倍数小于等于 1、jitter 小于等于 0 或大于等于 1。Task 3 的组合根映射再把这些字段转换为 `legacywire.ConnectionConfig`。
- [ ] 先写日志测试，验证稳定字段、级别、JSON 输出、错误分类与 token/secret/proof/code 脱敏。
- [ ] 基于 `log/slog` 实现 logger 构造与领域字段 helper，不向 Core 增加通用 logger API。
- [ ] 先写 `node` 关闭测试，验证当前 GameLogic 纵向切片的 readiness、连接、factory、Service 和 Runtime 按显式逆序关闭，重复关闭稳定，超时仍可诊断真实未返回 owner。
- [ ] 在 `node.go` 直接装配并持有本节点资源；第一版不创建通用 lifecycle group，等第二个真实节点出现后再评估是否上移到 Tooling。
- [ ] 运行 `go test ./examples/nhsk -run 'Config|Logging|Node' -count=100` 与 `go test -race ./examples/nhsk -run 'Config|Logging|Node' -count=20`。
- [ ] 提交：`feat(nhsk): 增加 GameLogic 节点组合根`。

### Task 3：冻结 Command 与 Legacy TCP Bridge

**Files:**

- Create: `examples/nhsk/commands.go`
- Create: `examples/nhsk/outputs.go`
- Create: `examples/nhsk/output_service.go`
- Create: `examples/nhsk/output_service_test.go`
- Create: `examples/nhsk/bridge_service.go`
- Create: `examples/nhsk/bridge_service_test.go`
- Create: `examples/nhsk/legacy_mapper.go`
- Create: `examples/nhsk/legacy_mapper_test.go`
- Create: `examples/nhsk/internal/legacywire/header.go`
- Create: `examples/nhsk/internal/legacywire/packet.go`
- Create: `examples/nhsk/internal/legacywire/codec.go`
- Create: `examples/nhsk/internal/legacywire/codec_test.go`
- Create: `examples/nhsk/internal/legacywire/connection_config.go`
- Create: `examples/nhsk/internal/legacywire/connection_config_test.go`
- Create: `examples/nhsk/internal/legacywire/connection.go`
- Create: `examples/nhsk/internal/legacywire/connection_test.go`
- Create: `examples/nhsk/internal/legacywire/failure_reporter.go`
- Create: `examples/nhsk/internal/legacywire/failure_reporter_test.go`
- Create: `examples/nhsk/internal/legacywire/testdata/origin.bin`
- Create: `examples/nhsk/internal/legacywire/testdata/game_info.bin`
- Create: `examples/nhsk/internal/legacywire/testdata/out_card.bin`
- Create: `examples/nhsk/internal/legacywire/testdata/game_result.bin`
- Create: `examples/nhsk/internal/legacywire/testdata/game_scene.bin`

- [ ] 冻结 `BS_MSG_GAME = 0x7600` 和全部宁海双扣绝对 MessageID；测试逐个断言，禁止 codec 私自复制常量。
- [ ] 用 golden bytes 冻结双向 origin、入站 `0x7402 + GameHeader + SuffixIndex + NHSK Suffix`、`0x8605 + 0x7402 envelope`，以及出站 `0x8644 + BSREQ_GS2GC_RELAY_HEADER + NHSK Suffix` 和 GameMaster → Agent 的 `BSREQ_GS2GC_RELAY_HEADER + NHSK Suffix`；分层测试每次只处理自己拥有的 Header。
- [ ] 先写 frame 失败测试：`Length < 24`、`Length > 8192` 和无法恢复边界关闭当前 ConnectionGeneration；边界完整的未知 MessageID 只告警丢帧；已知消息固定区、内层 Header、suffix 下界或上界非法时告警、计量、丢弃当前帧且连接继续。证明所有坏帧都没有 Battle Command、Reply 或 GameOutput，并且下一条完整帧仍可处理。
- [ ] 用精确字节测试冻结 Header 字段偏移和双向 origin：GameLogic 首帧 `Origine=107`，GameMaster 首帧 `Origine=100`，两者 `Type=0x600`、`Length=24`，其余字段按参考构造为零；入站只把 Type、Origine、Length 作为握手契约，不把 Magic、Reserve、Serial 或 Param 提升为认证字段。
- [ ] 不在 `go.mod` 引用任何参考 module。用独立生成步骤从 v1.0.5 参考 formatter 产出并人工核对 `.bin` fixture，生产 `legacywire` 只实现当前保留消息的最小常量和 codec；测试证明业务 Command、Battle 与 Output 不导入或暴露旧协议类型。
- [ ] 写嵌套一致性测试：外层 `HeaderLen=34`、外层 Length 等于整帧、内层 Length 等于外层余量、suffix 以所属内层起点计算且没有尾部字节；拒绝线上零 Length，不复制参考 formatter 由 `PackDetect` 补写的中间态。
- [ ] 写身份归一化测试：外层 GameInnerId/UserId 生成 BattleID/UserID；玩家帧内外 UserID 不同、初始化后 MatchID/ProductID 不同、玩家动作 UserID 为零都被 adapter 丢弃且不触达 Battle；明确允许的零 UserID 控制/广播消息正常映射。成功结果只留下规范化 Command，不携带重复 Legacy Header。
- [ ] 写 Header 元数据测试：Magic/Reserve 不进入 Command，异步 GameOutput Serial 为零，Param 只由逐消息 codec 显式赋值；不存在通用 Serial pending-call 表，也不把 NewGame ACK、`0x8644` 或其他异步输出编码为 GSR Reply。
- [ ] 定义协议无关的入站/出站消息类型；固定数组先映射为独立 slice/value，再进入 Command bridge。
- [ ] 从参考实现与真实样本生成 Legacy golden bytes，覆盖 Header、固定字段、隐藏手牌、`GAME_RESULT` suffix、`GAME_SCENE` 双 suffix 和畸形长度。
- [ ] 建立 NHSK 能力兼容矩阵，逐项记录功能、参考入口、调用方、测试、目标部署启用事实、录制消息证据、Legacy MessageID、实现状态和裁决；AI、回放、约局、投票、道具、偏洗牌、自定义牌堆及每条结算路径不得按目录名直接纳入。确认旧系统未使用的能力标记“放弃”并不实现任何预留代码；发现真实可观察缺陷则默认兼容或先取得有意偏差确认。
- [ ] 为自定义牌堆先写 provider/runner 契约测试：本地文件成功加载；ProductID 优先、GameID 回退；白名单房间生效；非白名单房间由 provider/runner 直接返回无 catalog 且不读取数据源；每个小局只装载一次且下一小局重新装载；Battle 在 `Preparing` 只接受当前小局结果且不持有 enable/白名单；`0x01..0x68` 连续字节、重复和非标准编码保持可用；不足 104 项忽略，超过 104 项取前 104 项，越界庄家视为未指定；token/庄家解析错误、读取错误、超时、队列满或空值都回退普通洗牌且不隔离 Battle；迟到结果忽略。
- [ ] 为回放先写 golden 与 runner 契约测试：固定 Clock 下名称和 `FuPan/<date>/<hour>` 路径与参考一致；XML 覆盖规则、发牌、动作、道具留痕和结算；综合结算响应先提交客户端 GameResult，再进入 `FinalizingReplay`；`MkdirAll/Create/Write/Close` 成功、失败或超时收敛后才发送 GM GameOver。逐项失败和队列满仍携带既定 replay name、只告警/计量且不隔离；旧 BattleRef、旧小局结果或重复结果不能再次通知 GM；不存在原子改名、`fsync`、自动重试或回放上传调用。
- [ ] 实现小端 Legacy codec；严格验证总长度、suffix 范围、玩家数、牌数和尾部多余字节。
- [ ] 按参考实现冻结映射：`0x7701 -> Send(NHSKOutCard)`、`0x7702 -> Send(NHSKCardAction)`；`0x7402`、`0x8605`、`0x8644` 只作为 envelope，不定义业务 Command 或同步 `Reply`。
- [ ] 冻结 `0x7609` 仅表达人工出牌拒绝且成功不发送 error=0；冻结 `0x7702` 非法输入无错误包，以及所有 `0x7601`–`0x7612` 输出的广播/定向目标。
- [ ] 通过等价测试证明：Cluster 调用方从 `.nhsk-game-host` 解析 `BattleRef` 后直接 `Send`/`Call`，Legacy TCP bridge 经 Host 定位目标，三者进入同一个 Battle Handler，得到相同状态变化和异步业务输出；Cluster 玩家动作必须携带属于本局的 `UserID`。Call Reply 只含类型化结果和拒绝原因，不含通用 Revision，也不复制客户端 GameOutput。另测 GM 断线后的 stopping/not-found、隔离错误和 ConnectionGeneration fence 都不能被 Cluster 入口绕过。
- [ ] 增加最小 Send/Call 测试：Send 成功只证明入箱；普通玩法 payload 和 Battle 状态中不存在通用 RequestID 或幂等结果表；Call 超时不撤回已经执行的状态，也不自动重发，调用方可用 Snapshot 判断结果。断言 transport 未送达/超时/断线与业务拒绝使用不同错误层；结算、钱包、创建和隔离释放继续只验证各自已有的专用幂等键。
- [ ] 增加完整重连对照测试：分别验证 `USER_RECONNECT` 清除 Offline 且只在 playing 时恢复视图，以及 `GAME_SCENE` 要求有效 game/subgame、绝不修改 Offline；两者与参考实现逐项比较 RobotState、ClientReady、TurnRevision、TimelineRevision，以及 GameInfo、GameScene、AskOutCard、亮牌 ShowCard 的顺序、定向/广播目标和隐藏信息；明确断言目标部署关闭的解说时间与没有目标规则证据的战绩不会产生输出。无效请求保持静默。独立 `GetBattleSnapshot` Call 返回同源快照但不改状态、不输出；`Quarantined` Legacy 请求静默告警且 Cluster 返回隔离错误。
- [ ] 先写连接代际输出测试：同一代际只创建一个 GameOutputService，多个 Battle 共用其 Ref；批次内部不交错，不同批次按 Mailbox 接受顺序进入单 writer。Service/Mailbox 拒绝、sink 队列满和 writer 失败关闭当前代际、丢弃未发 frame，且新代际看不到任何旧输出。
- [ ] 先写 reporter seam 测试：Battle 到 GameOutputService 的 Send 同步拒绝时只报告 generation 和稳定失败类别；匹配当前代际会取消连接，重复调用幂等，旧代际迟到报告不关闭新连接。Reporter 不阻塞、不修改 Battle、不执行重连或重放。
- [ ] 先写连接状态机测试：每次物理连接先取得新 Generation；本方 origin 必须完整写出并先于业务 frame；GameMaster origin 类型通过且 OutputService 创建成功后才 Ready。Ready 前收到的普通业务字节不被解码或投递；origin 写入、读取或校验失败，创建失败和提前 EOF 都关闭该代际。关闭顺序禁止新输出、停止 OutputService、取消连接并等待 reader/writer 返回。
- [ ] 先写重连测试：注入 fake Clock 和确定性 jitter source，断言基础退避为 `1s, 2s, 4s, 8s, 16s, 30s, 30s`，实际等待落在 ±20% 区间；初次 Dial 失败和 Ready 后断开都不退出进程，readiness 为 false；Ready 不足 60 秒继续增长，连续 Ready 60 秒后重置到 1 秒；Dial 与 origin 分别在 5 秒超时；同时最多一个 Dial，`Send` 不发起连接，关闭立即取消等待。断开旧代际时收敛 `Creating`、`Active`、`Stopping`，保留 `Quarantined` 并阻止新代际覆盖其编号；新代际不接收旧输出、旧输入或旧 BattleRef，只接收新建牌局。
- [ ] 运行 `go test ./examples/nhsk/... -run 'MessageID|Legacy|Bridge|Origin' -count=100`，预期全部通过。
- [ ] 提交：`feat(nhsk): 兼容 Command 与 Legacy GameLogic 协议`。

### Task 4：建立 MySQL 工具模块

该任务不阻塞 GameLogic 替换，也不在当前阶段创建目录。进入存储脚手架阶段时，先用 RFC 确认 driver、公共调用方和依赖层级；只有多个 GSR 纵向切片需要同一连接生命周期后，才决定它属于 `tooling` 的可复用模块还是具体 example 的内部 adapter。首版不得创建玩家、牌局或账本 schema。

### Task 5：建立 Redis 工具模块

该任务不阻塞 GameLogic 替换，也不在当前阶段创建目录。进入快速存储阶段时，先冻结真实 owner、key、TTL、丢失与重建语义；没有业务 owner 前只讨论连接工具，不把 Redis 接入 NHSK 节点启动路径。

### Task 6：实现认证 seam、微信和开发 provider

该任务属于替换更靠近客户端入口的后续阶段。届时先扩展 `RFC-0290` 或新增相邻 RFC，再决定认证能力是否进入 `tooling/entry`；当前不创建顶层 `auth` 包。微信 provider、开发 provider 和有界外部 I/O runner 必须由真实 Login/Auth 纵向切片证明接口后再落目录。

### Task 7：打通 Gateway、Login、Legacy Bridge 与 Agent

该任务在 GameMaster 替换之后执行。Agent 是否成为 `game` 公共模板，必须先由宁海双扣和第二个玩法共同证明；当前不创建 `game/agent.go`，也不修改 `tooling/entry`。后续场景测试归到对应纵向切片，不建立脱离 owner 的顶层 `tests/scenarios` 技术目录。

### Task 8：用宁海双扣补齐棋牌游戏领域

**Files:**

- Create: `examples/nhsk/cards.go`
- Create: `examples/nhsk/cards_test.go`
- Create: `examples/nhsk/rules.go`
- Create: `examples/nhsk/rules_test.go`
- Create: `examples/nhsk/state.go`
- Create: `examples/nhsk/logic.go`
- Create: `examples/nhsk/logic_test.go`
- Create: `examples/nhsk/host_service.go`
- Create: `examples/nhsk/host_service_test.go`
- Create: `examples/nhsk/battle_factory.go`
- Create: `examples/nhsk/battle_factory_test.go`
- Create: `examples/nhsk/ai_provider.go`
- Create: `examples/nhsk/ai_provider_test.go`
- Create: `examples/nhsk/ai_runner.go`
- Create: `examples/nhsk/ai_runner_test.go`
- Create: `examples/nhsk/ai_legacy_http.go`
- Create: `examples/nhsk/ai_legacy_http_test.go`
- Create: `examples/nhsk/round_test.go`

- [ ] 从只读参考项目提取 4 人、对家组队、104 张牌、牌型比较、出牌/过牌、抓分、结束与单扣/双扣结算用例，先写入 RFC 和表驱动测试。
- [ ] 核心规则表必须覆盖：两副无王牌的精确多重集合和每座 26 张；`3..K<A<2`；单/对/三/三带二/4～8 张炸弹；异型不可压、炸弹跨型、炸弹张数与同张数点数比较；首出禁止过牌；已出完座位跳过、三家过后墩结束、分牌归属和对家领出；两组对家全部名次排列；`100/105/200` 前后边界的单双扣与胜负倍数。
- [ ] 增加随机初始化偏差测试：每小局只调用一次重置并只从 Battle PRNG 抽取一次普通庄家；自定义庄家覆盖该结果。核对记录明确参考双 Reset 被删除，不能因追求随机序列字节一致而重新引入。
- [ ] 增加操作输出 golden：VerifyCode 从 3 开始按 2 递增；GameInfo 广播后四份 Deal 只含各自手牌，再广播 Ask。玩家各类拒绝只收到失败结果且旧 ActionDeadline 不变，AI/托管拒绝没有客户端输出；成功动作没有 ACK，并精确验证出完亮对家、OutCardInfo、TurnEnd、下一 Ask 的目标和顺序。终局验证最后 OutCardInfo、全桌 ShowCards、0x8650、客户端 GameResult、回放结果、逐玩家 ROUND_STAT、GM GameOver 的跨阶段线序。
- [ ] 增加生命周期输出 golden：每小局先发送无 body `0x7205 GAME_START`，再发送 `GAME_STARTED(Res=true, ReplayName)`，最后才进入 NHSK GameInfo/Deal/Ask；确认生产 encoder/switch 不含无调用点的 `0x7206 GAME_END`。GAME_START 与全部 NHSK ClientGameOutput 对 ClientReady=false 仍生成，只跳过 Exited，且领域 API 不出现 force 参数；START 遇任一 Exited 座位稳定拒绝，UPDATE_PLAYER 激活后重试成功。普通小局在客户端结果和回放之后，向每个未退出且 ClientReady 玩家定向发送相同的 `0x7246 ROUND_STAT(PlayerCount=0)`，再发送 `GAME_OVER(IsGameOver=0, PlayerCount=4)`；PlayerDatas 严格按 SeatID 0..3 编码最终 Score、最新 Exp、最终 Auto，约局字段为零，且不发送 NOTICE。断言不解析 BaseRule index 5、不建立跨局统计、不用回放 Summary 填充空包。缺少任一座位时不发送错位 GAME_OVER 并隔离该 Battle。MATCH_STOP 则展示牌、跳过 0x8650、完成客户端结果与回放、依次发送 ROUND_STAT、GAME_OVER 和 NOTICE_ROUND_OVER。
- [ ] 增加 MATCH_STOP 阶段表测试：Playing 和 AwaitingSettlement 都废止原 Timeline/0x8650 单飞并按 Success(0) 走强制本地结果，迟到结算 ACK 无效；AwaitingInit、Preparing、FinalizingReplay、SubgameFinished no-op。单独等待强制流完成时断言 GameResult→回放→GAME_OVER→NOTICE；立即跟随 DEL_GAME 时断言屏障不等待 writer，已提交输出保留、未完成回放结果被 fence、编号只在 Runtime Stop 完成后释放。
- [ ] 增加 CARD_ACTION golden：WaitingForAction 的当前用户可以广播空列表、手中牌、重复牌、非手牌和不能压牌的任意最多 26 字节列表，payload 原样进入 CARD_ACTION_WATCH 且不改变手牌/回合；非当前用户、非玩法阶段、超长或坏结构拒绝且无输出。断言没有 BaseRule index 2 开关，OUT_CARD 的完整校验不受影响。
- [ ] 增加新手发牌 golden：固定 Battle seed 后先普通洗牌，再对座位顺序中第一个非机器人执行旧 `RandCardListByNewPlayer` 等价调整，并断言四家最终牌序而非只断言目标座位；全机器人不调整。可用自定义牌堆必须绕过新手路径；实现和构造依赖中不得出现 Nacos、每座偏牌配置或通用 `BiasedShuffling`。
- [ ] 增加普通发牌散牌调整 golden：GameRule 第四项完整、缺失、空值、非法值和 `<=0` 分别得到指定值、默认 4 或关闭；固定 seed 下锁定旧 `SwapSingleCard` 的座位顺序和四家最终牌序，同时验证总牌多重集合未变且每座 26 张。不得断言四家最终单张数都低于阈值；自定义牌堆和新手牌均不重复执行此普通路径。
- [ ] 增加 TestMode 放弃项结构测试：NHSKConfig、生产组合根、Legacy/Cluster Command 和发牌分支都不存在 `test_mode_enabled`、TestMode 或固定六张牌 helper。确定性随机测试使用固定 seed，完整指定牌局通过测试注入的 CustomDeckProvider catalog 完成，不创建第三种生产发牌来源。
- [ ] 增加 NHSKConfig owner 结构测试：玩法配置中不存在 MsDeal、MsContinueDelay、TableMultiplier、MsShowCard、MsCommentate、TestMode、RecordUserAction、RobotServerAddrs、回放根目录、Redis key 或自定义牌堆 enable/白名单。AIProvider、ReplayWriter、CustomDeckProvider/Runner 分别解析自己的应用配置；Battle 构造只接收类型化依赖，配置对象创建后不可变。
- [ ] 增加 INIT owner 表驱动测试：codec 完整解析四个 suffix 和所有固定字段；归一化后 BattleIdentity 只持有 BattleID、ProductID、MatchID、RoundID、RoundUniCode，进度上限与当前 UPDATE_GAME 计数分离，GameID 不进入每桌状态。Fee 精确进入 GameInfo，ScoreBase/Denominator 进入回放分数换算；ReplayMetadata 只保存 MatchName/GameType/ScoreType/ScoreMode/RoomID/CreatorID，并由 ReplayBuilder 复用同一身份和计分快照。TourneyID/MatchTime/PhaseID/BoutID/StageID/TableID/CreateTime 不出现在领域结构；冲突 INIT 拒绝，成功后 Handler 不重复比较整份 INIT。
- [ ] 增加 GameDescriptor 测试：NHSK 组合根只构造 `{82,"宁海双扣"}`；Legacy NEW_GAME GameID 82 可进入正常异步创建，0 或其他玩法 ID 只产生 Res=0 且不提交 runner。Cluster NHSK 创建请求与 BattleIdentity 均无 GameID。Replay 根节点和 RobotTran JSON 读取同一 descriptor，结构测试禁止在 ReplayConfig、AIConfig 或 Battle 配置复制常量。
- [ ] 增加小局时间快照测试：fake Clock 在每次读取返回跨秒/跨小时值，StartSubgame 仍只读取一次；StartTimestampSec、UniCode、ReplayName 和 FuPan 目录全部由首次值生成。覆盖 UTC+8 日期边界、CreatorID=0、同秒相同 CreatorID 不额外拒绝，以及 RoundUniCode 原样保留；生产玩法和回放 builder 中不得直接调用 time.Now。
- [ ] 增加 NEW_GAME IsNewNacos 放弃项测试：Legacy decoder/golden 精确读取 0/1，但规范化 CreateLegacyBattle、Host 操作、Battle factory、Cluster 创建和诊断结构中均无该字段；不同 IsNewNacos 值不改变创建 payload 语义或 Battle 行为。
- [ ] 增加托管结算表驱动测试：合法出牌与过牌都增加 MoveCount；真人 AI/Timeout/Auto/AITimeout 响应增加 AutoCount，人工响应和所有机器人响应不增加。覆盖两个阈值均为 -1、只启用 count、只启用 ratio、两者 AND、零值和边界值，严格断言 `AutoCount * RatioFactor >= MoveCount` 而非百分比公式。成功失败组分别覆盖无人托管、单人托管和双人托管的负分修正；非成功结束不修正。GM flags、客户端 PlayerIsAuto 和回放 IsAuto 必须一致。
- [ ] 增加 DRESS 线序测试：Legacy 和 Cluster `UpdatePlayerDress` 都只覆盖已有玩家的不透明字符串；空值覆盖、同值重复、未知用户和接近 8 KiB 帧边界分别按契约处理，无 Reply、客户端输出或 gameplay Revision。DRESS 在 START 前入箱会进入当前小局回放，在 START 后才到则只影响下一次冻结；玩法和重连 Snapshot 不含装扮。
- [ ] 增加道具回放测试：Legacy `BROADCAST_USE_PROP` 与 Cluster `RecordPropUse` 生成相同 `Prop` Move；外层 UserID 不匹配 SenderID、未知发送者、未开始或无当前回放时丢弃且不缓冲。断言 PropID 不解析，SendCount 原值保留，TargetIDs 的顺序及重复值进入 `TargetID` 属性；没有库存/权限查询、玩法 handler、Reply、客户端输出或 gameplay Revision。
- [ ] 增加 GAME_MSG allowlist golden：离线、重连、场景、道具广播以及 `0x7402` 内 OUT_CARD/CARD_ACTION/USER_STATE_CHANGE 分别进入唯一类型化 Command；身份冲突、未知内层 ID、投票、骰子和坏 suffix 不进入 Battle。断言 codec 与输出 switch 不含 `0x7200` 或 `0x8655`，所有正常客户端输出走 `0x8644`，CARD_ACTION 成功仍产生 `0x7611`。
- [ ] 增加放弃项结构测试：生产 codec/Command 表中不存在 PLAYER_LIMIT 与 UpdatePlayerInfo；旧 GM 错误嵌套的 PLAYER_LIMIT fixture 作为未知 GAME_MSG 被丢弃并计量，不修改玩家、输出或 Revision。不要为未来 kick/continue/mini-game 限制建立占位接口。
- [ ] 增加托管状态测试：自动领出选择最小单张、跟牌只过；普通玩家首次超时切为托管并广播后立即行动，后续走托管期限；人工动作、重连和场景恢复取消托管并 fence 当前旧 AI。主动开启托管时，非当前玩家不触发动作；当前玩家剩余期限大于 100ms 时取消原期限并立即执行，小于等于 100ms 时只保留原到期 Command；状态变化严格产生一次广播和一次定向确认。TimeoutAutoMove=false 时到期不出牌、不创建新期限、不触发 Host 隔离或清理，字段缺失默认 true。
- [ ] 先用 RFC 写清 Lobby/Match/Table/Seat/Turn 的唯一状态 owner，以及 ready、发牌、操作、超时、托管、断线、结束和结算 Command。
- [ ] 先写一个完整确定性对局测试；输入固定牌堆、随机种子和 Clock，不使用 `sleep` 推进规则。
- [ ] 先写 Battle 构造依赖测试：生产 seed 只能来自 `crypto/rand`，失败使创建失败；固定 seed 产生固定洗牌/庄家/新手路径；两个 Battle 不共享随机序列；玩法代码不直接使用包级 `math/rand`、`time.Now` 或 `time.Since`。
- [ ] 先写 GameRule 表驱动测试：三个保留字段分别覆盖完整、缺失、空值、非法值和多余字段，断言缺失/坏值沿用默认、配置创建后不可变；偏置洗牌和其他放弃项不进入 `NHSKConfig`。
- [ ] 先写 BaseRule 归一化表：INIT 独立 Fee、局数、计分和身份字段覆盖任何重复索引；只解析 index 1/6/22，缺失、空值、坏值分别保持 OfflineAutoUsesAI=false、TimeoutAutoMove=true 和进程 RobotLevel（目标默认 2）。断言 NHSKConfig 不含其他历史索引，客户端 GameResult 到 GM GameOver 之间也不存在 BaseRule index 10 的额外展示 Timer；诊断只有脱敏摘要。
- [ ] 先写综合结算状态测试：每小局只发送一次 `0x8650`；`AwaitingSettlement` 拒绝玩法和下一局；合法但身份/内容不匹配的响应保持等待；正确响应只推进一次 `FinalizingReplay`；重复/迟到响应忽略；不存在重试 Timer、结算超时、RequestID 或额外关联号；GM 断线收敛未隔离 Battle。
- [ ] 先写下一小局协调测试：完成回放和 `GAME_OVER` 后只进入 `SubgameFinished`；GM 随后的 `COMMAND CONTINUE` 被接受但不改变状态、不启动玩法；只有 `PrepareSubgame` 成功冻结 `UPDATE_GAME` 的 `GameNum/SubGameNum` 后，`StartSubgame` 才能进入 `Playing`。可选 `UpdateRoundContext` 只覆盖 `START_NEW_GAME` 的 SecRoundTotal/SecRoundUsed/RoomInfo pending 值，不改变阶段、输出或 gameplay Revision；StartSubgame 冻结给当前回放，之后更新只影响下一局，首次默认 0/0/空且不按 Clock 推算 Used。自动下一局不要求它。缺少准备、重复 START、旧小局身份和乱序输入稳定拒绝；Cluster 共用这些 Command，不存在合并启动或 `ContinueBattle` API。Battle 不自行开始下一局或释放实例。
- [ ] 增加 RoundContext 回放测试：同值 UpdateRoundContext no-op；START 前最后一次更新同时进入 MatchInfo 与 GameRule 节点，START 后更新不改变当前文档；下一小局读取新值。Legacy 与 Cluster Send 结果一致且没有 Reply、客户端输出或 gameplay Revision。
- [ ] 增加回放 GameRule 投影测试：BaseRule index 11/15/38/49 的完整、缺失、空值和非法值只生成不可变 ReplayRuleSnapshot，不出现在 NHSKConfig 或玩法分支；TimeoutAutoMove 复用同一玩法值。SecRoundTotal 为 0 时只输出 GameNum=MaxGameNum，非零时只输出 GameTime，另外五项固定属性始终存在。结构测试禁止恢复超时解散、语音或随机换座 handler。
- [ ] 增加回放玩家与文本 golden：UPDATE_PLAYER 乱序到达也按 SeatID 0..3 输出 Player0..Player3 和 Dress D1..D4，精确保留 UserID、UserName、InitScore、Platform 与 Dress；有效 UTF-8 昵称不变，GBK 字节转 UTF-8，坏编码有明确失败/替代测试。`golang.org/x/text` 只能被回放文本 helper 引用，Battle、Runtime 和 Legacy codec 不得引用旧 nbgame_core 编码包。
- [ ] 增加空 RecordGameStart 兼容测试：回放首个 Move 仍为 Deal，XML 不出现 BiasedShuffling、SingleCountSwap、BankerSeat 或虚构 GameStart；SingleCountToSwap 只由玩法 golden 和脱敏诊断覆盖。
- [ ] 增加回放结束时间测试：结算结果提交时只读一次 fake Clock 并冻结 SubgameEndedAt；EndTime 使用该 Unix 秒，跨秒值不能污染其他字段。ReplayBuilder、NHSK 玩法和统计代码不得直接调用 time.Now/time.Since；Ask 与每次合法动作间的 Clock 差按毫秒累计到对应座位，出牌和过牌都覆盖。
- [ ] 增加根 GameOver golden：固定 Scale=Game、Reason=Success、EndReason/OverCode/OverUserID/OverStatus 为 0、RecordValid=1，并覆盖三种 ResultType。Chair0..3 按座位验证换算 Score、综合结算 TotalScore、Result、IsSeal/IsBreak/IsAuto/IsWin、CatchScore、Multiple；普通与 MATCH_STOP 成功路径都不为已放弃结束原因建立状态。
- [ ] 增加 Summary/CardDetail golden：四座依次输出固定六类 Actions、获胜 PaiXing、本局 SumScore/WinCount/Double RoundStat，最后 S4 汇总 OutCount/AutoOutCount/MoveTime；所有实际炸弹按座位与出牌顺序进入 Other/CardDetail。断言不存在跨小局战绩累计依赖。
- [ ] 更新 replay runner 输入兼容测试与 writer golden 的边界：writer 的 Moves 不出现 GameStart 或 GameOver，首个动作仍为 Deal；旧 fixture 可以作为 reader 兼容输入，但不得驱动 writer 补造这两个 Move。
- [ ] 增加回放 Moves 逐动作 golden：Deal 只增加一次 Count 且 D0..D3 按座位；计分出牌固定 CurrentPoint→OutCard，过牌只有空 Cards/不出 OutCard；墩末固定 OutCard→可选 CatchPoint→TurnEnd。逐项断言 ChairID/UserID/Cards/Point/MSec/Scores、六种中文牌型和玩家/AI/超时/托管/系统 Actor，普通超时与 AI 超时都映射“超时”。
- [ ] 增加回放生产 API 结构测试：不存在 PickCard、Offline Move、Reconnect Move 或公开任意 AddMove；Prop 只能经 RecordPropUse。Moves.Count 等于 M 节点数量而非 Deal 子节点数。
- [ ] 增加完整 XML byte golden：包含标准 XML Header、Tab 缩进、属性名字典序、`0x%02x` 小写牌值且无额外尾部字节；writer 的 TurnEnd 只使用逗号 Scores，reader 对旧 Score_0..3 的兼容不得反向污染输出。
- [ ] 增加 ReplayBuilder 完整树序 golden：根严格为 Info/Moves/GameOver/Summary/Dress/Other，Info 为 MatchInfo/GameInfo/Players，GameInfo 为 GameRule/GameSetting；Dress D1..D4 和 CardDetail P0..P3 即使值或明细为空也存在。生产 package 不含 PlayersPre。
- [ ] 增加 ReplayDocument 冻结测试：结果节点完成后才补 Moves/Summary Count；Name、RelativePath、XMLBytes 均深拷贝，冻结后修改 builder 测试对象不能改变已提交文档。ReplayWriterRunner 输入类型不得含 XMLNode、builder、Battle、ServiceContext 或可变 callback。
- [ ] 增加序列化失败收敛测试：客户端 GameResult 已提交，编码失败不提交 writer、不隔离 Battle、不生成第二次结果，直接以既定 ReplayName 产生一次 GM GameOver 并进入 SubgameFinished；不得为纯内存编码启动 goroutine。
- [ ] 增加回放字段级文本测试：MatchName/UserName 分别覆盖合法 UTF-8 与 GBK；GameName 不重复转换。RoomInfo 空值不生成节点，非空 JSON/普通文本原样进入 Json 属性；RoomInfo、Dress、PropID 中 XML 元字符被标准 escaping，非法编码按 Go replacement 行为稳定输出。结构测试禁止全局 text normalizer，日志捕获断言不含原文。
- [ ] 增加 Prop Move byte golden：空/单个/重复/乱序 TargetIDs 分别生成原顺序逗号串，尾部 NUL 由 Legacy decoder 裁掉，Count/PropID/dwSenderID 精确；节点无 Actor，且在 Mailbox 中与相邻玩法 Move 保持到达顺序。
- [ ] 增加 ReplayPlayerSnapshot 跨阶段测试：START 前最后一次 UPDATE_PLAYER/DRESS 冻结 SeatID/UserID/NickName/InitScore/CltID/Dress；START 后更新当前玩家但不改变当前 XML，下一小局才读到新值。CntID 永不进入回放，CltID=0 与非零均原样成为 Platform。
- [ ] 增加结算分数时间线 golden：当前 Players.InitScore 固定为开局分，GM 的 UPDATE_PLAYER 在 0x8650 ACK 前更新 Battle 分数但不回写当前节点，Chair.TotalScore 精确取 ACK，下一局 Players.InitScore 才等于更新值；不得由冻结时延迟读取同一个 Player.Score。
- [ ] 增加发牌单一快照测试：自定义牌堆、新手调整和普通散牌三条路径各自产生一次最终四座牌序；Replay D0..D3 与四份私有 Deal 对相应座位逐字节相等，后续手牌 mutation 不改变已记录 Deal。
- [ ] 增加 ReplayName owner 测试：NHSK replay package 私有固定 prefix=NHSK，GameDescriptor 和 ReplayWriterConfig 均无 prefix 字段；名称精确使用 ProductID、RoundID、同一 SubgameStartedAt 的 UTC+8 日期/时间和 ReplayPlayerSnapshot Seat0UserID。
- [ ] 增加 ReplayUID/FuPanUID golden：同一 ReplayUID 同时进入 XML Info.UniCode 和客户端 ResultDetail.FuPanUID[64]，覆盖零填充与超长截断；0x8650 请求 golden 中不存在该字段。GAME_STARTED/GAME_OVER 使用 ReplayName 而非 ReplayUID，结构测试禁止两者互相解析或拼接。
- [ ] 增加无效回放开关放弃测试：生产配置、NHSKConfig、ReplayWriterConfig、Battle 和 CLI 均不存在 replay_enabled/Enabled；组合根缺失 ReplayWriterRunner 时启动失败，测试用内存 writer。每局始终提交 ReplayDocument，GAME_STARTED/GAME_OVER 没有配置分支。
- [ ] 增加逐局回放失败恢复测试：第一局序列化/队列/超时/写盘失败按契约结束，第二局仍提交一次新文档；没有自动 disable、Noop/Discard writer、熔断或失败状态跨局继承。只有根目录可配，FuPan 日期/小时与 NHSK prefix 结构固定。
- [ ] 增加 ReplayName 生命周期结构测试：StartSubgame 冻结的当前名同时进入 GAME_STARTED、ReplayDocument 和同局 GAME_OVER；下一小局替换后旧名不可再由 Battle 读取。领域结构与公开 API 不存在 replayNames/ReplayNames 历史列表；Legacy GAME_OVER Reason 只在 output DTO 编码，不形成额外 Battle 状态或分支。
- [ ] 先写创建与初始化测试：`NEW_GAME ACK Res=1` 只在 Service 创建并绑定完整 Ref 后产生，Battle 此时为 `AwaitingInit`；相同 INIT 重复 no-op，冲突 INIT 告警拒绝但不隔离，INIT 前 UPDATE_PLAYER/UPDATE_GAME/START 不缓存。UPDATE_PLAYER 可重复 upsert；四个不同非零 UserID 必须占满 `0..3` 四个唯一座位后才允许 START。缺人、重复座位、零用户、旧局号和乱序消息均不进入 Playing。
- [ ] 先写玩家批次与阶段测试：INIT 后 AwaitingInit、Preparing、Playing、AwaitingSettlement、FinalizingReplay 和 SubgameFinished 的 UPDATE_PLAYER 均按契约可达；零 UserID、越界座位、重复用户或冲突座位使整批无变化，省略用户仍保留。Preparing 可随机换座；StartSubgame 至本局完成冻结 UserID/SeatID，局中换人、换座或占用冻结座位整批拒绝、告警但不隔离，Score/Exp/AI/PlayerState/NickName/CltID 仍可更新，CntID 不进入状态。玩法规则不读取 CltID，只有下一局 ReplayPlayerSnapshot 使用；ClientGameOutput 不使用二者。PLAYER_EXIT_GAME 后保留座位但停止定向和广播投递，后续 UPDATE_PLAYER 可重新进入。Flag、PlayerFlag、ScoreChangeReason、ScoreChange、ForceExit 不改变 NHSK 状态；StartSubgame 把 IsBreak/IsSeal 初始化为 false，综合结算 ACK 再按 0x200/0x100 精确赋值，连续两局不会继承旧标记。
- [ ] 增加 ClientGameOutput Legacy 封包 golden：同一广播 payload 以 SeatID 0..3 目标顺序展开四个包，每包精确为外层 `0x8644 GLHeader` 加内层 `0x7400 GameHeader` 和 payload；双层 UserID 等于当前目标，GameInnerID/MatchID/ProductID 精确，CntTID/CltTID/Reserved2 为零。定向输出只生成一包；不存在 UserID=0 广播分支。Cluster adapter 对相同 Targets 只调用自身 SessionRegistry，不读取 Battle 连接字段。
- [ ] 增加 0x8650 整包校验表：成功响应覆盖乱序但完整的四玩家、缺失/未知/重复 UserID、TeamID 不唯一或不等于 SeatID、TeamCount 非 4、交易引用未知 TeamID、负分、零分和重复有向交易。仅完整响应一次性提交分数与 IsBreak/IsSeal；坏包保持 AwaitingSettlement 且无部分状态、客户端输出或回放。失败响应即使附带坏 payload 也直接异常收尾；随后 MATCH_STOP、重复和迟到响应不产生第二次结果、回放或 GAME_OVER。
- [ ] 增加 0x8650 字段 owner 测试：刻意让 PlayerData.Score 与 ResultDetail 交易矩阵不一致，客户端 ResultDetail.Score、回放 Chair.TotalScore 和 GAME_OVER Score 仍只等于交易矩阵计算值，不因冗余值拒绝；PlayerData.Exp/ResultType 改变不影响状态或输出，GAME_OVER Exp 取 ACK 前 UPDATE_PLAYER.Exp。TeamCount 只参与等于 4 的校验，领域模型不存在对应字段。
- [ ] 增加 0x8650 失败 golden：IsSuccess=false 携带任意坏 payload 都废止单飞，展示剩余手牌并跳过第二次结算请求；客户端 ResultDetail 精确为 EndReason=4、Peace、四座 Peace/零分/非破产非封顶，回放 Reason=Success、EndReason=4，随后只生成一次 GAME_OVER。断言诊断记录 SettlementFailed 与脱敏身份但旧 wire 不出现新原因；随后 MATCH_STOP、重复/迟到 ACK no-op。
- [ ] 增加 CompleteSettlement 双入口测试：Legacy BSAck 解码后 Runtime.Send 与 Cluster Send/Call 使用同一 payload、CommandID 和 handler；Send 不要求 Reply，Call 只返回 Accepted/Rejection/Revision。三条入口对成功、坏内容、失败、重复和迟到响应产生相同状态与 GameOutput；Call Reply 不含 GameResult、ReplayDocument、GAME_OVER。结构测试禁止 Battle handler 同步 Call 外部结算，SettlementRequestOutput 不携带或复用 Session。
- [ ] 先写正常删除线序测试：同一连接的 `MATCH_STOP -> DEL_GAME` 使 MATCH_STOP 在 Battle Mailbox 屏障前执行；MATCH_STOP 本身不调用 Runtime Stop、不释放 BattleID。Host 以精确 Ref 进入 `Stopping` 后拒绝新 Resolve，runner 的屏障取消 Timeline、禁止输出并使迟到 AI、自定义牌堆、回放和玩法 Command 无效，但不补做结算或等待外部结果；屏障完成后 Runtime Stop，真实返回前编号不可复用。连接断代走同一路径；隔离条目不投递屏障。补充 writer 先完成与屏障先到两种顺序：前者最多一次 GAME_OVER，后者允许磁盘孤立文件但无 GAME_OVER/NOTICE、无删除/补偿，迟到旧 Ref 不能命中新实例。
- [ ] 实现宁海双扣最小规则、Host/Factory 和 BattleLogic 组合；牌局状态只在 Battle Mailbox 内变化，Host 只拥有路由和创建请求状态。
- [ ] 先写 staged-output seam 测试：NHSKBattleLogic 成功时返回保持顺序、不可变并携带 candidate Revision 的 GameOutput 批次；专用 BattleService 提交 Revision 后才投递。稳定拒绝或 panic 不提交 Revision，也不向 GameOutputService 交付该批次。输出 Service 拒绝或连接失效不回滚 Battle；通用 BattleContext 的 Send/Broadcast/Reply/Timeline 仍保持直接语义。
- [ ] 为宁海双扣等待阶段写 Timeline 测试：每个 `TurnRevision` 恰好只有一个有效 `ActionDeadline`；普通玩家、托管/机器人和外部 AI 使用各自已确认期限。真实机器人 AI 结果立即产生 `AskRespAI`；离线托管结果早到时取消硬期限并只保留剩余 `MsOutCardRobot` 最小延迟，到期应用保存候选；硬期限与最小延迟不得并存。旧 TimelineRevision 不改变状态，不增加 Host 级最长牌局或停滞扫描。
- [ ] 增加未使用 COMMAND 测试：当前 Legacy PAUSE 不创建状态、Timeline 或输出；START、CONTINUE、MATCH_STOP 仍按真实 GM 路径处理。
- [ ] 先写 `AIProvider`/`AIRunner` 测试：请求保存完整 BattleRef、UserID、SeatID、TurnRevision、VerifyCode 和起始时间；有界队列满不阻塞，Close 等待 worker 返回。HTTP golden 固定 POST、content-type、`game_id + data`、base64、小端 envelope、两个 Suffix、`moveMS=1000ms`、Scene 和 AskOutCard；响应 golden 固定 `code/message/data`。响应 Match/Round/内层 VerifyCode 不作权威，SeatID 不匹配、非法候选、旧 Ref/Revision 被拒绝；失败不隔离且不改变 6000ms 硬期限。有效结果按真实机器人/离线托管最小延迟应用；任何日志都不包含 payload 或隐藏手牌。
- [ ] 用 `httptest.Server` 和 golden bytes 验证 Legacy HTTP provider 的 URL、Content-Type、JSON、base64、内嵌 `GAME_SCENE_FOR_AI + ASK_OUT_CARD` 二进制布局及响应解码；未配置 provider 时测试不得访问网络。只使用 Go 标准库。
- [ ] 增加契约测试，断言首版不主动调度 `Deal`、`ContinueDelay`，且 Timeline API 不暴露参考实现未使用的 timer-key 可选参数；以后只有发现真实调用证据并先更新 RFC/golden 行为，才能改变该契约。
- [ ] 增加契约测试，断言 Timeline 清单不包含 `TimerGameOver`；若以后发现真实启动点和处理语义，必须先更新 RFC 和 golden 行为再新增 CommandID。
- [ ] 在 `host_service_test.go` 先写失败测试：达到 `MaxActiveBattles` 后创建返回容量错误；Battle 停止完成前编号不可回收；停止完成后同一 `BattleID` 创建出不同的完整 `BattleRef`；旧 Ref 投递返回 Service 已关闭。
- [ ] 先写异步创建测试：Host 接受后保存 `Creating` 并非阻塞 Submit；Runner 接单不会提前发送成功 ACK；只有 `CreateService + Init` 成功、结果 OperationID/完整 Ref/连接代际匹配且 Host 绑定 Ref 后才进入 `Active` 并产生 `Res=1`。队列满、Create/Init 失败产生 `Res=0` 并释放条目；创建成功但结果无法交回 Host 时 Stop 孤儿 Service；`Creating` 完成前 INIT/START/玩家消息不缓存、不路由。
- [ ] 增加创建重入测试：同连接代际、同 BattleID、相同规范化 payload 在 `Creating` 时只提交一次 Runner、只发送一次最终 ACK；不同 payload 立即失败且不影响原 Operation；只有 `Active` 才走 Legacy 强制替换；`Quarantined` 的同号请求记录告警并回复 `Res=0`，不提交 Stop 或 Create；Cluster 对 `Creating`、`Active`、`Quarantined` 都返回 `ErrBattleIDInUse`。
- [ ] 增加 Legacy Relay golden 测试：入站分别用参考字节覆盖 `BS_MSG_GM2GL_GAME_MSG`、`BS_MSG_GAME_BASE_RELAY` 和内层 `BS_MSG_GC2GS_RELAY`，断言都映射到同一类型化 Command；出站分别覆盖 `BS_MSG_GL2GM_GAME_MSG`、`BS_MSG_GL2GM_GAME_MSG_OLD`。Battle Handler 和 Cluster Command 不得包含外围 Header 或原始 Relay envelope。
- [ ] 先写隔离测试：普通玩法 Command 和 Timeline 到期 Command 中的 panic、不变量错误和 Stop 超时不会删除 Host 条目或释放容量；Timer panic 不被吞掉；只拒绝故障 Battle 的同号创建与业务 Resolve；其他 Battle 继续处理；节点报告 `Degraded`，GM 连接保持；不同编号的新局在容量内仍可创建；`Creating + Active + Stopping + Quarantined` 达到上限后新建失败；GM 后续断线只收敛未隔离条目，`Quarantined` 仍占容量且新代际不能覆盖同号条目；第一次 `DEL_GAME` 保存首次/最近观察时间、计数 1 和 ConnectionGeneration，后续重复消息不改变首次时间，只更新最近时间、增加计数并保存本次代际，且始终不提交 Stop 或释放容量。进入隔离自动非阻塞提交一次导出；队列满为 `ExportPending`，有限重试失败为 `ExportFailed`，成功签发 receipt 为 `Exported`，所有状态均不释放现场且可由本地运维重试。本地 exporter 的部分写入、文件/目录同步或原子改名失败不生成 receipt，临时目录不能被识别为完成导出；完整写出、同步并原子发布后生成绑定 BattleID、完整 Ref 和材料摘要的 receipt，但仍不自动释放；只有三项精确匹配的人工 `ReleaseQuarantinedBattle` 才能局部释放，旧 receipt 不能释放同号新实例。
- [ ] 增加本地运维 CLI 测试：只能列出隔离项、发起/重试导出、携票释放和独立清理诊断目录；释放不删除材料，清理材料不改变 Battle；这些操作不注册 Legacy 消息、不经 GM TCP、不暴露普通 Cluster Service API。
- [ ] 先写诊断测试：失败包包含最后稳定 Snapshot、输入 Record、随机种子、Clock、失败 CommandID、Command Record Sequence、TurnRevision、TimelineRevision、BattleID、完整 Ref、连接代际、panic stack 和 Runtime Inspection；不包含 token、secret、proof 或完整身份材料。
- [ ] 覆盖非法操作、重复操作、超时操作、断线重连快照、旧回合消息、结算重试和进程恢复。
- [ ] 只有两个真实调用方重复同一复杂逻辑时，才将对应边界上移到 `game` 通用模板，并为公开 API 更新 RFC。
- [ ] 运行玩法单测、场景测试和 Record/Replay 一致性测试。
- [ ] 回查 `nhsk`、`gamelogic`、`gamemaster`、`gamecore`、`protocol`、`baison_middle/protocol`、`nbgame_core` 的对应入口与测试，更新参考核对记录；把“`Quarantined` 不响应 `DEL_GAME` 清理、同号 `NewGame` 返回 `Res=0`”标记为 `有意偏差` 并链接 D-038。任何 `发现遗漏` 必须在本 Task 内先补 RFC、测试和实现，不能带入提交。
- [ ] 只有能力兼容矩阵中所有“有使用证据”的条目均为“已一致”或已链接确认的“有意偏差”，完整 Legacy golden、确定性整局、重连、结算和故障验收全部通过后，才把里程碑标记为“可替换旧 GameLogic”；最小可玩切片单独标记，不能混用。
- [ ] 提交：`feat(nhsk): 实现宁海双扣完整示例`。

### Task 9：装配多角色进程与多个游戏实例

该任务不属于第一阶段 GameLogic 替换，从当前可执行计划移出。替换 GameMaster、Agent、Auth/Login/Gateway 时分别新增 RFC 和实施计划；不得预建 `cmd/nhsk-*`、`app/nhsk` 或空节点目录。每个后续节点仍以自己的 `examples/<vertical-slice>` 组合根验证，再根据两个以上真实节点的重复压力决定是否上移公共装配能力。

### Task 10：宁海双扣模板复用评审

第一阶段收尾只记录 `examples/nhsk` 内部边界，不把单一玩法实现上移到 `game`。正式模板复用评审推迟到第二个完整玩法出现；届时再修改 `RFC-0370`，并以两个真实调用方共同承受的变化为证据。

### Task 11：故障、性能与交付验收

**Files:**

- Create: `docs/benchmarks/<date>-cardgame-runtime.md`
- Modify: `README.md`
- Modify: `CHANGELOG.md`
- Modify: `docs/TODO.md`
- Modify: `docs/rfcs/RFC-0500-Roadmap.md`

- [ ] 建立 Legacy GM 连接、单桌、多桌、Host 路由、Battle Mailbox 和出站队列的延迟/吞吐/分配基线，记录固定机器和配置。
- [ ] 增加 Battle 生命周期 churn 测试和 benchmark：连续创建、运行、停止并复用 `BattleID`，断言活动 Service 数始终不超过 `MaxActiveBattles`，完成后 `Runtime.Inspect()` 的 Service、Timer、任务和相关 Metrics 回到基线；记录 `BenchmarkBattleCreateStop` 的 ns/op、allocs/op 和 p95/p99。
- [ ] 使用至少 100,000 次创建/停止循环验证 Registry、Timer、PendingCall、Mailbox 指标和堆内存不随累计 ServiceID 单调增长；若失败先修复生命周期泄漏，不以复用 ServiceRef 掩盖问题。
- [ ] 注入 GM 断线、握手失败、畸形 frame、GameLogic 停止、重复输入和结算失败；证明断线不跨代际重放，业务状态没有双 owner。
- [ ] 注入玩法 panic、状态不变量失败、永不返回 Handler、Stop 超时、Timeline 投递失败、旧 ServiceRef 和错误 TimelineRevision；证明只有具有明确技术错误的 Battle 进入隔离、编号不回收，其他 Battle 与 GM 连接不受影响，并能用诊断包离线重放到同一失败点。单纯推进测试时钟但未触发业务配置的到期条件不得隔离。
- [ ] 汇总所有功能切片的参考核对记录；逐项关闭 `发现遗漏`，并确认每个 `有意偏差` 都链接已接受 RFC 和决策编号。未完成核对不得进入 Phase 收尾。
- [ ] 运行 `go test ./...`、`go vet ./...`、`go test -race ./...`、场景重复测试、AST goroutine 门禁和 `git diff --check`。
- [ ] 更新 README 的本地启动、配置、迁移、运行一局、故障排查和敏感配置说明。
- [ ] 在 Phase 收尾说明实际业务作用、明确非目标，以及第二玩法或生产部署下一阶段为什么仍需要。
- [ ] 提交：`docs(cardgame): 完成棋牌游戏示例验收`。

## 5. 当前不做

- 不修改 Core Runtime 来感知数据库、Redis、微信、socket 或棋牌游戏术语。
- 不先做通用 ORM、通用缓存框架、全局依赖容器或万能 Agent。
- 不把 Redis 当作钱包账本或完整牌局的默认权威来源。
- 不预创建固定 BattleService 槽位，不重置已完成 Service 承载下一局，也不复用 ServiceRef。
- 不自动清理、重启或覆盖发生程序缺陷的 Battle；诊断材料未确认保存前不释放隔离编号。
- 不在玩法和容量目标未确认前承诺分库分表、跨地域容灾、自动扩缩容或无损热迁移。
- 不把测试 Handshake、内存 Registry 或 `MemoryLedgerStore` 宣称为生产实现。

## 6. 下一轮澄清

已建立能力矩阵、Legacy MessageID 矩阵和逐切片参考核对记录。切换范围只接受真实运行、目标部署启用、录包或用户明确要求作为使用证据；约局、旁观、回放上传、偏置、惩罚、战绩、投票、GL 骰子定座、Robot relay、暂停和无当前发送点的遗留消息均已放弃。下一步只冻结保留项的 golden bytes，再细化 Task 1 的 RFC 字段级契约。
