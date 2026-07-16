# RFC-0340：PlayerService 设计

> 状态：草案  
> 范围：Business Layer  
> 依据：`docs/learn/007-Game-Service-Runtime详细设计与实现.md`

## 目的

本文定义 PlayerService 的状态边界。

## 职责

PlayerService 是玩家状态 owner。

职责：

- 玩家基础状态。
- 当前 RoomRef 或 BattleRef。
- 重连状态。
- 结算结果应用。
- Snapshot 或持久化。

## 状态

```go
type PlayerState struct {
    PlayerID  PlayerID
    RoomRef   ServiceRef
    BattleRef ServiceRef
    Online    bool
}
```

## 规则

1. Battle 不直接改 PlayerState。
2. PlayerService 通过 Command 接收状态变更。
3. PlayerService 建议支持 Snapshot。
4. PlayerService 崩溃可按策略重启恢复。
5. PlayerService 不直接持有 Battle 指针。
6. PlayerService 的 `Online` 只描述已认证会话的可达性；登录认证、密钥交换和连接 proof 验证不属于 PlayerService。
7. 跨 Service 的加入房间、进入 Battle、结算等请求必须有业务幂等键；PlayerService 不通过用户锁与其他 Service 共享可变状态。

## 重连

流程：

```text
Player reconnect
  ↓
GateService
  ↓
PlayerService
  ↓
BattleRef
  ↓
Call CmdGetSnapshot
```

返回应包含：

- BattleEpoch。
- TimelineRev。
- 玩家个人状态。
- 对局可见状态。
