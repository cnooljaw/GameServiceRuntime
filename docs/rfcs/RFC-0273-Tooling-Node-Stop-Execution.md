# RFC-0273：受控 Node Stop 执行

> 状态：待实现
> 目标阶段：Phase 10C2A
> 范围：Runtime Tooling、Cluster Control Plane
> 依赖：[RFC-0130](RFC-0130-Core-Send-Call-Reply.md)、[RFC-0180](RFC-0180-Core-Lifecycle.md)、[RFC-0250](RFC-0250-Tooling-Cluster-Control-Plane.md)、[RFC-0260](RFC-0260-Tooling-ServiceGroup-Routing.md)、[RFC-0271](RFC-0271-Tooling-Drain-Guard.md)、[RFC-0272](RFC-0272-Tooling-Controlled-Drain-Operation.md)
> 依据：Runtime Stop 的真实返回追踪、Coordinator 的 ReadyToStop 事实与 NodeAgent 的节点生命周期 owner

## 目的

本文冻结 Phase 10C2 的第一个纵向切片：在已完成 `ReadyToStop` 的 Drain Operation 后，`DrainCoordinatorService` 以 Gateway+Principal 重新授权并确认 Directory，再请求目标节点的 `NodeAgentService` 提交本地 Stop。由组合根持有的有界 `NodeStopRunner` 在 Service Handler 外调用 `Runtime.Stop`，并把结果投递回 NodeAgent；Gateway 必须显式 `ResolveStop` 读取结果。

它把“下线旧实例”的高危 Runtime 生命周期能力留给节点本地 owner，不让 Coordinator、Gateway 或业务 Service 直接持有 Runtime。它不创建替代实例、不重新发布已经 Guard 的旧 Ref，也不自动补偿。

## 目标

Phase 10C2A 必须交付：

- `DrainCoordinatorService` 的 `BeginStop`、`ResolveStop`、`GetStop` 和独立、同 RequestID 的 `StopOperation`。
- 由精确 Coordinator ServiceRef 授权的 NodeAgent Stop Command，以及只对该 Coordinator 可见的 Node Stop receipt。
- 组合根持有的固定上限 `NodeStopRunner`：提交有界、关闭可取消且等待真实 `Runtime.Stop` 返回；它是唯一可调用本地 Runtime lifecycle 的执行 adapter。
- 每次实际 Stop 前由 Runner 使用 Directory `Get` 再确认 `StopOperation.Published`；不匹配或读取不确定时绝不调用 `Runtime.Stop`。
- 本地与双节点 TCP 验收、结果未知后的显式 Resolve、Runner 关闭和 goroutine 收敛测试。

## 非目标

Phase 10C2A 不实现：

- 节点进程退出、`Runtime.Close`、创建或替换实例、健康检查、扩缩容、放置、Desired State、Controller、后台 Reconcile 或定时轮询。
- 自动回滚、重新发布已经 Guard 的旧 Ref、反向 Resume Guard、自动恢复、补偿策略、持久化/HA 操作记录、外部审计 sink、HTTP/CLI/Web Console 或用户 token 校验。
- 跨节点原子“Directory 检查 + Runtime.Stop”事务。Runner 只在真正调用 Stop 前作一次强读取；之后并发发布的竞争必须由后续 Controller/人工恢复处理。

## 分层与所有权

```text
认证完成的 Gateway
  -> DrainClient.BeginStop / ResolveStop
  -> DrainCoordinatorService owns StopOperation
     -> Directory Get
     -> NodeAgent BeginNodeStop / GetNodeStopReceipt

NodeAgentService owns NodeStopReceipt
  -> NodeStopRunner.Submit (bounded, non-blocking)

组合根 owns NodeStopRunner
  -> Directory Get
  -> Runtime.Stop
  -> private NodeAgent result Command
```

