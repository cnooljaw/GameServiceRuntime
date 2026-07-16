# Runtime Inspection

> 状态：已实现
>
> 规范：[RFC-0192](../../rfcs/RFC-0192-Core-Runtime-Inspection.md)

## 作用

`Runtime.Inspect()` 向 Monitor 等 Tooling 提供只读运行状态，同时隐藏 Registry、Mailbox、Task 和 Transport 等内部对象。

结果包含 Runtime 状态、Service 列表、Mailbox 深度、PendingCall、Timer、活动任务和 Metrics。

## 一致性

Inspection 是独立副本和最终一致视图，不是停机事务快照。每个子系统只在自己的锁内复制，避免观测引入 Runtime 全局锁。

Service 和 Task 按稳定键排序。调用方可以修改自己持有的切片，但不能通过它改变 Runtime 或后续结果。

## 生命周期

Running、Closing 和 Closed 状态都允许读取。关闭超时后仍未返回的 Runtime 任务继续保留，并显示 owner、类型、开始时间和 `TimedOut`；任务真实返回后才消失。

Inspection 不用于业务恢复。Player 或 Battle 状态恢复由 [RFC-0210](../../rfcs/RFC-0210-Tooling-Snapshot.md) 定义。
