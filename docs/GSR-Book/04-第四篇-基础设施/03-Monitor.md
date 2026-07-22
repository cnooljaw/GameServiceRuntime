# Monitor

> 状态：已实现本地第一版
>
> 依据：[RFC-0192](../../rfcs/RFC-0192-Core-Runtime-Inspection.md)、[RFC-0230](../../rfcs/RFC-0230-Tooling-Monitor.md)

## 本章目标

本章说明如何在不污染 Core Runtime 的前提下，把 `Runtime.Inspect()` 转换为可供日志、文件、CLI 或后续 HTTP adapter 使用的稳定报告。

第一版 Monitor 只处理本地即时快照。它不是 Service，不进入 Cluster Data Plane，也不负责远程管理。

## 为什么放在 Tooling

Core Runtime 只负责提供可独立修改的运行事实：Runtime 状态、Service、Mailbox、Task、PendingCall、Timer 和 Metrics。JSON 字段、状态字符串以及未来的 HTTP、CLI、Prometheus 格式都属于输出策略。

因此依赖方向固定为：

```text
Runtime.Inspect()
  -> RuntimeInspection
  -> tooling/monitor
  -> Report
  -> JSON / 后续 adapter
```

`runtime` 不依赖 `tooling/monitor`。Monitor 也不能读取私有 Registry、Mailbox 或 Metrics collector。

## API

```go
type Inspector interface {
    Inspect() gsr.RuntimeInspection
}

func New(Inspector) (*Monitor, error)
func (m *Monitor) Capture() Report
func (m *Monitor) WriteJSON(io.Writer) error
```

窄 `Inspector` 接口使 Monitor 可以消费真实 Runtime，也可以在测试或后续管理面 adapter 中消费等价的 Inspection 来源。

`Capture` 每次只调用一次 `Inspect`，并重新分配报告切片和指标 map。修改返回的 `Report` 不会改变 Runtime、Monitor 或下一次报告。

## 报告内容

`Report` 包含：

- `captured_at`、`node` 和 Runtime `status`。
- Service 数量、名字、`ServiceRef`、状态和 Mailbox 深度。
- Runtime Task 数量、owner、kind、开始时间和超时标记。
- PendingCall 和 Timer 数量。
- 全部 counter、gauge 和 duration；duration 统一输出为纳秒。

状态字符串是输出契约的一部分。Runtime 使用 `running`、`closing`、`closed`；Service 使用 `created`、`starting`、`running`、`stopping`、`closed`、`failed`、`restarting`；Task 使用 `init`、`dispatch`、`stop`、`close`。未知枚举统一输出 `unknown`。

## Metrics

Runtime 和业务 Service 仍通过 `Metrics` 写入指标。Monitor 只读取 `MetricsSnapshot` 的完整副本：

```go
counters := inspection.Metrics.Counters()
gauges := inspection.Metrics.Gauges()
durations := inspection.Metrics.Durations()
```

Core 额外固定记录远程 Call 结果：

```text
remote_calls_succeeded_total
remote_calls_failed_total
```

远程目标是 Node 非空且不同于本地 Node 的 `ServiceRef`。每个进入 Runtime 远程 Call 路径的请求只记录一个最终结果，本地 Call 不增加这两个指标。

## 使用示例

```go
localMonitor, err := monitor.New(runtime)
if err != nil {
    return err
}
if err := localMonitor.WriteJSON(os.Stdout); err != nil {
    return err
}
```

完整程序见 [`examples/monitor-runtime`](../../../examples/monitor-runtime/main.go)，运行：

```bash
go run ./examples/monitor-runtime
```

`WriteJSON` 写入一个 Report 和结尾换行，不关闭 writer。nil writer 返回 `ErrInvalidWriter`，编码和 writer 错误原样返回。空 Service、Task 和 Metrics 分别编码为 `[]`、`[]` 和 `{}`，不会出现 `null`。

## 一致性边界

报告继承 `RuntimeInspection` 的最终一致语义。Registry、Mailbox、Task、PendingCall、Timer 和 Metrics 分别复制，不承诺来自同一原子时刻。Monitor 不增加全局锁，也不会用自己的系统时间覆盖 Core 的 `CapturedAt`。

这与 Skynet 的取舍一致：Runtime 保持消息调度核心简洁，观测和管理通过外层能力组合。GSR 进一步用只读结构化快照隔离 Core 和输出格式，而不是让监控代码直接访问 Runtime 私有状态。

## 当前不做

- HTTP server、CLI 和 Web Console。
- Prometheus/OpenMetrics exporter。
- `MonitorService`、`NodeAgentService` 和远程观测 Command。
- 告警、历史时序、聚合和持久化。
- Battle、Player 等业务专用指标规范。

Phase 8 可以由 `NodeAgentService` 在本节点调用 Monitor，再通过白名单 Command 返回报告。远程管理仍不能直接读取 Runtime 私有结构。

## 验证重点

- 状态字符串与 JSON 字段稳定。
- Report、切片和 Metrics map 保持独立副本。
- Running、Closing、Closed 和未知状态均可报告。
- writer 错误透传且不会被关闭。
- Monitor 不创建 goroutine、不启动网络监听、不依赖业务类型。
