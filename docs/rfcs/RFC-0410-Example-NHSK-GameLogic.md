# RFC-0410：宁海双扣 GameLogic 替换示例

> 状态：待实现
> 目标阶段：Phase 14
> 范围：Business Layer、Examples、Legacy Adapter
> 依赖：[RFC-0110](RFC-0110-Core-ServiceRef.md)、[RFC-0130](RFC-0130-Core-Send-Call-Reply.md)、[RFC-0180](RFC-0180-Core-Lifecycle.md)、[RFC-0280](RFC-0280-Tooling-Command-Record-Replay.md)、[RFC-0310](RFC-0310-Business-Battle.md)、[RFC-0320](RFC-0320-Business-Timeline.md)、[RFC-0370](RFC-0370-Business-Templates.md)
> 依据：`nhsk`、`gamelogic`、`gamemaster`、`gamecore`、`protocol`、`baison_middle/protocol`、`nbgame_core` 的可达源码与测试

## 目的

本文定义 GSR 第一个完整棋牌游戏纵向切片。它先替换旧系统的 GameLogic，保持旧 GameMaster、Agent、客户端和 Legacy TCP 二进制协议不变。同一套宁海双扣规则同时通过 GSR `Send`/`Call` 开放给 Cluster Service，不建立第二套玩法实现。

本 RFC 是 Phase 14 中 NHSK GameLogic 切片的设计与公开 API 权威。[`RFC-0500`](RFC-0500-Roadmap.md) 只记录实施顺序；[`nhsk-capability-matrix`](../reviews/nhsk-capability-matrix.md)、[`nhsk-legacy-message-matrix`](../reviews/nhsk-legacy-message-matrix.md) 和 [`nhsk-reference-reconciliation`](../reviews/nhsk-reference-reconciliation.md) 只记录证据与核对结果，不定义第二份契约。

## 目标

- 以一个 `BattleService` 串行拥有一桌宁海双扣的玩家、手牌、回合、Timeline、结算和回放事实。
- 以 `NHSKHostService` 拥有 `BattleID -> BattleRef` 索引、容量、异步创建/停止操作和隔离条目。
- 保持单条 GameLogic→GameMaster 主动 TCP 连接、双向 origin、8 KiB frame 上限和已确认的 MessageID/二进制布局。
- Legacy TCP 与 Cluster `Send`/`Call` 映射到同一套类型化 Command，并进入同一 Battle Mailbox。
- 保留 NHSK 当前可达的规则、客户端输出、综合结算、回放 XML、外部 AI 和白名单自定义牌堆。
- 每个可验证切片都回查参考仓库，并记录“已一致、有意偏差、发现遗漏”。

## 非目标

- 本阶段不替换 GameMaster、Agent、Gateway、Login 或 Auth，也不改变客户端协议。
- 不引入 Protobuf、通用 RPC façade、MessageID→CommandID 算术转换或另一套 Cluster 玩法逻辑。
- 不让 MySQL 或 Redis 接管牌局权威状态。两者在 Phase 14 只提供独立工具模块。
- 不实现无目标使用证据的约局、旁观、投票、回放上传、战绩、惩罚、GL 骰子定座、偏置洗牌、Robot relay、暂停和无调用点的遗留消息。
- 不为未来宁海麻将、新 GameMaster 或整场回放索引预造通用 Catalog、Manager 或空 adapter。

## 参考核对规则

参考仓库是知识来源，不是新系统的运行时依赖。生产代码不引用旧 `gamelogic`、`gamemaster`、`gamecore`、`nbgame_core` 或其全局单例。`baison_middle/protocol` 和上层 `protocol` 仅用于产生、解读和复核 golden fixture；新生产 codec 在 `examples/nhsk/internal/legacywire` 实现最小子集。

每完成一个功能切片，实施者必须：

1. 核对对应参考入口、调用链和现有测试。
2. 把结论写入 `docs/reviews/nhsk-reference-reconciliation.md`。
3. 将与本 RFC 一致的结论标记为“已一致”。
4. 有意偏差必须链接已有决策。未裁决的偏差或遗漏必须先停止实现并修订 RFC。

## 分层与依赖

```text
LegacyGMConnection (socket/frame/origin/single writer)
  -> LegacyBridgeService (decode/identity/map Command)
       -> NHSKHostService (BattleID index/lifecycle/quarantine)
            -> BattleService (mailbox authoritative state)
                 -> NHSKBattleLogic (rules + immutable outputs)
                 -> Timeline
                 -> GameOutputService (one per connection generation)
                 -> bounded AI/CustomDeck/Replay runners
```

| 模块 | 拥有 | 不得拥有 |
|---|---|---|
| `LegacyGMConnection` | socket、frame buffer、ConnectionGeneration、origin 状态、有界写队列 | Battle 状态、玩法规则、跨代重放 |
| `LegacyBridgeService` | Legacy codec、冗余身份核对、MessageID→CommandID 映射、当前连接代际的派生路由缓存 | 牌局权威状态、原始字节透传到 Battle |
| `NHSKHostService` | BattleID 索引、容量、创建/停止操作、隔离元数据 | 手牌、出牌、结算细节、直接 Runtime 生命周期调用 |
| `BattleService` | 四座玩家、阶段、规则、Timeline、结算、回放事实和窄业务 Revision | socket、Legacy header、其他 Service 指针、直接 goroutine、通用 BattleRevision |
| `GameOutputService` | 已提交输出批次的代际、顺序和 sink 交付 | 修改 Battle 状态、跨连接代际重放 |
| 外部 runner | 有界任务、I/O 生命周期、取消与结果 Command | 在 Handler 外修改 Battle，保存 `ServiceContext` |

Core Runtime 不得引用 `examples/nhsk`、Legacy wire 或任何宁海双扣类型。Battle 只保存其他 Service 的完整 `ServiceRef`，不保存 Service 对象指针。

## 公开契约

### 身份与实例

```go
type ConnectionGeneration uint64
type HostOperationID uint64

type GameDescriptor struct {
    GameID   uint32
    GameName string
}

var NHSKDescriptor = GameDescriptor{
    GameID:   82,
    GameName: "宁海双扣",
}
```

`BattleID` 沿用 `game.BattleID` 的 `uint32` 表示，0 无效，默认编号池为 `1..10000` 且可配置。第一阶段由旧 GM 分配 GameInnerID，Host 不自行生成业务编号。运行实例使用当前节点进程内有效的 `ServiceRef{NodeID, ServiceID}`；不定义 BattleEpoch。节点断线或重启后，Host 名字与业务关系重新解析，不恢复旧 BattleRef。

NHSK Host 以稳定 Service 名 `.nhsk-game-host` 发布。Cluster 调用者先解析该名字，再用 `ResolveBattle` 取得当前完整 Battle Ref；Host 不替调用者代理玩法 Command。

Legacy `NEW_GAME` 只有 `GameID=82` 才能进入本例的 Host；其他玩法 ID 在 Legacy adapter 边界拒绝并返回失败 ACK。Cluster 入口不携带 GameID，因为调用者已经选择了 NHSK Host/Factory。

