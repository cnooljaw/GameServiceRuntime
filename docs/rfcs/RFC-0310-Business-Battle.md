# RFC-0310：Battle 设计

> 状态：待实现
> 目标阶段：Phase 12
> 范围：Business Layer
> 依赖：[RFC-0300](RFC-0300-Business-Layering.md)、[RFC-0320](RFC-0320-Business-Timeline.md)、[RFC-0360](RFC-0360-Business-WalletService.md)
> 依据：一局游戏的强一致状态应收敛于一个 Mailbox

## 目的

本文定义一次多人游戏活动的最小 BattleService。Battle 是权威的单局上下文：参与者、Epoch、可见状态、业务规则、Timeline 和结算编排都由一个 Service 串行拥有。它适用于棋牌桌、房间内一局、短时实时协作或打地鼠；它不是 Core 的通用 Service 父类。

## 目标

- 组合根可通过 `CreateBattle` 创建拥有稳定 BattleID 和参与者的 BattleService。
- 为游戏规则提供仅在 Handle 期间有效的 BattleContext、广播和 Timeline 能力。
- 定义开始、快照、参与者连接状态、完成和异步结算结果的稳定状态机。
- 以 `RequestID` 与 Wallet 交互，绝不在 Battle Handler 内 Stop 或等待 Wallet 形成分布式事务。

## 非目标

- 不定义座位、回合、胜负、匹配、观战、作弊检测或具体游戏 Command；这些由 `BattleLogic` 和示例定义。
- 不直接修改 Player/Wallet 状态、不持有其 Service 指针、不自动重启 Battle，也不提供跨 Battle 的全局索引。

## 分层与依赖

```text
composition root -> CreateBattle -> BattleService owns battle state
BattleLogic (inside same Service) <- BattleContext -> Timeline / Broadcast
BattleService --Send(RequestID)--> WalletService
Wallet result Command --> BattleService
```

Battle 的外部依赖只保存 ServiceRef。Room 是入口和索引 owner；Player 是长期玩家状态 owner；Wallet 是账本 owner。游戏逻辑对象可以是 BattleService 的私有字段，因为它不跨 Service 共享。

## 公开契约

包路径为 `game`：

```go
type Participant struct {
    Player PlayerID
    Ref    gsr.ServiceRef // optional PlayerService target
}
type ParticipantConnection struct {
    Player    PlayerID
    Connected bool
}
type BattlePhase string
const (
    BattleCreated   BattlePhase = "created"
    BattleRunning   BattlePhase = "running"
    BattleSettling  BattlePhase = "settling"
    BattleFinished  BattlePhase = "finished"
    BattleFailed    BattlePhase = "failed"
)
type ParticipantStatus string
const (
    ParticipantConnected ParticipantStatus = "connected"
    ParticipantOffline   ParticipantStatus = "offline"
)
type BattleSnapshot struct {
    ID           BattleID
    Epoch        BattleEpoch
    Phase        BattlePhase
    Participants map[PlayerID]ParticipantStatus
    Timeline     TimelineSnapshot
    State        []byte
}
type BattleLogic interface {
    Commands() []gsr.CommandID
    HandleBattle(BattleContext, gsr.Command) error
    Snapshot(BattleContext) ([]byte, error)
}
type BattleConfig struct {
    ID           BattleID
    Epoch        BattleEpoch
    Participants []Participant
    Wallet       gsr.ServiceRef
    Logic        BattleLogic
    RandomSeed   uint64
}
type SettlementIntent struct {
    RequestID RequestID
    Currency  Currency
    Entries   []SettlementEntry
}
type FinishBattle struct {
    RequestID   RequestID
    Settlements []SettlementIntent
}
type BattleContext interface {
    gsr.CommandContext
    BattleID() BattleID
    Epoch() BattleEpoch
    Now() time.Time
    Timeline() Timeline
    Broadcast(gsr.CommandID, any) BroadcastResult
    Send(gsr.ServiceRef, gsr.CommandID, any) error
}
type BroadcastResult struct { Delivered, Rejected int }
func NewBattleService(BattleConfig) (*BattleService, error)
func CreateBattle(ServiceCreator, gsr.ServiceName, BattleConfig) (gsr.ServiceRef, error)
```

