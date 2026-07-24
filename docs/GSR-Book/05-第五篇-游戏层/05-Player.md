# Player 与 PlayerModule：长期状态的组合边界

PlayerService 往往活得比一局 Battle 久，也比一条连接久。它适合拥有玩家长期在线投影和可组合模块。

## PlayerService 状态

```text
SessionIdentity
Online / Generation
Room Ref
Battle Ref
Reconnect projection
PlayerModule states
```

连接属于 Gateway，Player 只保存已认证后的业务身份和当前代际。

## 创建

```go
player, err := game.NewPlayerService(game.PlayerConfig{
    Identity: game.SessionIdentity{
        Player:  "alice",
        Account: "account-1",
    },
    Modules: []game.PlayerModule{
        &inventoryModule{},
        &questModule{},
    },
})
```

模块名必须稳定且唯一。各模块 Command 不能与 Player 保留 Command 或其他模块冲突。

## PlayerModule

```go
type PlayerModule interface {
    Name() string
    Commands() []gsr.CommandID
    Handle(PlayerContext, gsr.Command) error
    HandleEvent(PlayerContext, PlayerEvent) error
    Snapshot(PlayerContext) ([]byte, error)
}
```

Module 不是 Service：

- 没有独立 Ref；
- 跟随 Player 生命周期；
- 状态仍由 Player Mailbox 串行；
- 不能持有其他 Service 指针；
- 不能创建 goroutine。

## PlayerContext

```go
type PlayerContext interface {
    gsr.CommandContext
    PlayerID() PlayerID
    AccountID() AccountID
    Now() time.Time
    Send(gsr.ServiceRef, gsr.CommandID, any) error
}
```

Module 可以回复当前 Command，或向其他 Ref 发送 Command，但得不到 Create/Stop/Resolve。

Handler 返回或 panic 后，Reply/Send 返回 `ErrContextExpired`。

## Event

PlayerService 按模块名排序分发：

```text
PlayerActivated
PlayerOnline
PlayerOffline
PlayerBackup
```

顺序稳定便于测试和重放。一个模块返回 error 后，后续模块不继续执行。

## 在线代际

```go
type PlayerPresence struct {
    Identity   SessionIdentity
    Generation string
}
```

新连接 Online 时保存 Generation。Offline 必须带同一个 Generation：

```text
generation 8 online
迟到 generation 7 offline -> 忽略
generation 8 offline -> 生效
```

这避免旧连接关闭事件把新连接踢下线。

## 绑定 Room 与 Battle

```go
type PlayerBinding struct {
    RequestID RequestID
    Ref       gsr.ServiceRef
}
```

绑定按 RequestID 幂等。Player 保存 Ref 作为当前投影，不直接修改 Room/Battle 状态。

## 重连

Battle 或外层 mapper 获取 BattleSnapshot 后，生成玩家可见投影：

```go
ApplyPlayerReconnectSnapshotCommand
```

Player 保存 bytes 副本。它不读取 Battle 指针，也不让 PlayerModule访问 Battle 内部 map。

## Backup

`BackupPlayerCommand` 让各 Module 在 Player Mailbox 内生成 Snapshot bytes。外部持久化仍由 Handler 外的 Manager/Store 完成。

## 一个模块例子

```go
type scoreModule struct {
    score int
}

func (*scoreModule) Name() string { return "score" }
func (*scoreModule) Commands() []gsr.CommandID {
    return []gsr.CommandID{AddScoreCommand}
}
func (m *scoreModule) Handle(ctx game.PlayerContext, cmd gsr.Command) error {
    m.score += cmd.Payload.(int)
    return ctx.Reply(m.score)
}
func (*scoreModule) HandleEvent(game.PlayerContext, game.PlayerEvent) error {
    return nil
}
func (m *scoreModule) Snapshot(game.PlayerContext) ([]byte, error) {
    return json.Marshal(m.score)
}
```

生产代码应补 payload 验证。

## 对照源码

- `game/player.go`
- `game/module.go`
- `game/room_player_test.go`
- RFC-0340、RFC-0350

## 本章小结

PlayerService 为长期玩家状态提供单一 owner；PlayerModule 提供进程内组合，不增加地址和生命周期。两者通过 PlayerContext 保持同一 Command 语义。
