# RFC-0274：人工恢复与补偿

> 状态：待实现
> 目标阶段：Phase 10C2B
> 范围：Runtime Tooling、Cluster Control Plane
> 依赖：[RFC-0250](RFC-0250-Tooling-Cluster-Control-Plane.md)、[RFC-0260](RFC-0260-Tooling-ServiceGroup-Routing.md)、[RFC-0271](RFC-0271-Tooling-Drain-Guard.md)、[RFC-0272](RFC-0272-Tooling-Controlled-Drain-Operation.md)、[RFC-0273](RFC-0273-Tooling-Node-Stop-Execution.md)
> 依据：Stop 后不得复活旧实例；替代实例、目录发布与审计必须能分别确认

## 目的

本文冻结已 Drain 且已尝试 Stop 的实例之后的**人工**恢复路径。授权 Gateway 代表允许的 Principal 创建一个可审计的 RecoveryOperation；组合根所持有的 RecoveryRunner 才能按显式 Blueprint 创建替代实例；Coordinator 只在替代实例已创建、目录仍处于预期版本且操作者显式确认时，才发布包含新 Ref 的更高 `ServiceSet`。

恢复不是 Guard 的反操作：任何已 Guard 的旧 Ref 都不得重新接流，任何已停止的 Ref 都不得重新发布。本文也不引入 Desired State、自动 Reconcile 或自动补偿决策。

## 目标

- 新增独立 `RecoveryOperation`、有界审计和与 Drain/Stop 相同的 Gateway + Principal + RequestID 入口。
- 由组合根注册稳定 `BlueprintID`，以固定上限的 `RecoveryRunner` 在 Service Handler 外创建新实例，并把结果投递回 NodeAgent。
- Coordinator 仅记录 Blueprint、旧/新 Ref、目录快照与结果；不持有 Runtime、Factory 或业务 Service 指针。
- 操作者必须在 Runner 返回新 Ref 后显式 `ConfirmRecovery`；确认阶段以 `AuthorityEpoch + Revision` CAS 发布一个包含新 Ref 的更高 `ServiceSet`。
- 所有未知结果均通过 `ResolveRecovery` 收敛，所有 slice、map、ServiceSet 和操作结果均返回副本。

## 非目标

- 不自动选择 Blueprint、节点、容量或补偿策略；不重试已失败的创建；不在后台轮询。
- 不恢复旧 Ref、Resume Drain Guard、回滚到含旧 Ref 的 ServiceSet，也不提供跨节点原子“创建 + 发布”事务。
- 不提供用户认证协议、外部审计 sink、进程热补丁、节点进程重启、HA 操作存储或通用控制器。

## 分层与所有权

```text
认证完成的 Gateway
  -> DrainClient.BeginRecovery / ConfirmRecovery / ResolveRecovery
  -> DrainCoordinatorService owns RecoveryOperation and audit
     -> NodeAgent BeginRecoveryCreate / GetRecoveryReceipt
     -> Directory Get / Publish CAS

NodeAgentService owns local RecoveryReceipt
  -> RecoveryRunner.Submit (bounded, non-blocking)

composition root owns RecoveryRunner + BlueprintRegistry
  -> Runtime.CreateService
  -> private NodeAgent result Command
```

`BlueprintRegistry` 是组合根的只读映射，`BlueprintID` 只能引用部署时注册的 factory；Coordinator、NodeAgent 和 Wire payload 都只看 ID，绝不传递 closure、Service 指针或业务配置。创建后的新实例首先处于未发布状态，只有 Confirm 成功才对组路由可见。若 Confirm 失败或操作者放弃，实例保持孤立，由部署方显式 Stop/清理；该清理也必须留下审计记录，不能暗中复用旧 Operation。

## 公开契约

包路径为 `tooling/control`。RFC-0272 的 `DrainClient` 新增：

