# RFC-0230：Monitor 与可观测性

> 状态：已接受（2026-07-22）
>
> 范围：Runtime Tooling
> 依赖：[RFC-0192](RFC-0192-Core-Runtime-Inspection.md)

## 目的

本文定义 Phase 7C 的本地 Monitor 适配器和稳定 JSON 输出。

Monitor 消费 Core `Runtime.Inspect()` 的只读副本，把内部观测模型转换成适合日志、文件、CLI 和后续 HTTP adapter 使用的报告。Monitor 不修改 Runtime，不进入 Cluster Data Plane，也不承担远程管理职责。

## 第一版范围

Phase 7C 实现：

- `tooling/monitor` 本地适配器。
- Runtime、Service、Mailbox、Task、PendingCall 和 Timer 报告。
- Runtime 与业务通过 `Metrics` 写入的 counter、gauge 和 duration 输出。
- 标准库 `encoding/json` 输出到 `io.Writer`。
- 远程 Call 成功和失败计数。

Phase 7C 不实现：

- HTTP server、CLI 命令或 Web Console。
- Prometheus/OpenMetrics exporter。
- `MonitorService`、`NodeAgentService` 或远程观测 Command。
- Cluster 连接列表、节点心跳或管理命令指标。
- 告警、指标持久化、聚合、采样或历史时序存储。
- Last Command 和每个 Service 的 Slow Command 明细。
- Battle 等业务领域专用指标定义。

## 分层

```text
Runtime.Inspect()
  -> RuntimeInspection 独立副本
  -> tooling/monitor
  -> Report
  -> JSON / 后续 adapter
```

依赖方向：

```text
tooling/monitor -> runtime
runtime -X-> tooling/monitor
```

Monitor 不能读取 Runtime 私有 Registry、Mailbox、Task 或 Metrics collector，也不能持有 Service 指针。

## Core 观测补充

`MetricsSnapshot` 当前只支持按名称读取，Tooling 无法输出 Runtime 和业务注册的全部指标。Core 增加只读枚举副本：

```go
func (s MetricsSnapshot) Counters() map[string]uint64
func (s MetricsSnapshot) Gauges() map[string]int64
func (s MetricsSnapshot) Durations() map[string]time.Duration
```

每次调用都返回新 map。调用方修改结果不能改变 `MetricsSnapshot`、Runtime 或后续 Inspection。空快照返回非 nil 空 map。

这三个方法只暴露指标事实，不暴露 collector、锁或写入接口。

快照获取边界以 [RFC-0192](RFC-0192-Core-Runtime-Inspection.md) 为准：Monitor 必须先调用 `Inspect()`，再读取 `inspection.Metrics`，不得要求 Core 增加独立 Metrics getter。

## 远程 Call 指标

Core 固定记录：

```text
remote_calls_succeeded_total
remote_calls_failed_total
```

规则：

1. 目标 Node 非空且不同于本地 Node 时，记为远程 Call。
2. 每个进入 Runtime 远程 Call 路径的请求只记录一个最终结果。
3. Reply 成功返回时增加 `remote_calls_succeeded_total`。
4. 编码、发送、远端 Handler、超时、取消、断线或 Reply 解码错误增加 `remote_calls_failed_total`。
5. 本地 Call 不增加这两个指标。
6. 在进入 Runtime Call 路径前被 Service 生命周期规则拒绝的请求不计数。

## Monitor API

```go
type Inspector interface {
    Inspect() gsr.RuntimeInspection
}

type Monitor struct { /* private */ }

func New(Inspector) (*Monitor, error)
func (m *Monitor) Capture() Report
func (m *Monitor) WriteJSON(io.Writer) error
```

稳定错误：

```text
ErrInvalidInspector
ErrInvalidWriter
```

nil 参数包括 nil interface 和动态类型为 nil 的 interface。`New` 对两者都返回 `ErrInvalidInspector`；`WriteJSON` 对两者都返回 `ErrInvalidWriter`，不得延迟到 `Capture` 或 writer 调用时 panic。

`Monitor` 不创建 goroutine，不安排 Timer，不缓存 Runtime 指针之外的可变状态。`Capture` 每次调用一次 `Inspector.Inspect()` 并生成独立报告。`WriteJSON` 只编码一次新的报告，不启动网络监听。

## 报告模型

