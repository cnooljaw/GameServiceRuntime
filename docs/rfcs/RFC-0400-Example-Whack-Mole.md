# RFC-0400：打地鼠示例

> 状态：草案  
> 范围：Business Layer、Examples  
> 依据：`docs/learn/007-Game-Service-Runtime详细设计与实现.md`

## 目的

本文定义第一个端到端示例：打地鼠。

该示例用于验证 GSR 的核心模型。

## 服务划分

```text
GateService
RoomService
BattleService
PlayerService
WalletService
```

## Battle Command

```text
CmdStartBattle
CmdSpawnShrew
CmdKickShrew
CmdExpireShrew
CmdGetSnapshot
CmdPlayerReconnect
CmdFinishBattle
```

## BattleState

```go
type BattleState struct {
    BattleID BattleID
    Epoch    BattleEpoch
    Timeline TimelineState
    Players  map[PlayerID]ServiceRef
    Shrews   map[ShrewID]ShrewState
    Score    map[PlayerID]int64
}
```

## 地鼠生成

```text
CmdStartBattle
  ↓
Timeline.After(spawnDelay, CmdSpawnShrew)
  ↓
CmdSpawnShrew
  ↓
Create ShrewState
  ↓
Broadcast CmdShrewSpawned
  ↓
Timeline.After(ttl, CmdExpireShrew)
```

## 玩家点击

```text
Gate receives client input
  ↓
runtime.Call(battleRef, CmdKickShrew, req)
  ↓
Battle validates epoch/timeline/shrew
  ↓
Update score
  ↓
Reply KickResult
  ↓
Broadcast CmdShrewHit
```

## 地鼠过期

```text
Timeline emits CmdExpireShrew
  ↓
Battle checks shrew status
  ↓
Mark expired
  ↓
Broadcast CmdShrewExpired
```

## 对局结束

```text
CmdFinishBattle
  ↓
Calculate settlement
  ↓
Call WalletService CmdCommitSettlement
  ↓
Notify PlayerService
  ↓
Snapshot final state
  ↓
Stop BattleService
```

## 验收

示例必须证明：

1. Battle 可创建和关闭。
2. Timer 通过 Timeline 生成 Command。
3. 玩家点击通过 Call 返回结果。
4. 广播通过 Send 发送。
5. 结算通过 WalletService 完成。
6. 重连能拿到 Snapshot。
