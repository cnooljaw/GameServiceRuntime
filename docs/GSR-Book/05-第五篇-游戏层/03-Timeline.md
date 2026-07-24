# Timeline：把游戏时间变成可 fencing 的意图

“三秒后让地鼠消失”，最直接的代码是回调。

但 Battle 可能已经结束，地鼠可能已经被击中，这条回调也可能属于旧 Epoch。

## Battle 内部能力

```go
timeline := ctx.Timeline()

id, err := timeline.After(
    3*time.Second,
    ExpireCommand,
    ExpireRequest{Shrew: 7},
)
```

Timeline 属于 Battle，不是独立 Service。

## API

```go
type Timeline interface {
    After(time.Duration, gsr.CommandID, any) (TimelineID, error)
    At(time.Time, gsr.CommandID, any) (TimelineID, error)
    Replace(TimelineID, time.Duration, gsr.CommandID, any) (TimelineRevision, error)
    Cancel(TimelineID) bool
    Snapshot() TimelineSnapshot
}
```

`After` 和 `At` 创建意图。`Replace` 增加 Revision。`Cancel` 先做逻辑取消，再尽力取消 Core Timer。

## TimelineFire

Core Timer 到期后向 Battle 投递私有 Command：

```text
TimelineFire{
  BattleID,
  Epoch,
  ID,
  Revision,
  Command,
}
```

Battle 逐项验证：

```text
BattleID 是否一致
Epoch 是否一致
Item 是否仍存在
State 是否 scheduled
Revision 是否一致
Command 是否一致
Battle 是否 running
```

全部通过才把原 payload 交给 Logic。

## Replace 为什么需要 Revision

```text
Revision 1：三秒后过期
Replace
Revision 2：五秒后过期
```

物理 Timer 1 可能已经触发或取消失败。它到达时 Revision 不匹配，只增加 ignored 指标，不改变状态。

## Epoch 为什么还需要

Revision 只在一个 Battle Timeline 内有效。逻辑替换或重建可能复用 TimelineID，因此还需要 BattleEpoch 隔离代际。

```text
old epoch 1 / timeline 7 / revision 2
new epoch 2 / timeline 7 / revision 1
```

旧事件不能进入新局。

## Context 有效期

Handler 外调用：

```go
_, err := savedTimeline.After(time.Second, command, payload)
// ErrContextExpired
```

`Cancel` 返回 false，`Snapshot` 返回零值。Timeline 不能成为绕过 Battle Mailbox 的长期句柄。

## Snapshot 与重连

TimelineSnapshot 包含排序后的 Item：

```go
type TimelineItem struct {
    ID       TimelineID
    Revision TimelineRevision
    DueAt    time.Time
    Command  gsr.CommandID
    State    TimelineState
}
```

它用于诊断和重连投影，不会在读取时重新创建 Timer。

## 测试方式

单元测试直接捕获 After payload，再把 `TimelineFireCommand` 投回 Battle，验证旧 Revision 被忽略。不要靠 `time.Sleep` 等真实墙钟。

对应测试：`game/battle_test.go` 中的 Timeline fencing 场景。

## 本章小结

Timer 解决“何时敲门”，Timeline 解决“这次敲门是否仍然属于当前游戏意图”。ID、Revision、Epoch 三层 fencing 缺一不可。