`InitializeBattleRequest.Rules` 是可选的 `*NHSKConfig`。Legacy adapter 从旧 `INIT_GAME` 的 `BaseRule/GameRule` suffix 生成规则部分；旧 wire 没有本例当前消费的期限字段时，期限采用 `DefaultNHSKConfig`，直接 Cluster 调用可以显式覆盖。`NHSKConfig` 只包含 Battle 实际读取的出牌期限、托管 AI/超时托管、机器人等级、机器人出牌阈值和单牌换牌数量。Raw 规则字符串、比赛名称及其他 GM-owned 索引不进入 Battle 权威状态；未知、缺失或坏值在 adapter 归一化时沿用默认。

`HostOperationID` 在当前 Host Service 实例内单调且不复用，0 无效。它只关联异步创建/停止结果，不代替 BattleID 或 ServiceRef。

### CommandID

CommandID 不复用 Legacy MessageID。已导出常量是 Cluster Service 的公开 API；`0x0410f0xx` 常量保持包内私有。

| CommandID | 名称 | 目标 | 调用模式 | Reply |
|---:|---|---|---|---|
| `0x04100101` | `BeginCreateBattle` | Host | Send/Call | `CreateBattleOperation` |
| `0x04100102` | `GetCreateBattleOperation` | Host | Call | `CreateBattleOperation` |
| `0x04100103` | `ResolveBattle` | Host | Call | `ResolveBattleResult` |
| `0x04100104` | `RequestDeleteBattle` | Host | Send/Call | `HostCommandResult` |
| `0x04100105` | `GetHostSnapshot` | Host | Call | `HostSnapshot` |
| `0x04100201` | `InitializeBattle` | Battle | Send/Call | `CommandResult` |
| `0x04100202` | `UpdatePlayers` | Battle | Send/Call | `CommandResult` |
| `0x04100203` | `PrepareSubgame` | Battle | Send/Call | `CommandResult` |
| `0x04100204` | `StartSubgame` | Battle | Send/Call | `CommandResult` |
| `0x04100205` | `UpdateRoundContext` | Battle | Send | 无 |
| `0x04100206` | `ExitPlayer` | Battle | Send/Call | `CommandResult` |
| `0x04100207` | `UpdatePlayerDress` | Battle | Send | 无 |
| `0x04100208` | `ForceFinishSubgame` | Battle | Send/Call | `CommandResult` |
| `0x04100209` | `ProvideCustomDeck` | Battle | Send/Call | `CommandResult` |
| `0x04100301` | `PlayCards` | Battle | Send/Call | `ActionResult` |
| `0x04100302` | `PreviewCardSelection` | Battle | Send/Call | `ActionResult` |
| `0x04100303` | `SetPlayerAutoState` | Battle | Send/Call | `CommandResult` |
| `0x04100304` | `SetPlayerOffline` | Battle | Send | 无 |
| `0x04100305` | `ReconnectPlayer` | Battle | Send/Call | `CommandResult` |
| `0x04100306` | `RequestGameScene` | Battle | Send/Call | `CommandResult` |
| `0x04100307` | `RecordPropUse` | Battle | Send | 无 |
| `0x04100401` | `CompleteSettlement` | Battle | Send/Call | `SettlementCommandResult` |
| `0x04100402` | `GetNHSKBattleSnapshot` | Battle | Call | `NHSKBattleSnapshot` |

玩法 Command 的公开 Go 契约为：

```go
const (
    PlayCardsCommand            gsr.CommandID = 0x04100301
    PreviewCardSelectionCommand gsr.CommandID = 0x04100302
    SetPlayerAutoStateCommand   gsr.CommandID = 0x04100303
)

type PlayCardsRequest struct {
    Player     game.PlayerID
    Cards      []byte
    VerifyCode uint32
}

type PreviewCardSelectionRequest struct {
    Player game.PlayerID
    Cards  []byte
}

type SetPlayerAutoStateRequest struct {
    Player  game.PlayerID
    Enabled bool
}
```

`Cards` 是领域列表，不暴露 Legacy 固定数组或重复的 CardCount。Command payload 进入 Send/Call 后按不可变值使用；Legacy mapper 必须为它分配独立 slice，不保留 frame 缓冲区。`PlayCardsRequest` 允许携带 0..26 张 Legacy 输入，Battle 再应用 8 张玩法上限及完整合法性校验。`PreviewCardSelectionRequest` 允许 0..26 张，只应用已冻结的宽松预览门禁。

`SetPlayerAutoStateRequest` 只表达 NHSK 实际消费的托管开关。Legacy `0x720A USER_STATE_CHANGE` 固定为 32 字节 BSHeader、uint32 UserId 和 uint32 State；State bit 0 映射 Enabled，其他 bit 按参考行为忽略，不进入第二份玩家状态。payload UserId 必须非零并与 relay 外层和 GameHeader UserID 一致。结构或身份冲突使当前 frame 静默丢弃，不产生 Command 或错误包。

`examples/nhsk` 是可导入的 `package nhsk`，Cluster 调用方直接引用上述 CommandID 和 request。可执行组合根后续放在独立 `cmd/` 目录；不得把该业务包改成不可导入的 `package main`，也不为导出常量增加一层转发 package。

包内私有 CommandID 固定为：

| CommandID | 名称 | owner |
|---:|---|---|
| `0x0410f001` | `applyBattleCreated` | Host lifecycle runner result |
| `0x0410f002` | `applyBattleStopped` | Host lifecycle runner result |
| `0x0410f003` | `timelineFired` | Battle Timeline |
| `0x0410f004` | `applyAIResult` | AI runner result |
| `0x0410f006` | `applyReplayResult` | Replay writer result |
| `0x0410f007` | `applyDeleteBarrier` | Host/Battle stop barrier |
| `0x0410f008` | `legacyContinue` | Legacy no-op ordering compatibility |
| `0x0410f009` | `applyDiagnosticExportResult` | diagnostic runner result |
| `0x0410f00a` | `applyQuarantinedReleaseResult` | Host lifecycle runner result |
| `0x0410f00b` | `deliverGameOutputBatch` | GameOutputService |

Legacy bridge 必须使用显式映射表。Envelope `0x7402`、`0x8605`、`0x8644` 不是 Command，不分配 CommandID。Legacy TCP 不等待 GSR Reply；它使用 `Send`。Cluster 调用者需要当前结果时可以对表中标记 Send/Call 的同一 Command 使用 `Call`。

### 通用结果

```go
type Rejection string

type HostCommandResult struct {
    Accepted   bool
    Rejection  Rejection
    OperationID HostOperationID
}

type CommandResult struct {
    Accepted  bool
    Rejection Rejection
}

type ActionResult struct {
    Accepted  bool
    Rejection Rejection
}

type SettlementCommandResult struct {
    Accepted  bool
    Rejection Rejection
}
```

`Reply` 只表达 Command 是否应用。它不携带客户端消息、`GameOutput`、ReplayDocument、GAME_OVER 或 NOTICE。通过 `Send` 到达时，Handler 对同一结果的 `Reply` 无副作用。

`RequestDeleteBattle` 的 Call Reply 只表示 Host 已接受或拒绝停止请求；接受时返回停止 OperationID，不承诺 Battle 已经 Stop。最终释放以 Host Snapshot、ResolveBattle 返回不存在，或对应生命周期结果为准。

### Host 创建与解析

```go
type CreateBattleRequest struct {
    BattleID             game.BattleID
    IsNewbie             bool
    ConnectionGeneration ConnectionGeneration
}

type HostOperationPhase string

const (
    HostOperationCreating  HostOperationPhase = "creating"
    HostOperationCompleted HostOperationPhase = "completed"
    HostOperationFailed    HostOperationPhase = "failed"
)

type CreateBattleOperation struct {
    OperationID HostOperationID
    BattleID    game.BattleID
    Phase       HostOperationPhase
    Ref         gsr.ServiceRef
    Rejection   Rejection
}
```

