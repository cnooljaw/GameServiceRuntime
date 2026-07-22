# RFC-0320：Timeline 设计

> 状态：草案
> 目标阶段：Phase 12
> 范围：Business Layer
> 依赖：[RFC-0170](RFC-0170-Core-Timer.md)、[RFC-0310](RFC-0310-Business-Battle.md)
> 依据：`docs/learn/007-Game-Service-Runtime详细设计与实现.md`

## 目的

本文定义游戏时间轴 Timeline。

## 定义

Timeline 是 Business Layer 的时间模型。

它不等于 Core Runtime 的 Timer。

```text
Timeline -> Timer -> Command -> Mailbox
```

## API 草案

```go
type Timeline interface {
    Schedule(at time.Time, cmd CommandID, payload any) (TimelineID, error)
    After(d time.Duration, cmd CommandID, payload any) (TimelineID, error)
    Cancel(id TimelineID) error
    Rev() TimelineRev
}
```

Timeline 是 BattleService 内部状态的一部分，只能在该 Service 的 Handler 中调用。它使用 `ServiceContext.After` 创建 Core Timer，但不要求 `ServiceContext` 暴露 `Cancel`。

## 用途

Timeline 用于：

- 地鼠生成。
- 地鼠过期。
- 回合倒计时。
- 自动托管。
- 重连快照。

## TimelineRev

`TimelineRev` 用于重连和旧消息过滤。

客户端重连时，Battle Snapshot 应包含当前 `TimelineRev`。

## 规则

1. Timeline 只安排 Command。
2. Timeline 不直接修改 BattleState。
3. Timeline 必须支持取消。
4. Timeline 事件应进入 Battle 的 Mailbox。
5. Timeline 状态应可被 Snapshot 描述。
6. `Cancel` 只把对应 Timeline entry 标记为失效并递增 `TimelineRev`；已经创建的 Core Timer 可以迟到投递，但 Handler 必须按 `TimelineID + TimelineRev` 忽略失效 Command。
7. Service 停止时 Core 自动取消目标 Timer；Timeline 不持有 Timer 对象或回调。
8. Timeline 的状态修订只能在 BattleService Handler 中变化。
