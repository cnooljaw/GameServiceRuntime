# Control Plane：先看见，再决定，再执行

小林准备写一个 Controller：

```go
if node.CPU > 80 {
    runtime.Stop(service)
}
```

老周说：“你把观测、策略、权限和执行写在了同一个 if 里。”

## 三类角色

GSR 把控制面拆成：

```text
NodeAgent
  -> 采集本节点 observed state
ClusterObserverService
  -> 保存各节点最近事实
Coordinator / 外部 Controller
  -> 带身份与 RequestID 发起操作
```

NodeAgent 使用 `tooling/monitor` 生成报告，通过 Discovery lease 维持节点存在，并向 Observer 上报。

## NodeAgent

```go
agent, err := control.NewNodeAgentService(control.NodeAgentConfig{
    Reporter:          reporter,
    ObserverNode:      "node-a",
    Discovery:         discoveryRef,
    Address:           transport.Address(),
    HeartbeatInterval: 10 * time.Second,
    CallTimeout:       time.Second,
})
```

它是普通 Service：

- Timer 触发刷新 Command；
- Handler 调用本地 Reporter；
- 上报远端 Observer；
- 持有节点 lease 和最近结果。

它不直接执行任意管理脚本。

## Observer

```go
observer, err := control.NewClusterObserverService(
    control.ObserverConfig{},
)
```

Observer 保存：

- NodeID；
- address；
- lease generation；
- 最近 report；
- observed status；
- refreshed time。

它描述“系统现在被观察成什么样”，不是“系统应该变成什么样”。

## Observed 与 Desired

```text
Observed State：node-b 上报告有 12 个 Service
Desired State：player group 应该有 16 个副本
Action：在 node-c 创建 4 个替代实例
```

这三句话属于三个阶段。把它们混在 NodeAgent 中，Agent 就同时拥有事实、策略和执行权限。

当前 GSR 已实现 Observer、Drain Coordinator、NodeAgent receipt 和外部 Runner，但不提供通用自动 Reconcile 循环。

## 为什么操作需要 Principal

修改型请求带：

```go
type StartDrainRequest struct {
    RequestID RequestID
    Principal Principal
    // ...
}
```

Coordinator 校验 Gateway Source 和允许的 Principal，保存操作与审计事实。普通业务节点不能伪造管理操作。

## 为什么执行在 Runner

`Runtime.Stop` 和 `CreateService` 可能阻塞，也需要组合根持有完整 Runtime。Service Handler 内直接调用它们会：

- 占住 Mailbox 和 Scheduler；
- 在单 worker 下形成调度等待；
- 混淆控制状态 owner 与生命周期 owner。

所以：

```text
Coordinator Command
  -> NodeAgent receipt
  -> bounded Runner
  -> Runtime.Stop/CreateService
  -> result Command
  -> Coordinator 收敛
```

Runner 有固定 worker、Close、取消和真实返回等待。

## 可运行例子

```bash
go run ./examples/control-runtime
```

例子创建两个 Cluster Runtime，由 node-b 的 NodeAgent 把本地报告交给 node-a 的 Observer。

## 对照源码

- `tooling/control/service.go`
- `tooling/control/node_agent.go`
- `tooling/control/types.go`
- `examples/control-runtime/`

## 本章小结

控制面的关键不是多一个管理 API，而是让“谁看见、谁决定、谁执行、谁审计”都有独立 owner。