`BeginCreateBattle` 只在 Host Mailbox 内检查编号、容量和连接代际，并提交有界 `BattleLifecycleRunner`。Runner 在 Handler 外执行 `Runtime.CreateService` 和 Init，再以 `applyBattleCreated` 返回结果。Host 进入 Active 并绑定完整 Ref 后，Legacy bridge 才可以发送 `NEW_GAME ACK Res=1`。

`ResolveBattle` 只返回 Active 条目的当前 Ref。Creating、Stopping、Quarantined 和不存在条目分别返回稳定错误。Cluster 调用者解析后直接向 Battle Ref `Send`/`Call`，Host 不代理玩法 Command。

Legacy bridge 不为每个玩法 frame 同步 Call Host。创建操作完成并准备发送成功 ACK 时，Host 同时把 Active Ref 发布给当前连接代际的 bridge；bridge 只把它缓存为派生路由。进入 Stopping、隔离、同号替换或连接断代时必须先使相应缓存失效。缓存缺失、BattleID→Ref 绑定不匹配或连接代际不匹配的消息直接拒绝并计量，不猜测 ServiceID，也不回退到旧 Ref。BattleID→Ref 的权威映射始终只在 Host。

### Battle 初始化与玩家

NEW_GAME 成功只建立 AwaitingInit 的 Service。`InitializeBattle` 一次性冻结 BattleID、ProductID、MatchID、RoundID、RoundUniCode、局数上限、Fee、ScoreBase/Denominator 和回放 Metadata。完全相同的重复 INIT 是成功 no-op；冲突 INIT 拒绝但不隔离。INIT 前其他 Battle Command 不缓存、不重排。

`UpdatePlayers` 按批次原子 upsert。零 UserID、越界座位、批内重复用户或冲突座位使整批拒绝。UserID、SeatID、Score、Exp、AI、PlayerState、NickName 和 CltID 保存；CltID 只在小局开始时冻结为回放 Platform。CntID、Flag、PlayerFlag、ScoreChangeReason、ScoreChange 和 ForceExit 解码后丢弃。

`UpdatePlayerDress` 是独立的 Send Command。它只覆盖已有玩家的下一小局不透明 Dress 字符串，允许空值清除，不解析 JSON，不产生客户端输出或 gameplay Revision。`StartSubgame` 按 Mailbox 顺序把当时四座 Dress 冻结到当前小局；START 后再次更新只影响下一小局。

`ExitPlayer` 只标记 Exited，不删除身份和座位。后续 `UpdatePlayers` 可以重新激活。StartSubgame 前必须恰有四个非零 UserID，完整占用 SeatID 0..3 且全部非 Exited。本小局开始后到完成前，UserID↔SeatID 关系冻结；局中换人、换座或占用冻结座位使整批拒绝，其他玩家字段仍可更新。

### 小局阶段

```text
AwaitingInit -> Preparing -> Playing -> AwaitingSettlement
             -> FinalizingReplay -> SubgameFinished
```

`PrepareSubgame` 消费 UPDATE_GAME 的 GameNum/SubGameNum；`StartSubgame` 是唯一进入 Playing 的 Command。`UpdateRoundContext` 只更新下一小局的 SecRoundTotal、SecRoundUsed 和 RoomInfo，不改阶段。Legacy CONTINUE 是包内私有 no-op，不对 Cluster 导出。

`UpdateRoundContext` 的 pending 值可以在任意仍存活阶段被覆盖；相同内容不产生额外状态变化。`StartSubgame` 冻结当时值为当前小局回放上下文，START 后到达的更新不能改写当前快照；未收到时保持 `0/0/""`。

`ProvideCustomDeck` 是自定义牌堆的外部推送 API：

```go
type ProvideCustomDeckRequest struct {
    BattleID   game.BattleID
    GameNum    uint16
    SubgameNum uint16
    Catalog    CustomDeckCatalog
}
```

调用方先解析 Battle Ref，在 `PrepareSubgame` 之后、`StartSubgame` 之前以 `Send` 或 `Call` 提供已经转换完成的 `CustomDeckCatalog`。Battle 只接受匹配的 BattleID、当前小局号和 `Preparing` 阶段，随后深拷贝并固定该小局快照；迟到、错局、非法或已开始后的提供不改变状态。没有提供可用 catalog 时按普通发牌路径处理。

该 API 不包含 Redis key、ProductID/GameID 回退、旧文本 grammar、文件路径、enable 或账号白名单。旧系统兼容由外围 bridge 完成：bridge 读取并解析旧 `MakecardConfig` 后调用同一 `ProvideCustomDeck`；直接 Cluster 调用者直接提供 canonical catalog。Battle 不主动读取任何牌堆数据源。

StartSubgame 只读一次 Battle Clock，冻结 SubgameStartedAt、ReplayName、ReplayUID、回放目录和四座 `ReplayPlayerSnapshot`。ReplayName 只属于当前小局，下一局替换；不保存历史列表。

### 玩法与 Timeline

NHSK 使用两副不含大小王的 52 张牌，四座每人 26 张。牌型仅包含单张、对子、三张、三带二和 4～8 张同点炸弹。点数顺序为 `3..K < A < 2`。两组对家固定为 0/2 和 1/3。首出不能过；跟牌可以过。已出完玩家按参考规则跳过，任一组对家都出完时小局结束。抓分、名次、单扣/双扣和 `100/105/200` 阈值逐案例保持参考结果。

每个 TurnRevision 只允许一个有效 ActionDeadline。普通玩家使用首次/普通出牌期限；托管、机器人和外部 AI 使用已冻结的专用期限，专用值为 0 时回退普通期限。不复制参考同时保留自动 Timer 和硬超时 Timer 的双 Timer 模型。Timer 只投递私有 Command，不执行业务回调。无启动点的 Deal、ContinueDelay 和 TimerGameOver 不实现。

`PlayCards` 对手牌、重复牌、牌型、首出/跟牌和压制关系执行参考校验。`PreviewCardSelection` 保持参考宽松语义：只校验 wire 与数组上限，不校验手牌所有权或牌型。

### GameOutput

