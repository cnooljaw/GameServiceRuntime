# Game Layer：业务模板不是第二套 Runtime

“既然底层已经有 Send 和 Call，上层是不是应该全部包装成 `battle.Start()`、`player.Login()`、`wallet.AddCoin()`？”

“可以提供业务函数，但不能让调用者忘记消息和失败边界。”

## 业务层的位置

```text
game/
  battle.go
  timeline.go
  room.go
  player.go
  module.go
  wallet.go
  ledger_runner.go
```

它只依赖 Core 公开接口，不反向修改 Runtime。

## 四类 owner

| Service | 拥有的权威状态 | 生命周期 |
| --- | --- | --- |
| Battle | 单局阶段、参与者、Timeline、玩法状态 | 短 |
| Room | 成员与 Battle 索引 | 中 |
| Player | 玩家在线状态、绑定、Module 状态 | 长 |
| Wallet | 结算请求状态与余额投影 | 长 |

LedgerStore 才是资金持久事实。Wallet 内存是工作流 owner 和投影。

## 直接保留 Command 原语

组合根和 Service 仍使用：

```go
runtime.Send(ref, command, payload)
runtime.Call(ctx, ref, command, payload)
commandContext.Reply(value)
```

选择规则：

```text
不需要当前结果 -> Send
需要当前结果   -> Call
Handler 返回   -> Reply
```

Room 和 Wallet 没有插件式 Logic 边界，因此不增加只会转发的 `RoomContext`、`WalletContext`。

## 为什么 Battle 和 Player 有 Context

BattleLogic 不应该获得：

```text
CreateService
Stop
Resolve
Discovery
Cluster
```

它只需要当前 Command 和 Battle 能力：

```go
type BattleContext interface {
    gsr.CommandContext
    BattleID() BattleID
    Now() time.Time
    Timeline() Timeline
    Finish(FinishBattle) error
    Broadcast(gsr.CommandID, any) (BroadcastResult, error)
    Send(gsr.ServiceRef, gsr.CommandID, any) error
}
```

PlayerContext 同理，为 PlayerModule 增加 PlayerID、AccountID、Now 和 Send。

这是一层领域能力边界，不是隐藏 Runtime 的 RPC 框架。

## Context 有效期

BattleContext 和 PlayerContext 只在当前 Handler 有效。无论正常返回还是 panic 展开，写能力都会失效：

```go
err := saved.Send(target, command, payload)
// game.ErrContextExpired
```

禁止保存 Context、传给 goroutine 或在 Handler 外继续调度 Timeline。

## RequestID

跨 Service 工作流必须带 RequestID：

```go
type PlayerBinding struct {
    RequestID RequestID
    Ref       gsr.ServiceRef
}
```

同 RequestID、同内容：幂等返回已有结果。

同 RequestID、不同内容：`ErrRequestConflict`。

## 副本

公开 Snapshot 中的 map、slice 和 bytes 都必须复制。调用方修改返回值不能改变 Service 内部状态。

## 非目标

`game/` 不提供：

- ORM 和数据库连接；
- 网络协议；
- 通用 ECS；
- 自动创建所有业务 Service；
- 跨 Service 原子事务；
- 业务 goroutine。

## 对照源码

- 总契约：`docs/rfcs/RFC-0300-Business-Layering.md`
- 类型：`game/types.go`
- 统一错误：`game/errors.go`
- 示例：`examples/whackmole/`

## 本章小结

Business Layer 的目标不是让 Runtime 看起来“更像本地对象”，而是给常见游戏 owner 提供一套不破坏 Command、Mailbox 和生命周期语义的模板。