`DrainCoordinatorService` 是 Stop Operation 的唯一 owner；NodeAgent 是本节点 receipt 的唯一 owner；Runner 只持有有界外部执行队列和 worker 生命周期；Runtime 仍是 Service 生命周期的唯一 owner。Coordinator、Gateway、业务 Service 和 Runner 均不持有目标 Service 指针。

部署顺序固定为：先创建 Coordinator，取得其精确 `ServiceRef`；再以该 Ref 配置启用 Stop 的 NodeAgent 和 Runner；最后由 Gateway 发起 Stop。Coordinator 不静态配置 Agent 列表：BeginStop 请求携带 Target/Agent 对，Coordinator 在自己的 Mailbox 冻结其副本，并只接受 Node 相同的 Agent。

## 公开契约

包路径为 `tooling/control`。RFC-0272 新增：

```go
type StopTargetRequest struct {
    Target gsr.ServiceRef
    Agent  gsr.ServiceRef
}

type StopTargetState string
const (
    StopTargetPending    StopTargetState = "pending"
    StopTargetQueued     StopTargetState = "queued"
    StopTargetStopped    StopTargetState = "stopped"
    StopTargetSuperseded StopTargetState = "superseded"
    StopTargetFailed     StopTargetState = "failed"
)

type StopFailure string
const (
    StopFailureNone                 StopFailure = ""
    StopFailureQueueFull            StopFailure = "queue_full"
    StopFailureRunnerClosed         StopFailure = "runner_closed"
    StopFailureDirectoryUnavailable StopFailure = "directory_unavailable"
    StopFailureRuntimeStop          StopFailure = "runtime_stop"
)

type StopTarget struct {
    Target  gsr.ServiceRef
    Agent   gsr.ServiceRef
    State   StopTargetState
    Failure StopFailure
}

type NodeStopReceipt struct {
    RequestID RequestID
    Target    gsr.ServiceRef
    State     StopTargetState
    Failure   StopFailure
    UpdatedAt time.Time
}

type StopPhase string
const (
    StopDispatching StopPhase = "dispatching"
    StopWaiting     StopPhase = "waiting"
    StopCompleted   StopPhase = "completed"
    StopFailed      StopPhase = "failed"
    StopSuperseded  StopPhase = "superseded"
)

type StopOperation struct {
    RequestID RequestID
    Principal Principal
    Group     servicegroup.GroupName
    Published servicegroup.ServiceSet
    Targets   []StopTarget
    Phase     StopPhase
    CreatedAt time.Time
    UpdatedAt time.Time
}

type BeginStopRequest struct {
    RequestID RequestID
    Principal Principal
    Targets   []StopTargetRequest
}

type NodeStopExecutor interface {
    Submit(NodeStopTask) error
}

type NodeStopTask struct {
    Agent     gsr.ServiceRef
    RequestID RequestID
    Target    gsr.ServiceRef
    Group     servicegroup.GroupName
    Published servicegroup.ServiceSet
}

type NodeStopRuntime interface {
    Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error)
    Send(gsr.ServiceRef, gsr.CommandID, any) error
    Stop(context.Context, gsr.ServiceRef) error
}

type NodeStopRunnerConfig struct {
    Directory   gsr.ServiceRef
    Workers     int
    QueueSize   int
    CallTimeout time.Duration
    StopTimeout time.Duration
}

func NewNodeStopRunner(NodeStopRuntime, NodeStopRunnerConfig) (*NodeStopRunner, error)
func (*NodeStopRunner) Submit(NodeStopTask) error
func (*NodeStopRunner) Close(context.Context) error

func (*DrainClient) BeginStop(context.Context, BeginStopRequest) (StopOperation, error)
func (*DrainClient) ResolveStop(context.Context, RequestID, Principal) (StopOperation, error)
func (*DrainClient) GetStop(context.Context, RequestID, Principal) (StopOperation, error)
```