```go
type OutputPayload interface {
    isNHSKOutputPayload()
}

const (
    OutputGameStart        OutputKind = "game_start"
    OutputGameInfo         OutputKind = "game_info"
    OutputDeal             OutputKind = "deal"
    OutputAskOutCard       OutputKind = "ask_out_card"
    OutputOutCardInfo      OutputKind = "out_card_info"
    OutputTurnEnd          OutputKind = "turn_end"
    OutputShowCards        OutputKind = "show_cards"
    OutputGameResult           OutputKind = "game_result"
    OutputGameScene            OutputKind = "game_scene"
    OutputOutCardRejection     OutputKind = "out_card_rejection"
    OutputCardSelectionPreview OutputKind = "card_selection_preview"
)

type OutCardRejectionReason uint32

const (
    OutCardRejectionCardCount  OutCardRejectionReason = 1
    OutCardRejectionSeat       OutCardRejectionReason = 2
    OutCardRejectionVerifyCode OutCardRejectionReason = 3
    OutCardRejectionCardType   OutCardRejectionReason = 4
    OutCardRejectionPaused     OutCardRejectionReason = 5
)

type GameSceneState uint8

const (
    GameSceneStatePlaying       GameSceneState = 3
    GameSceneStateShowingResult GameSceneState = 4
)

type GameOverReason uint32

const (
    GameOverReasonSuccess GameOverReason = iota
    GameOverReasonEscape
    GameOverReasonOffline
    GameOverReasonException
    GameOverReasonDissolve
)

type SubgameResult uint8

const (
    SubgameResultSingle SubgameResult = iota
    SubgameResultDouble
    SubgameResultPeace
)

type PlayerOutcome uint8

const (
    PlayerOutcomeWin PlayerOutcome = iota
    PlayerOutcomeLoss
    PlayerOutcomePeace
)

type GameStartPayload struct{}

type GameInfoPayload struct {
    OutCardSeconds uint32
    ServiceFee     int32
    Scores         [4]int32
    GameNum        uint16
}

type DealPayload struct {
    Players [4]game.PlayerID
    SeatID  uint8
    Cards   [26]byte
}

type AskOutCardPayload struct {
    ActivePlayer       game.PlayerID
    VerifyCode         uint32
    ActionMilliseconds uint32
}

type OutCardInfoPayload struct {
    Player    game.PlayerID
    Cards     [8]byte
    CardCount uint8
}

type TurnEndPayload struct {
    Winner         game.PlayerID
    CapturedPoints uint32
}

type ShowCardsPayload struct {
    Players    [4]game.PlayerID
    HandCounts [4]uint8
    Cards      [4][26]byte
}

type GameResultPayload struct {
    Reason         GameOverReason
    Players        [4]game.PlayerID
    Automated      [4]bool
    Scores         [4]int32
    Outcomes       [4]PlayerOutcome
    CapturedPoints [4]uint16
    Ranks          [4]uint8
    Result         SubgameResult
    WinningTeam    uint8
    ReplayUID      string
}

type GameScenePlayer struct {
    Player           game.PlayerID
    Automated        bool
    Offline          bool
    HandCards        [26]byte
    HandCount        uint8
    LastPlayedCards  [8]byte
    LastPlayCount    int8
    CapturedPoints   uint16
    Rank             uint8
}

type GameScenePayload struct {
    State               GameSceneState
    ActiveSeat          int8
    PreviousPlayerSeat  int8
    RemainingSeconds    uint32
    TrickScoreCards     [24]byte
    TrickScoreCardCount uint8
    FinishedPlayerCount uint8
    Players             [4]GameScenePlayer
}

type OutCardRejectionPayload struct {
    Reason OutCardRejectionReason
}

type CardSelectionPreviewPayload struct {
    Player    game.PlayerID
    Cards     [26]byte
    CardCount uint8
}

type GameStartedOutput struct {
    ReplayName string
}

type GameOutput interface {
    isNHSKGameOutput()
}

type ClientGameOutput struct {
    Targets []game.PlayerID
    Kind    OutputKind
    Payload OutputPayload
}

type GameOutputBatch struct {
    BattleID             game.BattleID
    MatchID              uint32
    ProductID            uint32
    Ref                  gsr.ServiceRef
    ConnectionGeneration ConnectionGeneration
    Outputs              []GameOutput
}

type ConnectionFailureKind string

const (
    ConnectionFailureOutputSendRejected ConnectionFailureKind = "output_send_rejected"
    ConnectionFailureOutputSinkRejected ConnectionFailureKind = "output_sink_rejected"
)

type ConnectionFailureReporter interface {
    FailConnection(ConnectionGeneration, ConnectionFailureKind)
}
```

`NHSKBattleLogic` 产生类型化不可变输出，不产生 `0x8644`、GLHeader 或 GameHeader。Battle Handler 先在本地计算候选状态与完整输出；规则成功后提交状态，再把批次投递给当前连接代际的 `GameOutputService`。稳定拒绝或提交前 panic 丢弃未提交批次。该局部 staged-output seam 不依赖通用 BattleRevision。Batch 中的 MatchID、ProductID 是 `InitializeBattle` 已冻结身份的输出快照；sink 不回查 Battle，也不另存 BattleID→路由元数据。

`GameOutput` 和 `OutputPayload` 是 NHSK 包拥有的封闭类型集合；导出的具体值类型分别描述客户端输出、GM 控制输出和结算请求。adapter 可以读取这些值，但不能注入任意 payload 或扩展业务变体。实现不得用 `any`、旧 wire struct 或原始字节替代该边界。

Battle 向 GameOutputService 的 `Send` 在进入目标 Mailbox 前同步失败时，报告 `ConnectionFailureOutputSendRejected`。Batch 已进入 GameOutputService，但有界 sink 拒绝提交时，Service 报告 `ConnectionFailureOutputSinkRejected`。Reporter 只接收这两类跨边界失败；socket writer 失败由拥有该 writer 的 LegacyGMConnection 直接关闭当前代际，不再绕回 Reporter。Reporter 必须非阻塞、并发安全且幂等，旧 ConnectionGeneration 的迟到报告不得关闭新连接。

GameOutputService 的 `ServiceSpec` 固定使用 `DiscardMailbox`。连接关闭先禁止新 Batch，再 Stop 当代 OutputService；已经进入 Handler 的一次非阻塞 sink 提交允许收敛，仍排队的 Batch 直接丢弃，不在旧代际关闭过程中继续排空输出。OutputService 不关闭 sink；sink 和 socket 的生命周期只由 LegacyGMConnection 拥有。

Targets 在 Battle Mailbox 内按 SeatID 升序冻结。所有 NHSK 玩法输出只过滤 Exited，不读取 ClientReady；`0x7246 ROUND_STAT` 是唯一例外，额外要求 ClientReady。后续玩家状态变化不撤回已提交 Targets。

Legacy egress 按 Targets 展开为每用户一个 `0x8644 GLHeader + 0x7400 GameHeader + payload`。内外 UserID 都等于目标用户，GameInnerID、MatchID、ProductID 取冻结身份，CntTID、CltTID、Reserved2 为 0。不生成 UserID=0 广播。Cluster/Agent adapter 仅消费 UserID 并使用自身 SessionRegistry 路由。

`OutputGameStart` 使用无字段的 `GameStartPayload`，Legacy 编码为无 body 的 `0x7205 GAME_START`。它仍是普通 `ClientGameOutput`，按已冻结 Targets 逐用户展开，不因为 payload 为空而改用 GM 控制输出或裸 MessageID。

`GameStartedOutput` 是发给协调方的类型化控制事实，不携带客户端 Targets。Legacy adapter 将其编码为 `0x8654 GAME_STARTED`，GameInnerID 取 Batch 的 BattleID、UserID 为 0、Res 固定为 1，ReplayName 按参考 `[80]byte` 的 `copy` 语义零填充或截断。目标业务没有 Res=false 调用点，因此领域 API 不暴露该无用分支。

`OutputGameInfo` 使用独立的 `GameInfoPayload`。四个 Scores 槽位按 SeatID 0..3 排列；它们是四人玩法的固定领域槽位，不是旧 wire struct。Legacy 编码固定为 50 字节 `0x7601 NHSK_GAME_INFO`，字段顺序为 OutCardSec、ServiceFee、四座 GameScores、GameNum，不包含 suffix 或额外配置字段。

