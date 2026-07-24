# Lifecycle：超时以后，函数可能还活着

“Stop 超时了，Runtime 把 goroutine 杀掉不就行了？”

“Go 没有这个按钮。”老周说。

## Service 状态

公开状态包括：

```text
Created
Starting
Running
Stopping
Closed
Failed
Restarting
```

普通成功路径：

```text
Created -> Starting -> Running -> Stopping -> Closed
```

Init、Handle、Stop 或 Close panic/失败可能进入 Failed。

## Stop 的接受边界

```go
err := runtime.Stop(ctx, ref)
```

Runtime 会：

1. 在 `acceptMu` 下把 Running 改为 Stopping；
2. 拒绝边界后的新 Send/After；
3. 按 Mailbox policy 处理已排队消息；
4. 通过 Scheduler 串行执行 Stop；
5. 调用 Close；
6. 清理 Mailbox、Timer、Pending Call、Registry 和 Name。

重复 Stop 会等待同一个实例结果，不会并发执行两次清理。

## Drain 与 Discard

```go
gsr.ServicePolicy{Mailbox: gsr.DrainMailbox}
```

处理 Stop 前已接受的消息。

```go
gsr.ServicePolicy{Mailbox: gsr.DiscardMailbox}
```

丢弃未开始处理的消息。正在执行的 Handler 仍会完成。

## 三种 timeout

```go
ServicePolicy{
    StopTimeout:      time.Second,
    CloseTimeout:     time.Second,
    LifecycleTimeout: 2500 * time.Millisecond,
}
```

- `StopTimeout`：等待 `Service.Stop`；
- `CloseTimeout`：等待 `Service.Close`；
- `LifecycleTimeout`：整个 Stop 请求的上限。

超时后 Runtime：

- 标记任务 TimedOut；
- 释放 Runtime 自有 Registry、Pending Call 等结构；
- 把实例记为 Failed；
- 保留任务句柄，继续观察真实返回。

它不能证明业务函数已经停止。

## 为什么 Runtime 还要追踪任务

```text
调用方看到 ErrStopTimeout
Service.Stop 仍在某个 goroutine 中运行
```

如果 Runtime 丢掉句柄，`Close` 返回后后台任务仍可能访问资源。`taskTracker` 记录：

- owner；
- kind：init/dispatch/stop/close；
- started time；
- timedOut；
- 完成句柄。

`Runtime.Inspect()` 可以看见这些任务。

## Runtime Close

```go
err := runtime.Close(ctx)
```

Close 会：

1. 停止接受 Create/Send/Call；
2. 失败所有 Pending Call；
3. 关闭 Cluster Transport；
4. 等待正在创建的 Service；
5. 并行请求各 Service Stop；
6. 取消 Timer；
7. 关闭 Scheduler；
8. 报告仍活跃任务；
9. 清理 Registry。

Runtime 内部可以并行发起多个 Service 的 Stop，因为这些实例拥有独立状态；每个实例内部仍保持串行。

## 业务终态不是 Stop

Battle 结束：

```text
BattleRunning -> BattleSettling -> BattleFinished
```

这是业务状态机，经 Command 完成。之后组合根才能 `Runtime.Stop(battleRef)`。

如果把结算塞进 `Stop`：

- 调用方拿不到稳定结果；
- 超时会留下未知资金状态；
- Runtime 生命周期被业务协议污染。

## panic

Handler panic 时，Runtime：

- 恢复 panic；
- 记录 `service_panics_total`；
- 对未 Reply 的 Call 返回 `ErrServiceFailed`；
- 关闭失败实例。

它不会原地重启。Supervisor 可以按策略创建新实例，但得到新 Ref。

## 对照源码

- `runtime/lifecycle.go`
- `runtime/task.go`
- `runtime/lifecycle_*_test.go`
- `runtime/resource_leak_internal_test.go`

## 本章小结

生命周期设计的核心不是“尽量 Close”，而是对每一个未知状态保持诚实：

```text
超时 = 没等到
取消 = 请求停止
返回 = 任务真的结束
```
