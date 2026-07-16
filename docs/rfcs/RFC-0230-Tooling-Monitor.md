# RFC-0230：Monitor 与可观测性

> 状态：草案  
> 范围：Runtime Tooling  
> 依据：`docs/learn/007-Game-Service-Runtime详细设计与实现.md`

## 目的

本文定义 Runtime 需要暴露的观测信息。

## 第一版范围

Phase 7A 先提供日志、内存指标和只读 `RuntimeInspection`。

后续再提供 HTTP/CLI 工具。HTTP/CLI 只是适配层，不能直接读取 Runtime 内部结构。

## Runtime Inspection

Core 只提供一个观测入口：

```go
func (r *Runtime) Inspect() RuntimeInspection
```

数据模型：

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

`Inspect` 可以在 Runtime 处于 Running、Closing 或 Closed 时调用。它不返回 error，不阻止关闭，也不延长 Runtime 生命周期。

返回数据必须满足：

- 不包含 Service、Mailbox、PendingCall、Timer、Task、Registry、channel、取消函数或 Transport 指针。
- 所有切片和 Metrics 都是独立副本；调用方修改自己的结果不能影响 Runtime 或后续 Inspection。
- `Services` 按 `ServiceRef` 排序，`Tasks` 按任务 ID 排序。
- 各子系统只在自己的锁内复制数据，不能为了观测增加 Runtime 全局锁。

Inspection 不是跨 Registry、PendingCall、Timer、Task 和 Metrics 的停机事务快照。调用方必须把它理解为同一采集过程中的最终一致视图。

`RuntimeInspection` 只用于诊断，不能用于 `RFC-0210` 定义的业务状态恢复。

## 指标

必须记录：

- Service 数量。
- Service 状态。
- Mailbox 长度。
- Pending Call 数量。
- Timer 数量。
- Command 执行耗时。
- Slow Command 次数。
- Remote Call 成功/失败数。

Cluster 连接状态、节点心跳和管理命令指标由 Phase 8 的 Transport 观测适配器与 Control Plane 提供，不进入 Phase 7A 的 Core Inspection。

## Debug 信息

应支持 dump：

```text
ServiceRef
ServiceStatus
Mailbox length
Active task
Pending Call count
Timer count
```

Last Command 和每个 Service 的 Slow Command 明细需要在热路径增加额外记录，Phase 7A 不实现；全局慢 Command 指标继续保留。

## Phase 8：NodeAgentService

每个节点可以启动一个系统级 `NodeAgentService`，用于响应管理面查询。

建议 Command：

```text
CmdPingNode
CmdGetNodeStats
CmdListServices
CmdGetServiceStats
CmdGetMailboxStats
CmdGetSlowCommands
CmdGetPendingCalls
```

这些命令只能读取 Runtime 状态，默认不能修改业务状态。

## Admin API

HTTP、CLI 或 Web Console 只允许调用管理面 Service：

```text
Admin API / CLI
  ↓
ClusterControlService
  ↓
MonitorService / NodeAgentService
```

禁止 Web Handler 直接访问 `ClusterTransport`、`Scheduler`、`Mailbox` 或 Service 指针。

## Business Layer 指标

Battle 层建议记录：

- Battle 数量。
- Battle 时长。
- 玩家数量。
- Broadcast 次数。
- Timeline 事件数量。
- 重连次数。

这些属于 Business Layer 指标，不进入 Core Runtime 内核。

## 规则

1. Monitor 不应修改业务状态。
2. Monitor 不应暴露 Service 指针。
3. 慢 Command 必须带 ServiceRef 和 CommandID。
4. Cluster 底层错误要转换成 Runtime 错误后再上报。
5. 远程观测必须走白名单 Command。
6. 生产环境禁止任意代码注入式调试接口。
7. Inspection 只能返回独立副本，不能提供修改 Runtime 的反向通道。
8. Inspection 不承诺跨子系统的原子一致性。