`OutputDeal` 使用 `DealPayload{Players, SeatID, Cards}` 表达一名玩家的私有发牌事实。Players 按 SeatID 0..3 冻结且必须是四个非零、互异玩家；SeatID 必须位于 0..3；ClientGameOutput 必须只有一个 Target，且等于 Players[SeatID]。Cards 是该座最终不可变 26 张牌，直接复用玩法发牌和回放 Deal 的同一份牌序。Legacy adapter 编码 `0x7602 NHSK_DEAL` 时保留四个 UserID，只填写 SeatID 对应的 26 字节牌区，其余三座牌区固定为零；不得把完整四手牌交给 adapter 后再按 Target 临时裁剪。

`OutputAskOutCard` 使用 `AskOutCardPayload{ActivePlayer, VerifyCode, ActionMilliseconds}`。ActivePlayer 必须是非零玩家，但不要求出现在 Targets 中：正常出牌机会面向过滤 Exited 后的全桌，场景恢复只在请求者正是当前行动者时定向发送。VerifyCode 必须非零。ActionMilliseconds 映射到旧 wire 的 `SecRemain`，但严格保持参考语义：单位是毫秒，值是该玩家当前配置的允许出牌时长，不是从 ActionDeadline 动态计算的剩余时间；生成输出和场景恢复都不得创建、替换或延长 Timeline。Legacy payload 固定为 36 字节 `0x7603 NHSK_ASK_OUT_CARD`，依次编码 ActivePlayer、VerifyCode、ActionMilliseconds。

`OutputOutCardInfo` 使用 `OutCardInfoPayload{Player, Cards[8], CardCount}` 表达一次已经提交的出牌或过牌事实。Player 必须是非零玩家，但不要求出现在 Targets 中；正常路径面向过滤 Exited 后的全桌广播。CardCount 必须位于 0..8，0 表示过牌；只有 Cards 的前 CardCount 项进入 wire。8 是 NHSK 合法牌组上限，领域模型不暴露旧协议预留的 26 张容量。Legacy adapter 将其编码为固定 55 字节 `0x7604 NHSK_OUT_CARD_INFO`：Player、26 字节 CardData 和 CardCount，未使用的 CardData 保持零。

`OutputTurnEnd` 使用 `TurnEndPayload{Winner, CapturedPoints}` 表达一墩已经结算完成。Winner 是取得该墩的非零玩家，但不要求出现在 Targets 中；CapturedPoints 是本墩刚归属给 Winner 的抓分，允许为 0，不是玩家累计 Point。Legacy payload 固定为 32 字节 `0x7605 NHSK_TURN_END`，依次编码 Winner 和 CapturedPoints。它只在最后一个 `OutCardInfo` 之后、下一次 `AskOutCard` 之前广播，不负责更新累计分、重置墩状态或推进 Timeline。

`OutputShowCards` 使用 `ShowCardsPayload{Players, HandCounts, Cards}` 表达一次按接收者视角冻结的四座手牌展示。Players 按 SeatID 0..3 排列且必须是四个非零、互异玩家；HandCounts 是各座真实剩余张数，必须位于 0..26，即使该座牌面隐藏也保留；Cards 的某一行全零表示该座牌面隐藏，否则只有前 HandCounts 项可以非零，尾部必须为零。玩家先出完且对家仍有牌时，Battle 只把该玩家放入 Targets，并只填写对家 Cards；终局则把过滤 Exited 后的全桌放入 Targets，并填写所有仍持牌玩家的 Cards。Legacy payload 固定为 148 字节 `0x7606 NHSK_SHOW_CARDS`：四个 UserID、四段 26 字节 Cards、四个 CardsCount。它不改变手牌、名次、结算阶段或 Timeline；展示等待由 Battle 的后续阶段和 Timer Command 管理。

`OutputGameResult` 使用 `GameResultPayload` 表达综合结算已经应用后的客户端结果。Players、Automated、Scores、Outcomes、CapturedPoints 和 Ranks 都按 SeatID 0..3 排列；Players 必须是四个非零、互异玩家，Ranks 必须位于 1..4。Reason 只接受 Success、Escape、Offline、Exception、Dissolve；Result 只接受 Single、Double、Peace；当前可达的 Outcomes 只接受 Win、Loss、Peace。WinningTeam 只接受固定对家组 0/1，Result=Peace 时必须为 0。ReplayUID 复用每小局冻结值，Legacy adapter 按 `FuPanUID[64]` 的 Go `copy` 语义零填充或截断，不增加空值或长度校验。Legacy payload 固定为 154 字节 `0x7607 NHSK_GAME_RESULT`：32 字节主消息包含指向 122 字节 ResultDetail 的 suffix；ResultDetail 依次编码 Reason、四座 UserID、Auto、Score、Outcome、CapturedPoints、Rank、Result、WinningTeam、ReplayUID。它不计算输赢、不应用综合结算、不更新玩家分数，也不启动回放；Battle 必须先提交结算结果，再按全桌 Targets 产生该事实，随后才冻结并提交 ReplayDocument。

`OutputGameScene` 使用请求者视角的 `GameScenePayload` 恢复一份完整客户端场景。ClientGameOutput 必须只有一个 Target，且该玩家必须位于 Players。State 只接受当前可达的 Playing(3) 或 ShowingResult(4)；ActiveSeat 与 PreviousPlayerSeat 接受 -1 或 0..3；RemainingSeconds 是当前 ActionDeadline 剩余时长向下取整后的秒数，0 表示没有有效期限，构造场景不得创建、替换或延长 Timeline。TrickScoreCardCount 必须位于 0..24，FinishedPlayerCount 必须位于 0..4。Players 按 SeatID 0..3 排列且玩家非零、互异；Automated/Offline 在 Legacy 中映射为 State bit 1/2；HandCount 为真实剩余张数 0..26，隐藏牌仍保留计数但 HandCards 全零；LastPlayCount 保留 -1=本墩尚无动作、0=过牌、1..8=已出牌，只有正数时 LastPlayedCards 才可非零；Rank 接受 0..4，0 表示尚未出完。所有固定牌区在对应 count 后必须为零。

Legacy payload 固定为 282 字节 `0x7608 NHSK_GAME_SCENE`：44 字节主消息含 Scene suffix offset=44/size=42、PlayerCount=4、Players suffix offset=86/size=196；随后编码 GameScene 和四个 49 字节 Player。它只投递已经冻结的视图，不修改 Offline、Automated、ClientReady、手牌、TurnRevision 或 Timeline。`ReconnectPlayer` 与 `RequestGameScene` 的不同副作用、GameInfo→GameScene→可选 AskOutCard/ShowCards 的恢复线序由 Battle 的 `RestorePlayerView` 负责，不由 Legacy encoder 推断。

`OutputOutCardRejection` 使用 `OutCardRejectionPayload{Reason}` 表达一次真人出牌被稳定拒绝，只允许 CardCount(1)、Seat(2)、VerifyCode(3)、CardType(4)、Paused(5)。它必须只有一个 Target，且该 Target 是被拒绝的请求玩家。Legacy payload 固定为 28 字节 `0x7609 NHSK_OUT_CARD_RESULT`，依次编码 Header 和 uint32 ErrorCode。参考枚举中的 NoError(0) 不可达：成功出牌没有同步 ACK；AI、机器人、托管和 Timeline 自动候选失败也不产生该输出。该事实不修改手牌、托管状态、TurnRevision 或 ActionDeadline，不重发 AskOutCard；错误判定和目标冻结都由 Battle handler 在提交状态前完成，Legacy adapter 只编码。

