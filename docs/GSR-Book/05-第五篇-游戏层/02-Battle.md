# Battle：一局游戏，一个串行 owner

BattleService 不知道打地鼠、卡牌或战棋规则。它只管理所有玩法都绕不开的边界。

## 组成

```text
BattleService
  ├── BattleID / Epoch
  ├── Phase
  ├── Participants
  ├── Timeline
  ├── Settlement states
  └── BattleLogic
```

玩法实现 `BattleLogic`：

```go
type BattleLogic interface {
    HandleBattle(BattleContext, gsr.Command) error
    Snapshot(BattleContext) ([]byte, error)
}
```

## 创建

```go
battle, err := game.NewBattleService(game.BattleConfig{
    ID: "battle-42",
    Participants: []game.Participant{
        {Player: "alice", Ref: aliceRef},
        {Player: "bob", Ref: bobRef},
    },
    Wallet: walletRef,
    Logic:  newWhackMoleLogic(7),
})

ref, err := runtime.CreateService(gsr.ServiceSpec{
    Service: battle,
})
```

Battle 不在 Handler 内创建其他 Service。创建属于组合根或显式 Factory Service。

## 状态机

```text
created
  -> StartBattleCommand
running
  -> FinishBattleCommand
settling
  -> all committed
finished

settling
  -> any rejected
failed
```

只有 Running 阶段接受玩法自定义 Command。

## 启动与输入

不需要当前结果时：

```go
err := runtime.Send(ref, game.StartBattleCommand, struct{}{})
err = runtime.Send(ref, StartWhackMoleCommand, struct{}{})
```

需要 Kick 结果时：

```go
value, err := runtime.Call(
    ctx,
    ref,
    KickCommand,
    KickRequest{Player: "alice", Shrew: 1, Epoch: 1},
)
```

Logic 直接：

```go
return ctx.Reply(KickResult{Hit: true, Score: score})
```

BattleContext 会把 Send 到达时没有 Reply 通道的情况归一化为成功无副作用。

## Broadcast

```go
result, err := ctx.Broadcast(StateChangedCommand, update)
```

`error` 只表示 Context 已过期。逐目标投递失败记录在：

```go
BroadcastResult{Delivered: 8, Rejected: 2}
```

已经接受的目标不会因其他目标失败而回滚。

## Finish 与结算

Logic 生成 Battle 内的结算意图：

```go
err := ctx.Finish(game.FinishBattle{
    RequestID: "finish-42",
    Settlements: []game.SettlementIntent{{
        RequestID: "settle-42",
        Currency:  "coin",
        Entries: []game.SettlementEntry{{
            Player: "alice",
            Delta:  10,
        }},
    }},
})
```

BattleService 冻结自身 `Self()` 为 SettlementRequest.Source，再 Send 给 Wallet。它等待 Wallet 的结果 Command，而不是在 Handler 内同步写数据库。

## Context 逃逸

下面的代码即使编译，也违反契约：

```go
type badLogic struct {
    saved game.BattleContext
}
```

Handler 返回后：

```text
Reply / Send / Finish / Broadcast / Timeline schedule
  -> ErrContextExpired
```

panic 展开也会通过 defer 立即失效。

## Snapshot

`GetBattleSnapshotCommand` 返回独立副本：

```go
type BattleSnapshot struct {
    ID           BattleID
    Epoch        BattleEpoch
    Phase        BattlePhase
    Participants map[PlayerID]ParticipantStatus
    Timeline     TimelineSnapshot
    State        []byte
}
```

`State` 来自 Logic，BattleService 再复制。

## 对照源码

- `game/battle.go`
- `game/battle_test.go`
- `docs/rfcs/RFC-0310-Business-Battle.md`

## 本章小结

BattleService 深在于它隐藏了一整套阶段、Timeline、结算和 Context 生命周期，只向玩法暴露少量稳定能力；它浅在于不替玩法决定任何游戏规则。
