# RFC-0330：Room 设计

> 状态：草案
> 目标阶段：Phase 12
> 范围：Business Layer
> 依赖：[RFC-0300](RFC-0300-Business-Layering.md)、[RFC-0310](RFC-0310-Business-Battle.md)
> 依据：`docs/learn/007-Game-Service-Runtime详细设计与实现.md`

## 目的

本文定义 RoomService 的职责。

## 职责

Room 负责组织玩家、提供房间入口和创建 Battle。

它不负责 Battle 内部规则，也不拥有 Battle 的强一致状态。

GSR 第一版淡化桌子概念。棋牌中的“桌子、座位、准备、重连、当前局”默认属于 `BattleService`，不是单独的 `TableService`。

## 流程

```text
Match success
  ↓
RoomService
  ↓
CmdRequestBattleCreation(RequestID)
  ↓
Business composition root / FactoryService
  ↓
BattleService
  ↓
CmdBattleCreated(RequestID, BattleRef)
```

RoomService 不持有完整 Runtime，也不在 Handler 内直接调用 `Runtime.CreateService`。Service 创建能力属于组合根；如需要从 Command 流程触发，应使用一个边界明确的 FactoryService，并把创建结果作为后续 Command 返回。

## 状态

```go
type RoomState struct {
    RoomID  RoomID
    Battles map[BattleID]ServiceRef
}
```

## 规则

1. Room 保存 BattleRef，不保存 Battle 指针。
2. Room 不处理 Battle 内部 Timeline。
3. Room 不写 Battle 的参与者、座位、准备、重连和当前局状态。
4. Room 接收 Battle 结束通知，并更新自己的索引或统计。
5. Room 可以是长期服务，也可以是临时服务，按游戏模式决定。
6. Room 发起创建或加入 Battle 后，只更新自己的索引和分配状态；参与者、座位、准备和重连的最终写入由 BattleService 处理。
7. Room 不以用户锁或桌子锁串行 Battle 操作。需要避免重复创建时，使用 Room 自己的 Mailbox 状态和请求幂等键。

## Command

建议：

```text
CmdCreateRoom
CmdJoinRoom
CmdLeaveRoom
CmdStartBattle
CmdBattleFinished
```