`OutputCardSelectionPreview` 使用 `CardSelectionPreviewPayload{Player, Cards[26], CardCount}` 表达当前操作玩家的一次非权威选牌预览，Legacy 编码为固定 55 字节 `0x7611 NHSK_CARD_ACTION_WATCH`：Player、26 字节 CardData 和 CardCount。Player 必须非零但不要求位于 Targets；正常路径面向过滤 Exited 后的全桌。CardCount 必须位于 0..26，只有前 CardCount 项进入 wire，允许空列表。为保持参考行为，它不校验牌是否属于手牌、是否重复、是否构成牌型或能否压过上家；这些规则只属于真正的 `PlayCards`。Battle 只在 Playing/WaitingForAction 且 Player 是当前操作人时产生该事实，不迁移无效的 BaseRule 开关。该输出不修改手牌、当前墩、TurnRevision、ActionDeadline 或任何玩家状态。

### 结算与回放

小局结束后产生一次 `SettlementRequestOutput` 并进入 AwaitingSettlement。Legacy `BSAck | 0x8650` 以 `Send(CompleteSettlement)` 完成；Cluster 可以 Send 或 Call 同一 Command。Battle Handler 不在 Mailbox 内同步 Call 外部结算 Service。

成功响应必须完整覆盖当前四名玩家，TeamID 唯一且等于 SeatID，TeamCount=4，交易只引用有效 TeamID，非零 Score 为正数，同一有向交易不重复。坏包整包拒绝并保持 AwaitingSettlement，不部分提交。通过后只消费身份、Flag 和 ResultDetail 交易矩阵；PlayerData.Score/Exp、ResultType 只解码。

当前已实现 Legacy adapter 对 `0x8650` 固定后缀的类型化解码：每条 ResultDetail 为 12 字节 `PayTeamID/GainTeamID/Score`，每条 PlayerData 为 20 字节 `PlayerID/Flag/Score/Exp/TeamID`，两个 suffix 的 offset、长度和记录数必须精确匹配。Battle 在 Mailbox 内先完成四名冻结玩家、TeamID、正分、有效有向键和无重复交易的整包校验，再一次性应用分数与 `Flag & 0x100/0x200` 对应的 IsSeal/IsBreak；任一成功内容失败都保持 AwaitingSettlement。`IsSuccess=false` 忽略附带详情，清零四座分数与标志并以 GameOverReasonDissolve 收敛。旧的最小 Cluster `Scores[4]int32` 形状暂时保留兼容测试/调用，Legacy 真实路径不使用它。

`IsSuccess=false` 保持旧客户端外观：Dissolve(4)、平局、四座零分，但内部另记 `SettlementFailed`。MATCH_STOP 在 Playing/AwaitingSettlement 废止当前 Timeline/结算单飞，按 Success(0) 本地强制收尾；其他阶段 no-op。

客户端 GameResult 先提交，再把不可变 ReplayDocument 交给有界 writer。writer 成功、失败或有界超时中第一个有效结果使 Battle 产生 ROUND_STAT 和 GAME_OVER，然后进入 SubgameFinished。回放失败仍使用已冻结 ReplayName，不隔离 Battle，不原子改名、`fsync`、自动重试或上传。

ROUND_STAT 在已放弃战绩模块的首版保持 PlayerCount=0。GAME_OVER 固定 PlayerCount=4，PlayerDatas 严格按 SeatID 0..3 编码；Score 取交易矩阵结果，Exp 取结算前最新玩家值，Auto 取本局最终托管认定。四座不完整是内部不变量缺陷，不发送错位数组。

## Legacy TCP 契约

### 连接与 frame

GameLogic 启动时主动建立一条到旧 GameMaster 的全双工 TCP 连接。每次物理连接分配新 ConnectionGeneration，先发送 GameLogic origin=107，再读取并校验 GameMaster origin=100，随后创建当代 GameOutputService，才进入 Ready。Ready 前不投递业务 frame。

BSHeader 固定为 24 字节小端：`Magic uint32, Serial uint32, Origine uint16, Reserve uint16, Type uint32, Param uint32, Length uint32`。单帧上限为 8192 字节。`Length < 24`、`Length > 8192` 或无法恢复边界时关闭当前代际；边界完整的未知 MessageID 或坏 body/suffix 只丢弃当前帧、告警和计量。

### 分层 relay

Legacy 入站为：

```text
client -> Agent:       0x7402 + NHSK payload
Agent -> GameMaster:   0x7402 + GameHeader + payload
GameMaster -> GL:      0x8605 GLHeader + 0x7402 GameHeader + payload
```

外层 GameInnerID/UserID 是权威路由身份。内层 UserID 必须一致；INIT 完成后 MatchID/ProductID 也必须等于 Battle 冻结值。CntTID、CltTID、Reserved2 不进入 Command。校验后 adapter 丢弃所有 envelope，Battle 不感知输入来自 TCP 还是 Cluster。

Legacy 出站为：

```text
GL -> GameMaster: 0x8644 GLHeader + 0x7400 GameHeader + payload
```

广播按座位展开为逐用户包。Magic、Reserve、Serial 保持零；Param 只由具体 codec 写入。Header.Length 必须一次写成最终长度，所有 suffix 同时校验固定区下界和当前 frame 上界，不接受尾部多余字节。

### 消息范围

Legacy bridge 只实现能映射到本 RFC Command 的消息。客户端玩法输入只保留 `0x7701 OUT_CARD`、`0x7702 CARD_ACTION`、通用 USER_STATE_CHANGE、`0x7208 USER_RECONNECT` 和 `0x720D GAME_SCENE`。GAME_MSG 外围事实只保留离线、重连、场景和道具成功广播。未知内层 ID 丢弃并计量。无效的 `0x7200` 直传、`0x8655` 旧输出、PLAYER_LIMIT、GAME_END、旧结算 ACK、投票和骰子不进入 codec switch。

`0x7701 OUT_CARD` payload 固定为 55 字节：24 字节 BSHeader、26 字节 CardData、1 字节 CardCount 和末尾 uint32 VerifyCode。codec 要求 Header.Type=`0x7701`、Header.Length=55、CardCount 位于 0..26，且 CardCount 后未使用的固定牌区全部为零；空选择和零 VerifyCode 都是可解码输入。codec 不应用 8 张玩法上限：9..26 张必须进入 `PlayCards`，再由 Battle 对真人产生 CardCount 拒绝；VerifyCode 是否等于当前行动机会也只由 Battle 判断。坏长度、超过 wire 容量的计数或非零尾部整条 payload 丢弃，不产生 Command 或客户端错误包。

`0x7702 CARD_ACTION` payload 固定为 51 字节：24 字节 BSHeader、26 字节 CardData 和末尾 1 字节 CardCount。codec 要求 Header.Type=`0x7702`、Header.Length=51、CardCount 位于 0..26，且 CardCount 后未使用的固定牌区全部为零；空选择有效。Magic、Serial、Origin、Reserve 和 Param 不进入归一化输入。codec 只复制前 CardCount 张牌，不校验重复、手牌归属、牌型或压牌关系；这些玩法门禁由 `PreviewCardSelection` Handler 决定。坏长度、坏计数或非零尾部整条 payload 丢弃，不产生 Command 或客户端错误包。