```go
type BlueprintID string

type RecoveryTargetRequest struct {
    Removed   gsr.ServiceRef
    Agent     gsr.ServiceRef
    Blueprint BlueprintID
}

type RecoveryTargetState string
const (
    RecoveryTargetPending   RecoveryTargetState = "pending"
    RecoveryTargetCreating  RecoveryTargetState = "creating"
    RecoveryTargetCreated   RecoveryTargetState = "created"
    RecoveryTargetPublished RecoveryTargetState = "published"
    RecoveryTargetFailed    RecoveryTargetState = "failed"
    RecoveryTargetAbandoned RecoveryTargetState = "abandoned"
)

type RecoveryFailure string
const (
    RecoveryFailureNone                 RecoveryFailure = ""
    RecoveryFailureQueueFull            RecoveryFailure = "queue_full"
    RecoveryFailureRunnerClosed         RecoveryFailure = "runner_closed"
    RecoveryFailureBlueprintUnavailable RecoveryFailure = "blueprint_unavailable"
    RecoveryFailureCreate               RecoveryFailure = "create"
    RecoveryFailureDirectoryUnavailable RecoveryFailure = "directory_unavailable"
    RecoveryFailureDirectoryChanged     RecoveryFailure = "directory_changed"
    RecoveryFailurePublish              RecoveryFailure = "publish"
)

type RecoveryTarget struct {
    Removed  gsr.ServiceRef
    Agent    gsr.ServiceRef
    Blueprint BlueprintID
    Created  gsr.ServiceRef
    State    RecoveryTargetState
    Failure  RecoveryFailure
}

type RecoveryPhase string
const (
    RecoveryCreating  RecoveryPhase = "creating"
    RecoveryAwaitingConfirmation RecoveryPhase = "awaiting_confirmation"
    RecoveryPublishing RecoveryPhase = "publishing"
    RecoveryCompleted  RecoveryPhase = "completed"
    RecoveryFailed     RecoveryPhase = "failed"
    RecoveryAbandoned  RecoveryPhase = "abandoned"
)

type RecoveryOperation struct {
    RequestID RequestID
    Principal Principal
    Group     servicegroup.GroupName
    Expected  servicegroup.ServiceSet
    Published servicegroup.ServiceSet
    Targets   []RecoveryTarget
    Phase     RecoveryPhase
    CreatedAt time.Time
    UpdatedAt time.Time
}

type BeginRecoveryRequest struct {
    RequestID RequestID
    Principal Principal
    Group     servicegroup.GroupName
    Expected  servicegroup.ServiceSet
    Targets   []RecoveryTargetRequest
}

type RecoveryCreateTask struct {
    Agent     gsr.ServiceRef
    RequestID RequestID
    Removed   gsr.ServiceRef
    Blueprint BlueprintID
}

type RecoveryExecutor interface { Submit(RecoveryCreateTask) error }
type RecoveryFactory interface { CreateService(gsr.ServiceSpec) (gsr.ServiceRef, error) }
type BlueprintRegistry interface { Build(BlueprintID) (gsr.ServiceSpec, error) }
type RecoveryRuntime interface {
    CommandCaller
    Send(gsr.ServiceRef, gsr.CommandID, any) error
    CreateService(gsr.ServiceSpec) (gsr.ServiceRef, error)
}

type RecoveryRunnerConfig struct { Registry BlueprintRegistry; Workers, QueueSize int }
func NewRecoveryRunner(RecoveryRuntime, RecoveryRunnerConfig) (*RecoveryRunner, error)
func (*RecoveryRunner) Submit(RecoveryCreateTask) error
func (*RecoveryRunner) Close(context.Context) error

func (*DrainClient) BeginRecovery(context.Context, BeginRecoveryRequest) (RecoveryOperation, error)
func (*DrainClient) ConfirmRecovery(context.Context, RequestID, Principal) (RecoveryOperation, error)
func (*DrainClient) ResolveRecovery(context.Context, RequestID, Principal) (RecoveryOperation, error)
func (*DrainClient) GetRecovery(context.Context, RequestID, Principal) (RecoveryOperation, error)
func (*DrainClient) AbandonRecovery(context.Context, RequestID, Principal) (RecoveryOperation, error)
```

`NodeAgentConfig` 新增成对出现的 `RecoveryCoordinator gsr.ServiceRef` 与 `RecoveryExecutor RecoveryExecutor`。两者均为零值时保持已有行为；仅设置其一、Registry 为 nil、Workers/QueueSize 非正数、空 Blueprint、重复 Removed Ref 或重复 Agent/Removed 对均为配置/请求错误。

BeginRecovery 仅接受已由同 Principal 创建且 `StopCompleted`、`StopFailed` 或 `StopSuperseded` 的 StopOperation 的 Removed 集合；请求的 `Expected` 必须是当前 Directory 的完整快照，且不能含任何 Removed Ref。不同输入复用同一 RequestID 返回 `ErrRecoveryRequestConflict`；相同规范化输入返回已有 Operation 而不再次创建。Coordinator 在提交每个创建任务前读取 Directory 并要求仍等于 Expected。

