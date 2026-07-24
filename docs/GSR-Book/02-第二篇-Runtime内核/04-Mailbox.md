# Mailbox：一扇门比十把锁更容易证明

小林给 Player 加了五把 mutex，分别保护装备、任务、好友、房间和金币。

“它们永远不会一起变化吗？”老周问。

小林沉默了。锁保护字段，业务不变量却常常跨字段。

## Mailbox 的职责

每个 Service 实例有一个私有 Mailbox。所有业务 Command 都必须先进入这扇门：

```text
Send / Call / Timer / Remote Envelope
                 ↓
              Mailbox
                 ↓
              Handler
```

同一 Service 一次只执行一个 Mailbox 批次，因此 Handler 串行。

## 有界队列

Runtime 默认 Mailbox 大小为 64，可通过 Config 调整：

```go
runtime := gsr.NewRuntime(gsr.Config{
    MailboxSize: 256,
})
```

队列满时返回 `ErrMailboxFull`。GSR 不提供无限队列，因为无限队列只是把过载变成延迟和内存事故。

调用方要按业务语义处理：

- 可丢通知：记录指标后丢弃；
- 关键写入：返回上游或进入有界重试；
- 状态查询：让 Call 失败；
- Timer：Runtime 记录投递失败指标。

## 接受边界

Send 和 Stop 共享 `acceptMu`。这保证一个清晰裁决：

```text
Stop 获得边界前已接受的消息 -> 按 MailboxPolicy 处理
Stop 获得边界后到达的消息 -> ErrServiceClosed
```

`DrainMailbox` 会处理已排队 Command，再执行 Stop。

`DiscardMailbox` 会丢弃未处理 Command，再执行 Stop。

正在执行的 Handler 不会被强行中断。

## 为什么状态不再需要业务锁

```go
type playerService struct {
    online bool
    room   RoomID
    battle BattleID
}
```

如果这些字段只在 Handler 内变化，就可以在一条 Command 中维护跨字段不变量：

```go
func (s *playerService) bindBattle(binding PlayerBinding) error {
    if !s.online || s.room == "" {
        return ErrStateConflict
    }
    s.battle = binding.Battle
    return nil
}
```

这不代表 Runtime 没有锁。Mailbox、Registry、Pending Call 内部当然需要同步。区别是业务作者不必把并发控制散落在领域代码中。

## Mailbox 不解决慢 Handler

如果 Handler 做了 300ms 数据库调用：

```text
Command 1: DB 300ms
Command 2: 等待
Command 3: 等待
...
```

Mailbox 会忠实地把问题变成排队。正确做法是把外部 I/O 交给有界 runner，再以结果 Command 回到 owner。

## 批处理与公平性

Scheduler 每次最多处理 `MaxBatch` 条消息，默认 32。处理完一批后，如果 Mailbox 仍有内容，会重新入 Ready Queue。

这样避免一个热门 Service 永久占住 worker，同时减少每条消息都重新调度的开销。

## 测试关注什么

Mailbox 测试不只看 FIFO，还应覆盖：

- 满队列；
- Stop 与 Send 竞争；
- Drain 与 Discard；
- panic 后剩余消息；
- 目标关闭后的拒绝；
- Mailbox depth 指标。

相关源码：

- `runtime/mailbox.go`
- `runtime/ready_queue.go`
- `runtime/lifecycle_conformance_test.go`
- `runtime/scheduler_conformance_test.go`

## 本章小结

Mailbox 不是为了消灭所有锁，而是为了把业务写入集中到一个可观察、可拒绝、可关闭的顺序边界。它让“谁能改状态”从团队约定变成 Runtime 契约。