客户端 NHSK 输出保留 `0x7601..0x7609` 和 `0x7611` 中已确认可达的 GameInfo、Deal、AskOutCard、OutCardInfo、TurnEnd、ShowCards、GameResult、GameScene、OutCardResult 和 CardActionWatch。托管状态继续使用参考代码中的通用消息，不把它伪装成 NHSK `0x76xx` 消息。已放弃的 `0x7610 COMMENTATE_TIME` 和客户端 `0x7612 ROBOT_RELAY` 不因为相邻编号而自动实现；外部 AI adapter 使用的 `0x7612` 只属于其 provider wire。

## 状态与生命周期

Host 条目状态为 Creating、Active、Stopping、Quarantined。`Creating + Active + Stopping + Quarantined` 共同占用 MaxActiveBattles。正常 Cluster 创建遇到任一非终态同号条目都返回 `ErrBattleIDInUse`。Legacy 同号替换只对 Active 保留：先完全 Stop 旧 Ref，再用同一 BattleID 创建新 Ref。Quarantined 绝不自动替换。

DEL_GAME 使 Host 进入 Stopping，先向精确 Battle Ref 投递 Mailbox 删除屏障，再由 runner Stop。屏障后取消 Timeline、禁止新输出并 fence AI、自定义牌堆、结算和回放结果；不等待已开始的磁盘 I/O，允许孤立回放文件，不补发 GAME_OVER/NOTICE。Runtime Stop 真实返回后才删除索引和释放 BattleID。

GM 连接断开后，当代 Creating、Active、Stopping Battle 进入停止收敛，Quarantined 保留。进程不退出，以有上限指数退避和 jitter 持续重连。新连接代际不缓存、重放、迁移或恢复旧牌局。

## 错误与失败语义

稳定拒绝包括：无效 BattleID、编号占用、容量耗尽、未初始化、非法阶段、非参与者、身份冲突、旧 TurnRevision/TimelineRevision/VerifyCode、非法动作、停止中、已隔离和找不到。稳定拒绝不产生未声明的客户端输出。

普通 Handler 或 Timer Handler 的 panic、状态机不变量失败和 Handler/Stop 超时是程序缺陷。它们只使当前 Battle 进入 Quarantined，保留编号和取证状态；其他 Battle 继续，节点报告 Degraded 但继续接收不同编号新局。隔离 Battle 收到 DEL_GAME 只记录外部结束事实，不 Stop、替换或释放。

诊断 runner 自动导出最后稳定 Snapshot、Command Record、seed、Clock、失败 Command、Command Record Sequence、TurnRevision、TimelineRevision、stack 和 Runtime Inspection。只有材料原子发布并产生绑定 BattleID、完整 Ref 和摘要的 receipt 后，本地 CLI 才能精确释放该隔离实例。

Call 超时只表示未及时收到 Reply，不取消已进 Mailbox 的 Command。普通玩法不增加通用 RequestID 或幂等缓存。调用方在超时后先查询 Snapshot，不自动重发原动作。

## 并发与所有权

Battle 业务状态只能在其 Mailbox Handler 中修改。Stop 和 Close 只执行 Runtime 串行清理，不补做结算。Battle、Host 和 adapter Service 不直接创建 goroutine。TCP I/O owner 和固定上限 runner 可按 RFC 约束使用 goroutine，但必须拥有取消/Close 入口并等待真实返回。

Timeline、AI 和 Replay 结果都必须携带完整 BattleRef、小局身份与各自 Revision。`ProvideCustomDeck` 请求携带 BattleID 和小局身份，并由 Battle 在 Mailbox 内执行当前阶段校验。迟到、重复、旧 Ref、旧小局或旧连接代际结果不得修改当前状态。Snapshot、Targets、手牌、CustomDeckCatalog 和 ReplayDocument 对外返回深拷贝或不可变值。

## 可观测性

Runtime 只通过 `Runtime.Inspect()` 提供 Core 观测。NHSK 业务 Snapshot 通过只读 Command 暴露。日志、Record 和诊断至少携带 BattleID、完整 Ref、ConnectionGeneration、Command Record Sequence、Subgame identity、TurnRevision、TimelineRevision、CommandID 和稳定错误类别。

日志不记录 token、secret、proof、微信 code、完整身份材料或未脱敏结算响应。节点健康必须区分 GM-link NotReady、业务 Ready 和存在 Quarantined 时的 Degraded。

## 验收

- 以参考 formatter 生成的 golden bytes 覆盖双向 origin、NEW_GAME/ACK、INIT、UPDATE_PLAYER、`0x8605+0x7402`、`0x8644+0x7400`、`0x8650`、GAME_STARTED、ROUND_STAT、GAME_OVER、NOTICE 和 DEL_GAME。
- 同一玩法输入分别通过 Legacy `Send` 和 Cluster `Send`/`Call` 进入同一 Battle，Snapshot、窄业务 Revision 和 GameOutput 序列一致；Call Reply 不复制客户端输出。
- 固定 seed 覆盖 104 张牌、牌型、压制、抓分、对家、名次、单扣/双扣、新手换牌、散牌调整、自定义牌堆与外部 AI。
- fake Clock 和 Timeline 覆盖唯一 ActionDeadline、托管/离线最小延迟、超时、迟到 AI、重连与旧 Revision。测试不用 `time.Sleep` 猜测业务顺序。
- 结算覆盖成功、坏响应、失败、重复、迟到、MATCH_STOP、回放成功/失败/超时和 DEL_GAME 竞争。
- 隔离覆盖 Handler/Timer panic、诊断队列满、导出重试、receipt 精确释放、同号创建、GM 断线和容量占用。
- 连接覆盖 origin 失败、半帧、超长帧、坏 suffix、输出队列满、writer 失败、指数退避、稳定重置和连接代际 fencing。
- 100,000 次 Battle 创建/停止 churn 后，Registry、Timer、PendingCall、Mailbox 和 goroutine 回到基线。不预创建或复用 ServiceRef 池。
- `go test ./...`、`go vet ./...`、`go test -race ./...` 和 `git diff --check` 全部通过；Core Runtime 的 import graph 不含 `examples/nhsk`。

## 当前实现进度（2026-08）

本 RFC 是目标契约；当前仓库已经交付的可验证代码包括 `examples/nhsk` 的 Legacy codec/mapper、Host/Factory/Battle Mailbox、TCP connection owner、进程组合根和 typed output seam。当前实现已覆盖：