```go
type Ref struct {
    Node gsr.NodeID
    ID   gsr.ServiceID
}

type Report struct {
    CapturedAt   time.Time
    Node         gsr.NodeID
    Status       string
    ServiceCount int
    Services     []ServiceReport
    TaskCount    int
    Tasks        []TaskReport
    PendingCalls int
    Timers       int
    Metrics      MetricsReport
}

type ServiceReport struct {
    Ref          Ref
    Name         gsr.ServiceName
    Status       string
    MailboxDepth int
}

type TaskReport struct {
    ID        uint64
    Owner     Ref
    Kind      string
    StartedAt time.Time
    TimedOut  bool
}

type MetricsReport struct {
    Counters      map[string]uint64
    Gauges        map[string]int64
    DurationsNanos map[string]int64
}
```

公开 Go 结构使用稳定 `snake_case` JSON 字段。空 Services、Tasks 和 Metrics map 编码为 `[]`、`[]` 和 `{}`，不能编码为 `null`。

Runtime 状态固定为：

```text
running
closing
closed
unknown
```

Service 状态固定为：

```text
created
starting
running
stopping
closed
failed
restarting
unknown
```

Task Kind 使用 Core 的 `init`、`dispatch`、`stop`、`close`；未知值输出 `unknown`。未知枚举不得导致 Capture 或 JSON 输出失败。

## 副本与一致性

`Report` 是独立副本。调用方可以修改切片、元素和 Metrics map，但不能影响 Monitor、Runtime 或后续 Report。

Monitor 不增加新的全局锁。报告继承 `RuntimeInspection` 的最终一致语义，不承诺 Registry、Mailbox、PendingCall、Timer、Task 和 Metrics 来自同一原子时刻。

`CapturedAt` 沿用 Core Inspection 的采集时间，Monitor 不用系统时间覆盖它。

## JSON 输出

`WriteJSON` 使用标准库 `encoding/json`，写入一个 Report 和结尾换行。它不关闭 writer。

序列化错误和 writer 错误原样返回；Monitor 不把错误转换成 Runtime 或业务错误。JSON adapter 不接受 nil writer，返回 `ErrInvalidWriter`。

## 后续阶段

Phase 8 的 `NodeAgentService` 可以在本节点调用 Monitor，再通过白名单 Command 返回报告。HTTP、CLI 和 Web Console 只能消费 Monitor 或管理面 Service，不能直接读取 Runtime 内部结构。

Prometheus/OpenMetrics exporter 作为独立 adapter 后续实现。它可以消费 `Report.Metrics`，但不得把 Prometheus 类型或标签规则压入 Core Runtime。

## 规则

1. Monitor 只读取 `Runtime.Inspect()`。
2. Monitor 不修改业务状态，不暴露 Service 指针。
3. Monitor 不创建 goroutine，不启动网络监听。
4. Report 和 Metrics map 必须是独立副本。
5. JSON 字段和状态字符串必须稳定。
6. Inspection 的最终一致语义必须透传，不伪装成原子快照。
7. 远程观测和管理命令留到 Phase 8。
8. Business Layer 指标可以通过 `Metrics` 写入，但指标命名由业务层负责。
9. Cluster 连接状态由 Transport 观测 adapter 提供，不反向加入 Runtime Inspection。

## 验收

必须覆盖：

- MetricsSnapshot 三类 map 的枚举与独立副本。
- 远程 Call 成功、失败和本地 Call 不计数。
- Runtime、Service 和 Task 状态字符串转换。
- Service、Mailbox、PendingCall、Timer 和 Task 报告。
- Report 切片和 Metrics map 的独立副本。
- 空集合 JSON 不为 null，字段使用稳定 `snake_case`。
- writer 错误原样返回，nil writer 返回稳定错误。
- Running、Closing 和 Closed 状态均可 Capture。
- Monitor 无 goroutine、无 HTTP、无远程 Command。
- 全量测试、`go vet` 和 Race Detector。

## 实现状态

Phase 7C 已完成：

- `MetricsSnapshot` 已提供三类指标的独立枚举副本。
- Runtime 已在远程 Call 统一返回路径记录成功或失败结果。
- `tooling/monitor` 已实现本地 Report、稳定状态字符串和 JSON writer。
- `examples/monitor-runtime` 已提供可执行示例。

远程 NodeAgent、HTTP/CLI、Prometheus exporter 和管理命令仍按本文边界留到后续阶段。