Runner 为每个任务仅尝试一次 Blueprint.Build + Runtime.CreateService；成功后以私有 Command 报告 Created Ref，失败后报告稳定 Failure。它不得自行 Stop 已创建 Ref、不得发布 Directory、不得重试。Confirm 只在所有 Target 已 Created 时有效，并以 Expected.Version 的 CAS 发布把 Removed 替换为 Created 的 ServiceSet；成功后 `Published.Version > Expected.Version`。CAS 未知、目录变化或发布失败均不猜测结果：Operation 保持 Publishing，操作者必须 Resolve。Abandon 只允许尚未 Published 的 Operation；它记录放弃事实，不调用 Stop。

Command ID 固定为：

```text
0x02500104 BeginRecoveryCreate       NodeAgentService
0x02500105 GetRecoveryReceipt        NodeAgentService
0x025001fc RecordRecoveryCreate      NodeAgentService（私有，Runner）
0x02500308 BeginRecovery              DrainCoordinatorService
0x02500309 ConfirmRecovery            DrainCoordinatorService
0x0250030a ResolveRecovery            DrainCoordinatorService
0x0250030b GetRecovery                DrainCoordinatorService
0x0250030c AbandonRecovery             DrainCoordinatorService
```

## 状态与生命周期

RecoveryOperation 只由 Coordinator 的 Mailbox 创建和推进。Begin 后每个 Target 依次 `pending -> creating -> created|failed`；全部 Created 后 Operation 为 `awaiting_confirmation`，Confirm 后为 `publishing -> completed|failed`。Abandon 只能进入终态 `abandoned`。Completed、Failed 和 Abandoned 为终态，除 Resolve 的只读确认外不再改变。

NodeAgent receipt 以 `(CoordinatorRef, RequestID, Removed)` 为键，迟到、重复或来自其他 Coordinator 的结果不得覆盖当前 receipt。Coordinator 审计保留固定上限，超限按最旧终态记录淘汰；进行中的 Operation 不淘汰。

## 错误与失败语义

- Gateway/Principal/RequestID 授权失败沿用 RFC-0272 错误；无对应 StopOperation 为 `ErrStopOperationNotFound`。
- Blueprint 缺失、队列满、Runner 关闭和创建错误被记录为 Target Failure；它们不回滚已创建的其他 Target。
- Directory 不可读或与 Expected 不同，绝不创建或发布；Resolve 通过 Get 识别已成功的 Publish，不能依赖网络错误文本。
- Create 成功但回报丢失时，Runner 不应重新创建；人工通过 NodeAgent receipt 和 Operation audit 收敛。跨进程崩溃恢复不属于本期。

## 并发与所有权

Coordinator、NodeAgent、Room/Battle 等业务 Service 都不得创建 goroutine。RecoveryRunner 是唯一例外：由组合根拥有固定数量 worker，Close 关闭接收入口、取消尚未开始任务并等待已开始 `CreateService` 返回。Runner 不保存 ServiceContext，不修改业务状态，所有状态结果经私有 Command 回到 NodeAgent。

任何 Operation、ServiceSet、Target 列表和审计查询均返回深拷贝。BlueprintRegistry 的 Build 必须返回全新的 ServiceSpec；同一 Blueprint 不得共享可变 Service 实例。

## 可观测性

Coordinator 的 audit 记录 RequestID、Principal、Expected/Published version、Removed/Created Ref、BlueprintID、阶段、Failure 和时间；不得记录 Blueprint 的私有配置或密钥。NodeAgent report 记录本地 receipt 数与最近结果。Runner 自身只暴露队列深度、拒绝次数、在途数和 Close 等待时间；Core Metrics 仍仅经 `Runtime.Inspect().Metrics` 读取。

## 验收

- 相同 RequestID 不重复创建；冲突输入和非 Gateway source 被拒绝。
- 无法创建、队列满、Runner Close、NodeAgent 结果迟到及 Directory 不可用均留下可查询的失败事实。
- 创建的新 Ref 在 Confirm 前不在 Directory；Confirm 成功后只发布新 Ref，旧 Guard/Stopped Ref 从不重新出现。
- Expected CAS 冲突或网络未知结果必须 Resolve，不得乐观宣布成功。
- 两节点 TCP 场景覆盖在另一节点创建并在 Directory 发布；旧 Ref 仍拒绝外部 Command。
- Runner Close 等待 worker 返回，反复测试和 `go test -race ./...` 无泄漏或数据竞争。