- `0x7701`、`0x7702`、`0x720A` 的固定字节解码、三层身份核对和显式 Command 映射。
- `0x7208 USER_RECONNECT` 与 `0x720D GAME_SCENE` 的固定布局解码、显式 Command 映射和 Battle 恢复视图边界；Reconnect 与 Scene 保留不同的 Offline/托管副作用，场景按请求者视角隐藏手牌，并在请求者已出完时显示固定对家手牌。
- Battle 已按固定对家组实现出完牌边界：单个玩家出完后继续 Playing、输出定向 `SHOW_CARDS` 并推进下一次 `ASK_OUT_CARD`；同组两座都出完才进入 AwaitingSettlement，输出全桌 `SHOW_CARDS`。
- `InitializeBattle`、`UpdatePlayers`、`PrepareSubgame`、`ProvideCustomDeck`、`StartSubgame`、`PlayCards`、`PreviewCardSelection`、`SetPlayerAutoState` 以及 `GetNHSKBattleSnapshot` 的最小可运行路径。
- `.nhsk-game-host` 的创建操作、BattleRef 解析和 Factory 停止；Legacy relay 与 Cluster 直接调用同一 Battle Mailbox。
- 单条主动 Legacy GM TCP 连接的双向 origin、ConnectionGeneration、bounded output queue、退避重连和 `cmd/gamelogic` 组合根。
- 旧 GM 控制面 `NEW_GAME/INIT_GAME/UPDATE_PLAYER/COMMAND/UPDATE_GAME/START_NEW_GAME/DRESS/PLAYER_EXIT/DEL_GAME/0x80008650` 的固定布局解码、显式 Host/Battle 映射；`NEW_GAME` 等待 Host Operation 完成后编码 `0x800086c0` 成功/失败 ACK，Battle 结算后可发最小 `GAME_STARTED/GAME_OVER`。
- `GameDescriptor` 已成为 Legacy 玩法选择边界：NHSK 组合根固定 `GameID=82`，其他 `NEW_GAME.GameID` 在进入 Host 前拒绝并编码失败 ACK；Cluster 直接使用 NHSK Host，不重复携带 GameID。
- `UpdateRoundContext` 与 `UpdatePlayerDress` 已在 Battle Mailbox 内保留 pending 元数据；START 时冻结当前 RoundContext 和四座 Dress，START 后更新只影响下一局，不产生客户端输出或 gameplay Revision。
- `StartSubgame` 现在以同一次 `NHSKClock` 读取同时设置首个行动期限和内存 `ReplayDocument` 的 `ReplayStartSnapshot`；快照深拷贝 BattleIdentity、RoundContext、四座 User/Seat/NickName/InitScore/CltID(Platform)/Dress/Automated 与最终 26 张手牌。当前已完成起始快照和基础事件内存模型，ReplayUID、规范文件名/目录、规范 Moves 序列化、Summary/CardDetail、XML 和 writer 仍待后续切片。
- `ReplayDocument` 起始时创建一个包含四手最终牌的单一 `Deal` 内存事件；合法出牌按参考顺序记录 `CurrentPoint -> OutCard`，每次出牌/过牌都记录 `OutCard`，三家过牌结束一墩时记录 `CatchPoint -> TurnEnd`。领域值用 `ReplayMove.Source/ReplayMoveSource` 表示来源，不引入 Actor 类型；未来 XML serializer 才把它写成旧属性名 `Actor`。事件和牌区都通过深拷贝返回；当前 MoveMilliseconds、终局 Summary/CardDetail、XML 属性树和 writer 仍未接入。
- Legacy `INIT_GAME` 已按旧固定体的连续 suffix 结构解码 `BaseRule/GameRule/MatchName/RoundUniCode`；adapter 只把 NHSK 实际消费的规则投影为不可变 `NHSKConfig`，直接 Cluster 初始化使用同一类型配置或默认值。当前接入的期限字段保持旧默认 10 秒，BaseRule 的托管 AI、超时托管、机器人等级和 GameRule 的机器人出牌阈值/单牌换牌数量已可达；偏置洗牌等未消费字段仍丢弃。
- `0x80008650` 的 ResultDetail/PlayerData 两段后缀已按旧 12/20 字节布局类型化解码；Legacy 映射与 Cluster `CompleteSettlement` 共用同一 Battle 矩阵门禁，坏包不会部分修改状态，成功 Flag 会更新 IsSeal/IsBreak，失败响应按 Dissolve(4) 清零收敛。完整客户端 GameResult、回放、ROUND_STAT/GAME_OVER 终局时序仍未接入。
- 强制结束小局按参考 `GameOverProcess` 的顺序提交最小 `GAME_OVER (0x8641)` 后的 `NOTICE_ROUND_OVER (0x864e)`；正常 `CompleteSettlement` 不提交 NOTICE。
- 客户端 `ROUND_STAT (0x7246)` 的首版空统计 wire/Legacy relay 已实现；PlayerCount 固定为 0，正式结算时序仍需 GameResult 和回放收敛后接入。
- Battle 已维护 `!Exited && ClientReady` 的 ROUND_STAT 目标资格表，正式结算时序仍需 GameResult 和回放收敛后接入。
- Battle 牌规层已实现参考 `Logic.GetCardType/CompareCardType` 的单牌、对子、三张、三带二和 4～8 张炸弹；A/2 逻辑值、同型比较和炸弹长度优先已锁定测试。每个 Battle 持有独立随机源和 `NHSKClock`：生产创建只从 `crypto/rand` 取种子，测试可注入固定 seed/fake Clock；每小局只抽一次庄家、洗牌一次 104 张标准牌组，普通路径按旧 `SwapSingleCard` 的座位顺序执行 `SingleCountToSwap` 散牌调整，新手路径按旧 `RandCardListByNewPlayer` 对首个非自动玩家执行三张/四张重试，并按庄家座位环形分发；`ProvideCustomDeck` 接收由外围文件/Redis bridge 或 Cluster 调用者转换好的不可变 catalog，命中时覆盖庄家与手牌且绕过普通/新手调整；期限起点和剩余时间只读取该 Clock，期限和 GameInfo 秒数读取已冻结的 `NHSKConfig`。完整机器人/AI 专用期限、回放时间事实和单扣/双扣仍未实现；`SingleCountToSwap<=0` 明确关闭普通路径调整，新手路径不消费该普通阈值。
- Battle 当前墩已按参考累计 5/10/K 抓分；三家过牌后提交 `TurnEnd`，把本墩分值归属给最后出牌者，并清空本墩牌、过牌计数和上次出牌投影；`GameScene` 暴露当前墩牌、上次出牌和累计抓分。
- 连接 Ready 时按 ConnectionGeneration 创建 `GameOutputService` 并绑定 Factory；GM 断线后由 Factory 有界 runner 停止该代际普通 Battle，旧输出不跨代提交。
- Battle 的最小唯一期限 fencing、托管当前玩家自动最小出牌和 `CompleteSettlement` 终态入口。

尚未完成的 RFC 契约包括 `ROUND_STAT` 的结算时序、带玩家数据的 `GAME_OVER`/客户端 GameResult、回放收敛、回放时间事实、名次与单扣双扣、完整综合结算、完整结果/托管责任输出、AI、Quarantine/诊断、Redis 真实联调以及 MySQL/Auth/Gateway/Agent 后续进程。当前墩抓分与 `TurnEnd` 已接入，但不代表最终计分或综合结算完成。实现进度和每次与只读参考目录的核对记录以 `docs/reviews/nhsk-reference-reconciliation.md` 和 `examples/nhsk/README.md` 为准。在这些切片完成前，本例不宣称达到“无损替换旧 GameLogic”的生产验收。

## 实际作用与后续阶段

本切片完成后，旧 GameMaster 可以把整个宁海双扣 GameLogic 进程切换到 GSR，无需同时修改 Agent 或客户端。它证明了 GSR Service、Command、Mailbox、Timeline、adapter 和外部 runner 可承载一个完整棋牌游戏。

本切片不解决 GameMaster 的编号分配、跨局协调、Agent SessionRegistry、认证或权威持久化。后续替换 GameMaster 时，新协调 Service 直接使用本 RFC 的 Host/Battle Command，并删除 Legacy envelope；替换 Agent 时，新 adapter 消费同一 ClientGameOutput Targets 并使用自身 SessionRegistry。
