# Timer：未来仍然是一条 Command

小林写下：

```go
time.AfterFunc(time.Second, func() {
    battle.score++
})
```

老周把 `battle.score++` 圈了起来：“它从哪扇门进来的？”

## Timer 的唯一职责

GSR Timer 只负责未来投递 Command：

```go
timerID, err := serviceContext.After(
    time.Second,
    ExpireCommand,
    ExpireRequest{Target: 7},
)
```

时间到达后：

```text
Timer
  -> Runtime.Send
  -> Battle Mailbox
  -> Handle(ExpireCommand)
```

业务状态仍在 Handler 内修改。

## 为什么不执行回调

回调有三个问题：

- 容易直接捕获并修改 Service 状态；
- 运行时机与 Stop/Close 竞争；
- Record/Replay 难以表示。

Command 则天然具有目标、ID、payload 和可观测投递结果。

## Runtime API

外部调用方可以指定目标：

```go
id, err := runtime.After(target, delay, command, payload)
```

Service 内部默认投递给自己：

```go
id, err := serviceContext.After(delay, command, payload)
```

取消是幂等的：

```go
_ = runtime.Cancel(id)
```

取消只阻止尚未触发的 Timer。真实系统中仍应假设迟到、重复或旧代事件可能出现，并在业务层做 fencing。

## Battle Timeline 的 fencing

Timeline payload 带：

```text
BattleID
Epoch
TimelineID
Revision
Command
```

即使旧 Timer 到达，Battle 也会比较 Epoch 和 Revision：

```go
if fire.Epoch != battle.epoch {
    return nil
}
if fire.Revision != record.Revision {
    return nil
}
```

Timer 负责“到时候敲门”，Battle 决定“这次敲门是否仍有效”。

## 生命周期

Service Stop 时，绑定目标的 Timer 被取消。Runtime Close 会取消全部 Timer。

如果 Timer 已经触发，但投递时目标关闭或 Mailbox 满，Runtime 记录：

```text
timers_fired_total
timer_deliveries_total
timer_delivery_errors_total
timer_delivery_mailbox_full_total
timer_delivery_target_closed_total
```

Timer 失败不会偷偷执行业务补偿。是否重试属于业务 owner。

## 当前实现与演进

当前 `runtime/timer.go` 使用标准库 `time.Timer`，结构简单、语义清晰。

只有基准证明大量 Timer 成为热点时，才考虑 Timer Wheel。即使替换数据结构，公开契约仍然是“未来投递 Command”，不会变成回调 Service。

## 可测试时间

Runtime Config 允许注入 `Now`，业务 Timeline 也通过 Context 读取时间。测试应直接驱动 Command 或可控时钟，不用长时间 `time.Sleep` 猜测调度。

## 对照源码

- Core Timer：`runtime/timer.go`
- 投递：`runtime/runtime.go`
- Battle Timeline：`game/timeline.go`
- 投递测试：`runtime/timer_delivery_test.go`

## 本章小结

Timer 不拥有业务状态，也不执行规则。它只是把“未来”重新翻译成一条普通 Command。
