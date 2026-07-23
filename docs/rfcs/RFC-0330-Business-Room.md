# RFC-0330：Room 设计

> 状态：已接受
> 接受日期：2026-07-24
> 实现日期：2026-07-24
> 目标阶段：Phase 12
> 范围：Business Layer
> 依赖：[RFC-0300](RFC-0300-Business-Layering.md)、[RFC-0310](RFC-0310-Business-Battle.md)
> 依据：房间入口与单局强一致状态分属不同 owner

## 目的

RoomService 是房间入口、成员集合和 Battle 索引的 owner。它适用于大厅房间、临时匹配房或长期社交房；它不拥有 Battle 内的座位、准备、回合、Timeline 或规则状态。

## 目标

- 定义创建、加入、离开、开始 Battle、接收创建结果/结束通知和只读查询 Command。
- 由 Room Mailbox 以 RequestID 防止重复创建；Factory/组合根 返回新 Battle Ref 后，再由 Room 写索引。
- 允许没有 Room 的游戏直接 CreateBattle，不把 Room 变成强制中间层。

## 非目标

- 不实现匹配算法、容量调度、跨节点放置、Battle 创建 closure、自动 Stop、桌子锁或 Battle 状态代理。

## 分层与依赖

```text
Gateway/Mapper -> RoomService owns members + battle index
RoomService --creation request(RequestID)--> application FactoryService / composition root
Factory result Command --> RoomService -> BattleService
BattleFinished Command --> RoomService
```

Room 只保存 Battle Ref 和可读摘要，不持有 Battle 对象；Battle 不反写 Room map，而是发送显式通知。Factory 是 application 边界，使用 `CreateBattle` 后通过 Command 返回，不能由 Room Handler 直接创建 Service。

## 公开契约

包路径为 `game`：

```go
type RoomPhase string
const (
    RoomOpen   RoomPhase = "open"
    RoomClosed RoomPhase = "closed"
)
type RoomSnapshot struct {
    ID      RoomID
    Phase   RoomPhase
    Members []PlayerID
    Battles map[BattleID]gsr.ServiceRef
}
type BattleCreateRequest struct {
    RequestID RequestID
    Room      RoomID
    Players   []PlayerID
}
type BattleCreatedResult struct { RequestID RequestID; Battle BattleID; Ref gsr.ServiceRef }
type BattleFinishedNotice struct { Battle BattleID; Ref gsr.ServiceRef }
type RoomFactory interface { RequestBattle(BattleCreateRequest) error }
type RoomConfig struct {
    ID         RoomID
    Capacity   int
    Factory    RoomFactory
    FactoryRef gsr.ServiceRef // required when Factory is non-nil
}
func NewRoomService(RoomConfig) (*RoomService, error)
```

保留 CommandID：

```text
0x03000301 JoinRoom
0x03000302 LeaveRoom
0x03000303 StartRoomBattle
0x03000304 ApplyBattleCreated（仅 Factory/受信任 source）
0x03000305 ApplyBattleFinished（仅已索引 Battle source）
0x03000306 GetRoomSnapshot
```

Join/Leave payload 使用 PlayerID；Start 使用 `BattleCreateRequest`；ApplyBattleCreated 使用 `BattleCreatedResult`；ApplyBattleFinished 使用 `BattleFinishedNotice`。`Factory` 非 nil 时必须同时配置精确 `FactoryRef`，ApplyBattleCreated 只接受该 source；ID/Capacity/Factory、成员重复、未加入玩家、重复 BattleID、零 Ref、超容量与不一致 RequestID 都是稳定错误。Factory 为 nil 时 Room 不支持 Start，但 Join/Leave/Query 仍可用。

## 状态与生命周期

Room Init 后为 Open。Join 在容量允许时加入有序 member 集；重复 Join 无副作用。Leave 移除成员，但不直接修改正在运行 Battle 的参与者。Start 在 Room 中冻结 RequestID 和成员副本为 pending；Factory 成功投递后仍 pending，只有 ApplyBattleCreated 才写 `Battles`。同 RequestID 的相同 Start 返回 pending/已创建事实；不同输入冲突。BattleFinished 仅删除匹配 `(BattleID, Ref)` 的索引，迟到/重复通知无副作用。Close 只使 Room Closed，不停止 Battle。

## 错误与失败语义

Factory 请求同步失败时 pending 被移除，调用方可用同一 RequestID 重试；请求已接受后的超时不表示未创建，必须等待/查询 Apply 结果。未知 Factory source、非索引 Battle source 和错误 Ref 被拒绝。Room 不回滚一个已创建但未回报的 Battle；该孤立实例由 Factory/组合根按业务审计处理。

## 并发与所有权

成员、pending request 和 Battles map 仅在 Room Mailbox 改写。Room 不使用用户/桌子锁，不 Call Battle 来完成 Join/Leave。Snapshot 的成员和 map 是深拷贝，Factory 不得保存 Room 的 ServiceContext 或在其 Handler 中创建 goroutine。

## 可观测性

Room Snapshot 至少显示 RoomID、成员数、pending RequestID 数和 Battle 索引；日志记录 RequestID、BattleID、Ref 和错误类别。成员身份信息或 Factory 私有配置不写入 Record 明文。

## 验收

- 加入/离开、容量、重复 RequestID、Factory 拒绝、创建结果迟到/重复/错误 source 与 Battle 完成均有测试。
- Room 从不直接 Create/Stop Battle，且 Battle 内部状态不回流到 Room。