`BattleConfig.ID`、Logic、Participants 和 Epoch（零值默认 1）必须有效；玩家 ID 不重复，Player Ref 若非零必须有效，Wallet 仅能在需结算的 Logic 中使用。`CreateBattle` 只能由组合根/FactoryService 调用；它创建 `gsr.ServiceSpec`，不在已有 Battle Handler 中调用。Logic.Commands 与下列保留 Command 不得重叠：

```text
0x03000101 StartBattle
0x03000102 GetBattleSnapshot
0x03000103 SetParticipantConnected
0x03000104 FinishBattle
0x03000105 ApplySettlementResult
0x03000201 TimelineFire（私有）
```

Finish payload 为 `FinishBattle{RequestID, Settlements []SettlementIntent}`；Intent 不携带 `Source`，Battle 在自己的 Handler 中冻结并填入 `Self` 后构造 `SettlementRequest`，转为 `BattleSettling` 并 Send 给 Wallet。Wallet 的 `SettlementResult` 以 `ApplySettlementResult` 回到 Battle；同 RequestID 的重复 Finish 返回原阶段，不重复发送。游戏 Logic 自定义 Command 仅在 `BattleRunning` 处理，且不得修改 BattleContext 之外的权威状态。

## 状态与生命周期

Init 固定 Battle 的 Self Ref、Timeline 和 Logic。`StartBattle` 只允许 `created -> running`；`SetParticipantConnected` 只更新连接可达性，不移除参与者；`GetBattleSnapshot` 返回副本。`FinishBattle` 只允许 `running -> settling`，全部 SettlementResult committed 后进入 `finished`，任一 rejected/unknown 进入 `failed` 并保持冻结结果以供人工处理。Stop/Close 不推进业务阶段。

Timeline 事件必须带当前 Epoch/Revision；迟到、取消或旧 Epoch 的事件被忽略，不得触发 Logic。Battle Finished/Failed 后拒绝新的游戏输入，但保留 GetSnapshot 与幂等的结算结果查询。

## 错误与失败语义

无效 ID、重复玩家、未知 Logic Command、错误 Epoch、非法阶段、非参与者输入和重复但不一致 RequestID 分别返回稳定业务错误。Broadcast 是尽力 Send：逐个拒绝被计数并记录，不因一个 Player Ref 关闭而回滚已处理 Battle 状态。Wallet Send 立即失败时 Battle 保持 Settling 并将待发请求保留为可重试 Command；Call 超时或迟到结果不得猜测结算结果。

## 并发与所有权

Battle 状态、Logic 状态和 Timeline 状态仅由 Battle Mailbox 改写。BattleContext、Timeline 和其 payload 只能在当前 Handler 使用；不得保存、传给 goroutine 或在 Handle 返回后继续调用。Broadcast 不持有 PlayerService 指针，只基于 frozen Participant Ref 调用 Send。所有 Snapshot、参与者 map 和 Logic bytes 均深拷贝。

## 可观测性

Battle 日志、Record 和 Snapshot 至少带 BattleID、Epoch、Timeline Revision、RequestID、Phase 与稳定错误码。它不输出 Wallet 余额明细或玩家密钥。每次 ignored timer、广播拒绝和结算 pending/failed 都应可通过业务 Snapshot/记录观察。

## 验收

- 创建、开始、连接变化、游戏 Command、Snapshot 和终态转换均由单一 Mailbox 串行验证。
- 重复 Finish、Wallet 延迟/拒绝、结果迟到、玩家关闭、旧 Timeline 事件和 Broadcast 部分拒绝都有测试。
- CreateBattle 不在 Service Handler 中创建实例；Battle Handler 不调用 Runtime.Stop、不创建 goroutine，也不直接修改 Player/Wallet。
