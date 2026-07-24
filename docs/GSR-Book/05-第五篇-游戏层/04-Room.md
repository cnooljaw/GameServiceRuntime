# Room：成员与 Battle 索引的 owner

“Room 既然能开始战斗，为什么不直接在 Handler 里 `CreateService`？”

“因为创建实例是组合根的生命周期能力，不是房间状态的一部分。”

## Room 拥有什么

```text
RoomID
Phase
Members
Battle index
Pending create requests
```

Room 不拥有 Battle 内部规则，也不持有 `*BattleService`。

## 创建

```go
room, err := game.NewRoomService(game.RoomConfig{
    ID:         "room-7",
    Capacity:   4,
    Factory:    battleFactory,
    FactoryRef: factoryRef,
})
```

如果只需要 Join/Leave/Query，可以不配置 Factory；这时 Start Battle 返回不支持。

## Join 与 Leave

```go
err := runtime.Send(roomRef, game.JoinRoomCommand, game.PlayerID("alice"))
err = runtime.Send(roomRef, game.LeaveRoomCommand, game.PlayerID("alice"))
```

Room Mailbox 串行维护容量、重复成员和阶段约束。

## 为什么 Factory 是两阶段

Start 请求：

```go
type BattleCreateRequest struct {
    RequestID RequestID
    Room      RoomID
    Players   []PlayerID
}
```

Room Handler 只调用窄 `RoomFactory.RequestBattle`，冻结 pending 事实。外层 Factory 创建 Battle 后，用 Command 返回：

```go
type BattleCreatedResult struct {
    RequestID RequestID
    Battle    BattleID
    Ref       gsr.ServiceRef
}
```

```text
Room Start Command
  -> pending request
  -> Factory / composition root
  -> Runtime.CreateService(Battle)
  -> ApplyBattleCreatedCommand
  -> Room index committed
```

Room 只接受精确 `FactoryRef` 的结果，防止其他 Service 伪造 Battle。

## RequestID 幂等

同一个 RequestID 重复 Start：

- 请求内容相同：返回已有阶段；
- 请求内容不同：`ErrRequestConflict`。

Factory 也必须按 RequestID 避免重复创建。

## Battle 完成

Room 只接受自己索引中 Battle Ref 发来的：

```go
ApplyBattleFinishedCommand
```

它更新房间索引和阶段，不去读取 Battle 对象。

## 为什么没有 RoomContext

Room 没有第三方 Logic seam。固定 Handler 已经直接拿到 `gsr.CommandContext`。

再增加：

```go
type RoomContext interface {
    Reply(any) error
}
```

只会制造一个转发层和新名词，没有缩小真正的权限。Room 直接遵守统一 Send/Call/Reply 语义。

## Snapshot

```go
value, err := runtime.Call(
    ctx,
    roomRef,
    game.GetRoomSnapshotCommand,
    struct{}{},
)
```

Members 和 Battle 索引都返回独立副本。

## 业务场景

适合 RoomService：

- 组队房；
- 棋牌桌的成员与对局索引；
- 副本入口；
- 小队 ready 状态。

如果只是一个无独立寻址需求的 Battle 内分组，不必额外创建 Room Service。

## 对照源码

- `game/room.go`
- `game/room_player_test.go`
- `docs/rfcs/RFC-0330-Business-Room.md`

## 本章小结

Room 的职责是维护“谁在房间、哪一局属于房间”。创建 Battle 的动作交给 Factory owner，玩法状态交给 Battle owner。
