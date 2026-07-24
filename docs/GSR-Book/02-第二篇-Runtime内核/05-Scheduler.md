# Scheduler：并行的是 Service，不是同一份状态

“我配置了 8 个 worker，为什么一个 Battle 还是只跑一个 Handler？”

“因为 8 个厨师可以同时做 8 桌菜，不能同时拿 8 把勺子搅同一口锅。”

## 调度模型

Runtime 创建固定数量的 worker：

```go
runtime := gsr.NewRuntime(gsr.Config{
    Workers:  8,
    MaxBatch: 32,
})
```

当 Service 的 Mailbox 从空变为可运行，Service 进入 Ready Queue。worker 取出 Service，获得它的执行许可，处理一批消息，再释放许可。

```text
Ready Queue
  ├── Battle A ── worker 1
  ├── Player 7 ── worker 2
  ├── Wallet  ── worker 3
  └── Battle B ── worker 4
```

同一 Service 不会被两个 worker 同时调度。

## 为什么按 Service 调度

如果 Ready Queue 存每一条 Envelope，两个 worker 可能同时取到同一目标的消息。Runtime 就必须给 Handler 再加互斥，或者把执行顺序交给运气。

按 Service 调度可以同时获得：

- 不同 owner 并行；
- 同一 owner 串行；
- 批量处理降低调度成本；
- Mailbox 仍是唯一顺序事实。

## MaxBatch

一次处理太少，调度开销高；一次处理无限多，热门 Service 饿死其他 Service。

`MaxBatch` 是折中：

```text
取 Battle A
处理最多 32 条
仍有消息 -> 重新排队
让其他 Service 获得 worker
```

调整它之前应观察 Mailbox depth、命令耗时和尾延迟。

## Service 内 Call 的许可

这是 Scheduler 最容易被忽略的细节。

假设只有一个 worker：

```text
A Handler -> Call B
```

如果 A 一直占着 worker 等 B，B 永远不能运行。GSR 在 Service 内 Call 前执行 `yield`，归还 A 的调度许可；Call 完成后再 `resume` A。

```text
A 开始
  -> yield A
  -> B 运行并 Reply
  -> resume A
  -> A 继续
```

Runtime 同时传播 CallPath，拒绝：

```text
A -> B -> A
```

这解决调度死锁，不会把跨 Service Call 变成事务。

## 一个性能误区

单 Battle 的轻量 Kick 大约是微秒级。把 worker 从 4 增加到 16，不会让同一个 Battle 的 Handler 并发。

想提高总吞吐，应让独立 owner 分片：

```text
10000 个 Battle
  -> 分布到 Ready Queue
  -> 多 worker 并行
```

如果一个 Battle 本身过热，应先：

1. 缩短 Handler；
2. 移出慢 I/O；
3. 减少复制和广播；
4. 最后才讨论拆分权威状态。

## 关闭

Runtime Close 会等待 Scheduler worker 真实返回。超时后 Runtime 可以报告仍活跃的任务，却不能强杀正在执行的 Go 函数。

这也是为什么业务 Service 不能随意创建 goroutine：Scheduler 无法追踪它们。

## 对照源码

- `runtime/scheduler.go`
- `runtime/ready_queue.go`
- `runtime/task.go`
- `runtime/scheduler_conformance_test.go`
- `runtime/call_conformance_test.go`

## 本章小结

Scheduler 的并发单位是 Service。配置更多 worker 是为了并行更多独立 owner，不是为了破坏同一 owner 的串行状态机。