`NodeStopRunnerConfig.Workers` 与 `QueueSize` 必须为正数；`Directory` 必须是具体 ServiceRef；负 timeout 无效，零值 `CallTimeout` 与 `StopTimeout` 均默认为三秒。

`NodeAgentConfig` 新增可选、成对出现的字段：

```go
StopCoordinator gsr.ServiceRef
StopExecutor    NodeStopExecutor
```

两者均为零值时保持 RFC-0250 的只读 NodeAgent 行为；仅设置其中之一、无效 Coordinator Ref 或 nil Executor 均为 `ErrInvalidConfig`。启用后，NodeAgent 只接受 `CommandContext.Source() == StopCoordinator` 的 Begin/receipt 查询；Target 的 Node 必须等于 Agent 自己的 Node，且 Target 不得等于 Agent 自己。

BeginStop 的 Principal 必须是 AllowedPrincipal，且只能由配置的 Gateway source 发起。它只接受已有的同 Principal、`DrainReadyToStop` Operation；Target/Agent 对必须与该 Operation 的全部 Target 一一对应、稳定排序、无重复，且 Agent.Node==Target.Node。Coordinator 在创建 StopOperation 前和每次向 Agent 提交前都读取 Directory，要求完整内容仍精确等于 DrainOperation.Published。

相同 RequestID 的 BeginStop 必须逐字段匹配 Principal 和规范化 Targets，匹配时返回已有独立 StopOperation，不重复提交；不同输入返回 `ErrStopRequestConflict`。同 Principal 可以 Get/Resolve；其他 AllowedPrincipal 得到 `ErrOperationOwnerMismatch`。所有 slice、map、ServiceSet、Operation 和 receipt 都返回独立副本。

CommandID 固定为：

```text
0x02500102 BeginNodeStop          NodeAgentService
0x02500103 GetNodeStopReceipt     NodeAgentService
0x025001fd RecordNodeStopResult   NodeAgentService（私有，本地 Runner）
0x02500305 BeginDrainStop         DrainCoordinatorService
0x02500306 ResolveDrainStop       DrainCoordinatorService
0x02500307 GetDrainStop           DrainCoordinatorService
```

Begin/receipt/Stop Operation Command 使用 `NewCodec`；私有 `RecordNodeStopResult` 绝不经 Cluster Codec 编解码。Gateway 只通过 `DrainClient` 调用 Coordinator；Coordinator 是唯一能 Call NodeAgent Stop Command 的远端 Service。

## 状态与失败语义

Coordinator BeginStop 先读取保存的 DrainOperation，要求其为 ReadyToStop，再强读取 Directory。验证通过后记录 StopOperation 为 Dispatching，逐个 Call Agent BeginNodeStop：

1. Agent 接受并由 Runner 排队，Target 为 Queued；Begin Reply 丢失、断线或无效响应保持 Pending，Resolve 可安全重试 Begin，因为 Agent 对 RequestID/Target 幂等。
2. Runner 在真正 `Runtime.Stop` 前强读取 Directory。读取失败将 receipt 留在 Pending 并附 `directory_unavailable`；内容不等将 receipt 设为 Superseded，绝不 Stop。
3. Runtime.Stop 成功，或返回 `ErrServiceClosed`/`ErrServiceNotFound` 时，receipt 为 Stopped。其它 Stop 返回为 Failed，附 `runtime_stop`；它是终态，不能自动重试或假装未发生。
4. Runner 满时 Agent 保持 Pending 并记录 `queue_full`；Runner 已关闭时 Agent 设为 Failed 并记录 `runner_closed`。Runner 关闭会取消尚未开始的工作并等待已开始 Runtime.Stop 的真实返回。

ResolveStop 是唯一推进路径：它先重新确认 Directory；不匹配时 StopOperation 进入 Superseded，不再提交 Pending Target。仍匹配时先 Get 每个 Agent receipt，再对 Pending Target BeginNodeStop。所有 Target Stopped 时为 Completed；全部 Target 终态且至少一个 Failed 时为 Failed；任何 Target Superseded 或 Directory 不匹配时为 Superseded。Ready/Failed/Superseded/Completed 均为终态。

