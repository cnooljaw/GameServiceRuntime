# RFC-0400：打地鼠示例

> 状态：已接受
> 接受日期：2026-07-24
> 实现日期：2026-07-24
> 目标阶段：Phase 13
> 范围：Business Layer、Examples
> 依赖：[RFC-0280](RFC-0280-Tooling-Command-Record-Replay.md)、[RFC-0310](RFC-0310-Business-Battle.md)、[RFC-0320](RFC-0320-Business-Timeline.md)、[RFC-0330](RFC-0330-Business-Room.md)、[RFC-0340](RFC-0340-Business-PlayerService.md)、[RFC-0360](RFC-0360-Business-WalletService.md)
> 依据：用小玩法验证完整 Command、Timer、幂等结算与受控 Stop 链路

## 目的

打地鼠是 GSR 的第一个端到端可执行示例。它故意保持规则很小：房间创建一局 Battle，Timeline 生成/过期地鼠，玩家通过 Call 点击，结算异步写 Wallet，Room 收到完成通知，组合根最后 Stop Battle。示例用于验证边界，不是生产游戏服务器。

## 目标

- 提供 `examples/whackmole` 可运行的 composition root、BattleLogic、内存 Ledger 和可重复测试。
- 覆盖 Room → Battle → Timeline → Kick → Settlement → Stop、玩家重连 Snapshot 与 Record/Replay。
- 用固定 seed、虚拟/可控 Clock 的业务输入使回放可重现，不依赖睡眠猜测 timer 顺序。

## 非目标

- 不提供网络协议、图形客户端、匹配、排行榜、持久 Wallet、反作弊、多 Battle 调度、进程退出或自动恢复。
- 不把 example CommandID 当作通用游戏 API，也不使 Core/Tooling 引用该包。

## 分层与依赖

```text
test/application composition root
  -> RoomService + Factory -> WhackMole BattleService
  -> Timeline -> BattleLogic
  -> WalletService + MemoryLedgerStore
  -> Room notification -> external Runtime.Stop(BattleRef)
```

测试中的 Gateway/ProtocolMapper 只构造 `KickRequest`；它不能直接访问 BattleLogic map。示例的 `BattleLogic` 位于 BattleService 内，所有状态变化经 Battle Command；Wallet 与 Room 仅通过 ServiceRef/Command 通信。

## 公开契约

包路径为 `examples/whackmole`，仅供示例/测试：

```go
type ShrewID uint64
type ShrewState string
const (
    ShrewVisible ShrewState = "visible"
    ShrewHit     ShrewState = "hit"
    ShrewExpired ShrewState = "expired"
)
type KickRequest struct {
    Player PlayerID
    Shrew  ShrewID
    Epoch  BattleEpoch
}
type KickResult struct { Hit bool; Score int64; Reason string }
type Snapshot struct {
    Battle BattleSnapshot
    Shrews map[ShrewID]ShrewState
    Scores map[PlayerID]int64
}
const (
    StartCommand       gsr.CommandID = 0x04000101
    SpawnCommand       gsr.CommandID = 0x04000102
    KickCommand        gsr.CommandID = 0x04000103
    ExpireCommand      gsr.CommandID = 0x04000104
    FinishCommand      gsr.CommandID = 0x04000105
    GetSnapshotCommand gsr.CommandID = 0x04000106
)
```

Kick 只能在 BattleRunning、Epoch 相等、Player 为参与者且 ShrewVisible 时命中；第一个有效 Kick 原子地标记 Hit、加一分并返回 `KickResult{Hit:true}`。同一 Shrew 的后续/迟到 Kick 返回 `Hit:false`，不改变分数。Spawn 分配递增 ShrewID，使用 Timeline After 固定 TTL 投递 Expire；Expire 仅对仍 Visible 的 Shrew 生效。Start、Finish 和 Timer 私有 Command 不能从外部协议直接伪造。

## 状态与生命周期

Room 成功创建 Battle 后，composition root 使用 `Runtime.Send` 投递通用 Start 和玩法 Start；它们不需要当前结果。随后客户端输入使用 `Runtime.Call` 投递 Kick 并取得 `KickResult`。同一 Battle Mailbox 保证先接受的 Start 在 Kick 前完成，Logic 中的 `ctx.Reply` 对 Send 是成功无副作用。Start 使用固定 RandomSeed 选择/生成首个 Shrew 并调度下一项；每个 Spawn/Expire/Kick 都写入 Logic 私有状态并可取得 Snapshot。Finish 冻结所有 Player 分数为 `SettlementRequest`，转入 BattleSettling；Wallet committed 后 BattleFinished，并向 Room 发送 `{BattleID, Ref}`。外层读取 Finished Snapshot、导出 Record 后调用 `Runtime.Stop`；Battle Handler 绝不 Stop 自己。

重连以 PlayerService/Mapper Call `GetBattleSnapshot`，返回 BattleEpoch、Timeline Snapshot、玩家可见 Shrew/Scores 投影；它不重新生成 timer 或重置分数。Record Bundle 初始状态在 Start 后捕获，记录每个 Battle 输入（包括 Timeline fire 和随机 seed Command）。

## 错误与失败语义

无效 Player/Shrew/Epoch、Battle 未运行、重复 Start/Finish 和非法 Command 产生稳定业务拒绝。Timer 迟到/重复、Expire 已 Hit 与 Kick 已 Expired 都是正常 no-op。Wallet reject/unknown 使 Battle 保持 Failed/Settling，Room 不删除索引，外层不得 Stop；只有 all committed 后才 Finished。Call 超时由 caller 查询 Snapshot/Settlement，不重发不同 RequestID。

## 并发与所有权

Shrew、Score、seed、next ID 和 timeline 全部属于一个 BattleLogic/Service 的 Mailbox；Room、Player、Wallet 不直接改它们。测试不得以 `time.Sleep` 推进规则，也不得由 example Service 创建 goroutine。所有 Snapshot map/bytes 为副本。示例基准分别测量一个 Battle 的连续 Kick 与多个独立 Battle 的并行 Kick；它们用于显示单局串行热点和按 Battle 分片，不把临时数字视为生产容量承诺。

## 可观测性

测试输出/Record 至少携带 BattleID、Epoch、ShrewID、PlayerID、Timeline Revision、RequestID 和结算状态。示例故意使用 MemoryLedgerStore，输出明确标记为非持久；真实余额、身份凭据或网络 payload 不写入 Record。

## 验收

- 正常路径从 Room 创建到 Battle Stop 完成，并验证 Stop 在 composition root，而不在 Handler。
- 竞争 Kick 只命中一次；Expire/Kick 顺序、旧 Epoch、重复 Finish、Wallet reject/unknown 和重连 Snapshot 都有场景测试。
- 相同 seed + 初始 Snapshot + Record Bundle 在隔离 Runtime 重放得到相同 Scores/Shrew 终态；Core/Tooling 的 import 图不含 examples。
