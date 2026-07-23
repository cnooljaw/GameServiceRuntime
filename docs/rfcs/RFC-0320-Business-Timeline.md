# RFC-0320：Timeline 设计

> 状态：待实现
> 目标阶段：Phase 12
> 范围：Business Layer
> 依赖：[RFC-0170](RFC-0170-Core-Timer.md)、[RFC-0300](RFC-0300-Business-Layering.md)、[RFC-0310](RFC-0310-Business-Battle.md)
> 依据：Core Timer 只投递 Command，业务取消必须以代际 fencing 表达

## 目的

Timeline 将一个 Battle 内的业务计时意图转换为 Core `After` 投递的 Command。它解决“回合超时、生成物过期、延迟结算”等需要取消/替换的业务计时，不为 Core 添加 Timeline、timer callback 或跨 Service 定时器。

## 目标

- 在 BattleService 内维护 TimelineID、Revision、原始 Command 与取消状态。
- 每个 timer 只在到达时投递私有 `TimelineFire` Command；Battle 验证 fencing 后才把原 Command 交给 Logic。
- 支持显式 After、At、Cancel、Replace 和只读 Snapshot；取消不依赖 Core Timer 的物理取消能力。

## 非目标

- 不跨 Battle 调度、不执行任意 callback、不直接修改业务状态、不保证墙钟精确性，也不把 TimelineID 暴露为全局唯一 ID。
- 不承诺 Runtime 停止后保留 timer；重启恢复由 Snapshot/业务逻辑重新安排。

## 分层与依赖

```text
BattleLogic -> BattleContext.Timeline()
  -> BattleService owns Timeline state
  -> ServiceContext.After(delay, TimelineFire, envelope)
  -> BattleService validates id/revision/epoch -> Logic command
```

Timeline 不持有 Runtime、ServiceRef 或 goroutine；它只借用当前 Battle Handle 的 ServiceContext。Timer 触发后仍通过 Battle 的 Mailbox，所以不会绕过 Battle 的串行状态边界。

## 公开契约

包路径为 `game`：

```go
type TimelineState string
const (
    TimelineScheduled TimelineState = "scheduled"
    TimelineCancelled TimelineState = "cancelled"
    TimelineFired     TimelineState = "fired"
)
type TimelineItem struct {
    ID       TimelineID
    Revision TimelineRevision
    DueAt    time.Time
    Command  gsr.CommandID
    State    TimelineState
}
type TimelineSnapshot struct {
    NextID TimelineID
    Items  []TimelineItem
}
type Timeline interface {
    After(time.Duration, gsr.CommandID, any) (TimelineID, error)
    At(time.Time, gsr.CommandID, any) (TimelineID, error)
    Replace(TimelineID, time.Duration, gsr.CommandID, any) (TimelineRevision, error)
    Cancel(TimelineID) bool
    Snapshot() TimelineSnapshot
}
```

delay 必须非负；At 早于 BattleContext.Now 时按零 delay 调度；Command 不能是 `TimelineFire`、保留 Battle Command 或零值。After/At 产生新的 ID、Revision=1。Replace 只允许 scheduled Item，使其 Revision 加一且投递一个新的 Core timer；Cancel 只改变状态并返回是否实际取消。所有返回 payload 必须在投递前深拷贝或由调用方提供不可变值；第一版只接受 JSON 可编码 payload，并在配置不合法时失败，而不是延迟到 timer 到达。

私有 `TimelineFire` payload 固定为 `{BattleID, Epoch, ID, Revision, Command, Payload}`；它不得通过 Gateway 或 Codec 作为外部业务 Command 编码。

## 状态与生命周期

Item 的合法转移为 `scheduled -> fired` 或 `scheduled -> cancelled`；Replace 使旧 Revision 逻辑上取消并创建同 ID 的新 scheduled Revision。Fire 仅在 BattleID、Epoch、ID、Revision、State 和 Command 全匹配时生效，先标记 fired，再交给 Logic；任一不匹配即静默计数并忽略。Battle 进入 Finished/Failed 时取消全部 scheduled Item。

Snapshot 以 Item ID 排序，包含非终态和可诊断的终态项；实现可按固定上限淘汰最旧 fired/cancelled 项，但不得淘汰 scheduled 项。

## 错误与失败语义

无效 delay/at/command/payload、未知或终态 ID、或在 Battle Handle 外调用 Timeline 均返回稳定错误。Core `After` 返回错误时不创建 Item。Core timer 允许迟到、重复或在物理取消后仍到达；Revision fencing 保证它不改变业务状态。Runtime Stop 前未到达的 Item 不会自动补偿。

## 并发与所有权

Timeline 状态仅由 BattleService Mailbox 修改；Timeline 对象和 Snapshot 返回副本都不能在 Handler 外保存。没有 goroutine、callback、channel 或共享锁。Timer payload 不携带 ServiceContext、Service 指针或可变 map。

## 可观测性

Battle Snapshot/Record 包含 TimelineID、Revision、DueAt、State 与忽略事件计数。日志只记录业务 ID、CommandID 和原因；不记录完整 payload。Core Timer 指标仍由 Runtime Inspect 提供。

## 验收

- After/At 到达后只以 Command 进入 Battle，Logic 从不被 timer goroutine 直接调用。
- Cancel、Replace、旧 Revision、旧 Epoch、重复/迟到 fire、After 失败和 Battle 终态清理均覆盖测试。
- 排序 Snapshot 与返回副本在 race 检查下不暴露可变内部状态。
