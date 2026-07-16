# RFC-0310：Battle 设计

> 状态：草案  
> 范围：Business Layer  
> 依据：`docs/learn/007-Game-Service-Runtime详细设计与实现.md`

## 目的

本文定义 Battle 的职责和实现方式。

## 定义

Battle 是一组参与者在同一个游戏活动中的权威上下文。

它可以表示：

- 一局麻将。
- 一桌扑克。
- 一场打地鼠。
- 一次匹配后的对战。
- 一个轻量多人玩法实例。

Battle 淡化“桌子”概念。桌子只是棋牌游戏里的表现形式，不是 GSR 的通用业务模型。

Battle 在 Game Layer 中是概念封装，默认落地为一个 `BattleService`。

## 创建

Game Layer API：

```go
battleRef, err := game.CreateBattle(ctx, CreateBattleOptions{
    GameID:  "whack_mole",
    Players: players,
})
```

内部：

```go
runtime.CreateService(BattleServiceSpec{...})
```

## BattleContext

```go
type BattleContext interface {
    CommandContext
    BattleID() BattleID
    Epoch() BattleEpoch
    Players() PlayerCollection
    Timeline() Timeline
    Broadcast(cmd CommandID, payload any) error
}
```

`BattleContext` 属于 Game Layer，不属于 Core Runtime。

## Battle 状态

```go
type BattleState struct {
    BattleID    BattleID
    Epoch       BattleEpoch
    Participants ParticipantState
    Ready       ReadyState
    Reconnect   ReconnectState
    Round       RoundState
    Timeline    TimelineState
}
```

具体游戏可扩展自己的状态。

在棋牌游戏模板中，原来容易被称为 `TableService` 的职责，第一版统一归入 `BattleService`：

```text
BattleService owns:
  participants / seats
  ready state
  reconnect state
  current round state
  action state
  timers
  replay events
```

这样一个 Battle 的强一致状态只有一个 owner，不需要在“桌子锁”和“对局锁”之间协调。

如果某个游戏确实需要保留桌子概念，应把 `Table` 建模为 `BattleState` 内的字段或业务命名，不新增默认 `TableService`。

## 规则

1. Battle 不直接修改 Player 或 Wallet 状态。
2. Battle 通过 `ServiceRef` 与 Player、Wallet 通信。
3. Battle 崩溃默认销毁，不自动重启。
4. Battle Snapshot 主要服务于重连和观战。
5. Battle 内部状态修改必须通过 Command。
6. 一个 Battle 的参与者、座位、准备、重连和当前局状态，默认由同一个 `BattleService` 写。
7. 第一版不拆默认 `TableService`。只有出现明确扩展压力时，才把桌子索引、长期场馆或跨局容器拆出去。
8. Battle 内的所有写操作都由其 Mailbox 串行执行，不增加 seat lock、user lock 或 battle lock。
9. 对 Player、Wallet 等外部状态的请求必须携带 `RequestID` 或等价业务幂等键；Battle 只在收到结果 Command 后推进自身状态。
10. 断线只改变连接可达性，不直接移除参与者。是否托管、超时判负或允许回到下一局，由 Battle 规则决定。

## 跨 Service 协作

Battle 不持有 Player、Wallet、Gateway 的对象指针。它也不在本地状态写到一半时，以 `Call` 等待外部 Service 后继续依赖旧状态完成写入。

推荐将流程拆成可观察的 Command：

```text
CmdFinishRound
  ↓
BattleService 冻结本局结果并生成 SettlementRequest(RequestID)
  ↓
WalletService 幂等结算
  ↓
CmdSettlementCommitted / CmdSettlementRejected
  ↓
BattleService 推进下一状态或进入人工补偿
```

这样重试、超时和重连不会退化成跨 Service 锁竞争。

## 常见 Command

```text
CmdStartBattle
CmdPlayerReconnect
CmdGetSnapshot
CmdFinishBattle
```

具体游戏增加自己的 Command。
