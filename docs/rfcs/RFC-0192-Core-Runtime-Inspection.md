# RFC-0192：Runtime Inspection

> 状态：已接受
> 范围：Core Runtime
> 依据：`RFC-0180` 的任务追踪与 `RFC-0230` 的只读观测需求

## 目的

本文定义 Core Runtime 提供给 Tooling 的唯一只读观测边界。

Inspection 用于诊断当前运行状态，不用于恢复业务状态，也不提供修改 Runtime 的反向通道。

## API

```go
func (r *Runtime) Inspect() RuntimeInspection
```

```go
type RuntimeStatus int

const (
    RuntimeRunning RuntimeStatus = iota
    RuntimeClosing
    RuntimeClosed
)

type RuntimeTaskKind string

const (
    RuntimeTaskInit     RuntimeTaskKind = "init"
    RuntimeTaskDispatch RuntimeTaskKind = "dispatch"
    RuntimeTaskStop     RuntimeTaskKind = "stop"
    RuntimeTaskClose    RuntimeTaskKind = "close"
)

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

type ServiceInspection struct {
    Ref          ServiceRef
    Name         ServiceName
    Status       ServiceStatus
    MailboxDepth int
}

type RuntimeTaskInspection struct {
    ID        uint64
    Owner     ServiceRef
    Kind      RuntimeTaskKind
    StartedAt time.Time
    TimedOut  bool
}
```

`MetricsSnapshot` 提供单项读取和全量枚举：

```go
func (s MetricsSnapshot) Counter(string) uint64
func (s MetricsSnapshot) Counters() map[string]uint64
func (s MetricsSnapshot) Gauge(string) int64
func (s MetricsSnapshot) Gauges() map[string]int64
func (s MetricsSnapshot) Duration(string) time.Duration
func (s MetricsSnapshot) Durations() map[string]time.Duration
```

三个枚举方法每次返回新 map；修改返回值不能影响原快照、Runtime 或后续 Inspection。零值快照返回非 nil 空 map。

## 生命周期

`Inspect` 可以在 Running、Closing 和 Closed 状态调用。它不返回 error，不启动后台任务，不阻止关闭，也不延长 Runtime 生命周期。

Runtime 关闭超时后，尚未真实返回的 Init、Dispatch、Stop 或 Close 任务仍出现在 `Tasks` 中，并带有 `TimedOut=true`。任务真实返回后才从 Inspection 中消失。

## 副本与顺序

返回结果必须满足：

- 不包含 Service、Mailbox、PendingCall、Timer、Task、Registry、channel、取消函数或 Transport 指针。
- `Services`、`Tasks` 和 `Metrics` 是独立副本。调用方修改自己的结果不能影响 Runtime 或后续 Inspection。
- Metrics 枚举方法返回的 map 也是独立副本，不暴露 collector 或写接口。
- `Services` 按 `ServiceRef.Node`、`ServiceRef.ID` 排序。
- `Tasks` 按任务 ID 排序。

稳定顺序属于公开行为，不能依赖 Go map 遍历顺序。

## 一致性

Inspection 是最终一致视图，不是跨子系统的停机事务快照。

Registry、Mailbox、PendingCall、Timer、Task 和 Metrics 分别在自己的锁内复制。实现不能为了观测同时持有多个子系统锁，也不能在 Send、Call、Timer 或 Scheduler 热路径增加 Inspection 专用写入。

`CapturedAt` 表示本次采集生成结果时使用的 Runtime 时间源，不代表所有字段在该时刻原子成立。

Task ID 只在当前 Runtime 内用于诊断，是不透明标识，不是取消、等待或远程控制句柄。

## 边界

Core Inspection 不提供：

- HTTP、CLI、Web Console 或 Prometheus exporter。
- MonitorService、NodeAgentService 或远程管理 Command。
- Cluster 连接对象、Transport 内部状态或节点管理状态。
- Last Command 和每 Service Slow Command 明细。
- 业务状态 Snapshot、Restore、Record 或 Replay。

这些能力由 Runtime Tooling RFC 定义，并通过本接口或独立适配器组合，不能反向污染 Core。

## 规则

1. `Runtime.Inspect` 是 Core 唯一运行状态观测入口。
2. Tooling 不得直接读取 Runtime 私有 Registry。
3. Inspection 不得暴露可变内部对象。
4. Inspection 不承诺跨子系统原子一致性。
5. Inspection 必须能观察关闭超时后仍未返回的 Runtime 任务。
6. 新增观测字段不能改变消息投递和生命周期热路径。
7. Runtime Task Kind 新增稳定值时必须先更新本 RFC 和公开测试。

## 验收

必须覆盖：

- Running、Closing 和 Closed 状态。
- Service 稳定排序、名称、状态和 Mailbox 深度。
- PendingCall 与 Timer 计数。
- Init、Dispatch、Stop、Close 任务类型。
- 关闭超时任务的 TimedOut 标记和真实返回后的回收。
- 返回切片与 Metrics 的副本语义。
- 并发创建、投递、计时、停止、关闭和 Inspection 的 Race 检查。
