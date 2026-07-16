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
WorkerPool takes ServiceRef
  ↓
Process mailbox batch
  ↓
Requeue if mailbox not empty
```

## Worker 流程

```go
for {
    ref := readyQueue.Pop()
    instance := registry.Get(ref)
    n := processBatch(instance, maxBatch)
    if instance.Mailbox.NotEmpty() {
        readyQueue.Push(ref)
    }
}
```

## 批处理

每次处理最多 `maxBatch` 条消息。

目的：

1. 提高吞吐。
2. 防止单个 Service 长时间霸占 Worker。
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

## 第一版范围

第一版实现：

- ReadyQueue。
- 固定 WorkerPool。
- maxBatch。
- 慢 Command 统计。

暂不实现：

- 多级优先级队列。
- Work stealing。
- 动态 Worker 数量。