Coordinator 和 NodeAgent 都不启动 Timer、goroutine 或后台重试。Runner 是固定上限外部 worker pool 的唯一例外：它由组合根构造并 Close；worker 不保存或使用 ServiceContext，不直接修改 Service 状态，结果只能通过私有 Command 回到 NodeAgent Mailbox。Runner 的 `context` 超时不能杀死 Service.Stop；Close 必须等待已开始 Stop 的真实返回或调用方 context 结束。

Stop 后的恢复是人工决策：Guard 不可 Resume，已停止 Ref 不可重新发布。StopOperation 为 Failed 或 Superseded 时，不允许自动补偿、重启或恢复；后续 Phase 10C2B 才能冻结创建新实例、发布更高 ServiceSet、人工确认和可审计恢复。

## 稳定错误、审计与指标

新增稳定错误：

```text
ErrInvalidStopRequest
ErrStopRequestConflict
ErrStopOperationNotFound
ErrStopNotReady
ErrStopTargetMismatch
ErrStopDisabled
ErrNodeStopQueueFull
ErrNodeStopRunnerClosed
```

来源、Principal、RequestID 或 Target/Agent 校验失败不得读取 Directory、Call Agent、提交 Runner 或创建 StopOperation。Coordinator 使用 RFC-0272 的有界审计记录 BeginStop、ResolveStop、重复、拒绝、Superseded、Completed 与 Failed；NodeAgent receipt 是节点执行事实，不是第二份全局审计。

Coordinator 记录：

```text
drain_stop_operations_started_total
drain_stop_operations_duplicate_total
drain_stop_operations_completed_total
drain_stop_operations_failed_total
drain_stop_operations_superseded_total
drain_stop_operations_denied_total
```

NodeAgent 记录 `node_stop_queued_total`、`node_stop_completed_total`、`node_stop_failed_total`、`node_stop_superseded_total`。队列满和 Directory 不可用分别通过 receipt 的 `queue_full` 与 `directory_unavailable` 归入 NodeAgent 的失败事实；Runner 是组合根持有的外置 adapter，不能绕过 `Runtime.Inspect().Metrics` 写入 Runtime 指标。

## 验收

必须覆盖：

1. StopCoordinator/StopExecutor 的配置成对校验；未启用 NodeAgent 不接受 Stop；Runner 拒绝无效配置、满队列和 Close 后 Submit，并在 Close 等待已启动 Stop 返回。
2. Gateway source、Principal、RequestID、Drain owner、ReadyToStop、Target/Agent 全量匹配和 Node 一致性验证；拒绝不触碰 Directory、Agent 或 Runner。
3. BeginStop 强确认 Published 后只提交一次；同 RequestID 重复不重复 Submit；不同 Targets 得到 ErrStopRequestConflict。
4. Begin Reply 丢失、Agent 暂不可达或 receipt Reply 丢失后，Resolve 以 Get/幂等 Begin 收敛；无 Timer、无 Service goroutine。
5. Runner 每次实际 Stop 前重读 Directory；读取失败不 Stop，内容改变为 Superseded，不 Stop；成功、已关闭、Stop timeout 和 Runner 关闭均有稳定 receipt/Operation 结果。
6. Directory 在部分 Stop 后改变时，Coordinator 不提交余下 Target，记录 Superseded，不自动补偿已停止 Target。
7. 本地和双节点 TCP 下 Gateway 能 Begin/Resolve，节点级 caller、其它 Service 和非 Coordinator NodeAgent caller 都被拒绝；私有结果 Command 不可远程 Codec。
8. Core、Directory、Drain Guard 和 Visitor Registry 不导入 control；`go test ./...`、`go vet ./...`、`go test -race ./...` 通过。
