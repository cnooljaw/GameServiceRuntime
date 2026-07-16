# RFC-0160：Scheduler 设计

> 状态：草案  
> 范围：Core Runtime  
> 依据：`docs/learn/007-Game-Service-Runtime详细设计与实现.md`

## 目的

本文定义 GSR 的调度模型。核心结论：Service 不等于 goroutine。

## 为什么需要 Scheduler

如果一个 Service 一个 goroutine：

```text
10000 BattleService -> 10000 goroutine
```

Go 可以承受大量 goroutine，但长期运行的游戏服务器更需要控制：

- 公平性。
- 执行耗时。
- Mailbox 堆积。
- 优先级。
- GC 压力。

## 推荐模型

```text
Mailbox receives Envelope
  ↓
ReadyQueue receives ServiceRef
  ↓
Dispatcher acquires execution permit
  ↓
Runtime-managed task processes mailbox batch
  ↓
Requeue if mailbox not empty
```

ReadyQueue 必须支持非阻塞 Push。Service handler 内的 Send 不能因为 ReadyQueue 满而占住所有执行许可。第一版使用互斥保护的可增长队列，每个 Service 的原子 ready 标记保证最多存在一个队列项。

## 调度流程

```go
for {
    ref := readyQueue.Pop()
    permit := executionPermits.Acquire()
    instance := registry.Get(ref)
    startTrackedTask(instance, func() {
        defer permit.Release()
        processBatch(instance, maxBatch)
        if instance.Mailbox.NotEmpty() {
            readyQueue.Push(ref)
        }
    })
}
```

## 批处理

每次处理最多 `maxBatch` 条消息。

目的：

1. 提高吞吐。
2. 防止单个 Service 长时间霸占执行许可。
3. 给其它 ready Service 执行机会。

## 慢 Command

Runtime 必须记录 Command 执行耗时。

超过阈值：

```text
log slow command
metrics slow_command_total++
```

## 禁止阻塞

Service handler 不应直接执行长时间阻塞操作。

例如 HTTP、DB、大文件 IO 应拆成专用 Service 或异步边界。

`ServiceContext.Call` 是受 Runtime 管理的例外：handler 等待 Reply 时让出有限执行许可，但保持该 Service busy；Reply 返回后重新获取许可并继续。这样不会把同步等待计入可运行 handler 的并发上限。

## Runtime Task 追踪

固定的是同时执行 Service 代码的许可数量，不是 Go goroutine 的绝对数量。Runtime 可以为 ready Service 创建执行任务，但每个任务必须登记：

- owner ServiceRef。
- task kind，例如 init、dispatch、stop、close。
- started time。
- cancel function，如果该任务可取消。
- done handle。

任务超时后，Runtime 可以释放 Mailbox、Registry、PendingCall 等自有结构，但任务记录必须保留到实际函数返回。Runtime 关闭时仍未返回的任务必须记录日志和 `runtime_tasks_abandoned_total`。

## 第一版范围

第一版实现：

- ReadyQueue。
- 固定执行许可池。
- maxBatch。
- 慢 Command 统计。
- 非阻塞 ReadyQueue 入队。
- Call 等待期间执行许可让出与恢复。

暂不实现：

- 多级优先级队列。
- Work stealing。
- 动态执行许可数量。

## 故障边界

Runtime 必须捕获 handler panic，记录 `service_panics_total` 并隔离失败 Service。Handler 返回 error 时记录 `handler_errors_total`；如果该 Command 来自 Call 且尚未 Reply，error 用原 Session 返回调用方。
