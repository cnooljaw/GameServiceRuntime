# Scheduler

> 状态：已实现
>
> 规范：[RFC-0160](../../rfcs/RFC-0160-Core-Scheduler.md)

## 调度模型

Service 不绑定 goroutine。Scheduler 使用共享 ReadyQueue 和固定数量执行许可：

```text
Mailbox 非空
    ↓
ReadyQueue（ServiceRef）
    ↓
固定执行许可池
    ↓
批量串行处理 Command
```

每个 Service 使用 `ready` 标记避免重复入队，使用执行许可保证同一实例只有一个调度任务。单次最多处理 `MaxBatch` 个队列项，之后重新排队，让其他 Service 获得执行机会。

## Call 期间的许可

Service Handler 同步 `Call` 其他 Service 时，Scheduler 暂时归还当前执行许可；Call 完成后重新获取。Service 在等待期间仍保持 busy，不处理自己的下一条 Command。

这种设计避免固定 Worker 数被同步 Call 全部占满，同时保留单 Service 串行语义。CallPath 会拒绝 self-call 和已检测到的跨 Service 调用环。

## 关闭

Scheduler 关闭 ReadyQueue，等待已启动的调度任务真实返回。期限耗尽时 Runtime 返回关闭错误，但任务仍保留在 `Runtime.Inspect` 中，不能假装 goroutine 已被强杀。
