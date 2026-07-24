# Runtime Inspection：只读快照，不是控制台后门

小林想给 Monitor 增加：

```go
runtime.AllServices()
runtime.PendingCalls()
runtime.Tasks()
runtime.Metrics()
```

老周说：“每多一个 getter，就多一个 Tooling 可以绑住 Core 内部结构的机会。”

## 唯一入口

Core 只提供：

```go
inspection := runtime.Inspect()
```

返回：

```go
type RuntimeInspection struct {
    CapturedAt   time.Time
    Node         NodeID
    Status       RuntimeStatus
    Services     []ServiceInspection
    Tasks        []RuntimeTaskInspection
    PendingCalls int
    Timers       int
    Metrics      MetricsSnapshot
}
```

## 看一眼当前 Runtime

```go
snapshot := runtime.Inspect()

for _, service := range snapshot.Services {
    fmt.Printf(
        "%s/%d status=%v mailbox=%d\n",
        service.Ref.Node,
        service.Ref.ID,
        service.Status,
        service.MailboxDepth,
    )
}
```

还可以发现超时后仍未返回的任务：

```go
for _, task := range snapshot.Tasks {
    if task.TimedOut {
        logger.Warn("runtime task still active",
            "owner", task.Owner,
            "kind", task.Kind,
            "started", task.StartedAt,
        )
    }
}
```

## 副本与最终一致

Inspection 是独立副本，调用方不能通过它修改 Runtime。

各子系统分别复制，因此它不是一个原子事务。例如捕获 Services 后，一个 Timer 可能刚好触发；`CapturedAt` 只说明观测时刻，不表示所有字段来自同一锁。

它适合：

- 监控；
- 诊断；
- 测试资源是否收敛；
- 控制面读取 observed state。

它不适合：

- 作为业务事务判断；
- 修改 Registry；
- 直接 Stop Service；
- 获取 Service 对象。

## Monitor 如何使用

`tooling/monitor` 只依赖一个窄接口：

```go
type Inspector interface {
    Inspect() gsr.RuntimeInspection
}
```

Monitor 把 Core 快照转换为稳定 Report，再输出 JSON。它不创建 goroutine，不启动 HTTP，也不反向修改 Runtime。

HTTP、Prometheus 或管理平台属于 adapter。

## Metrics 也只从这里读取

Service 可以通过 `ServiceContext.Metrics()` 增加指标。读取快照只能：

```go
metrics := runtime.Inspect().Metrics
```

这样避免 Tooling 为自己增加新的 Core 观测 API。

## 性能解释

Inspection 会复制 Services、Tasks 和 Metrics。它不是每条 Command 的热路径 API，也不应每毫秒轮询。

通常：

- 本地 Monitor 低频采集；
- NodeAgent 周期性上报；
- 测试在关键边界检查。

## 对照源码

- `runtime/inspection.go`
- `runtime/metrics.go`
- `tooling/monitor/`
- `examples/monitor-runtime/`

运行：

```bash
go run ./examples/monitor-runtime
```

## 本章小结

Inspection 让 Core 可观察，但不泄漏可变内部结构。观察和控制分开，是后续 Control Plane 能保持清晰权限边界的基础。
