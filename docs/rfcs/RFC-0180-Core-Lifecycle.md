# RFC-0180：Service 生命周期

> 状态：草案  
> 范围：Core Runtime  
> 依据：`docs/learn/007-Game-Service-Runtime详细设计与实现.md`、`hanxi/skynet-demo` 的服务退出管理

## 目的

本文定义 Service 创建、运行、停止和关闭流程。

Service 退出必须有明确阶段、超时和观测事件。GSR 学习 Skynet 项目中服务退出管理的思想，但不引入全局 `atexit` API。

## 状态

```go
type ServiceStatus int

const (
    ServiceCreated ServiceStatus = iota
    ServiceStarting
    ServiceRunning
    ServiceStopping
    ServiceClosed
    ServiceFailed
    ServiceRestarting
)
```

## CreateService 流程

```text
Validate ServiceSpec
  ↓
Allocate ServiceID
  ↓
Create Service
  ↓
Create Mailbox
  ↓
Register LocalRegistry
  ↓
Init(ServiceContext)
  ↓
Status = Running
  ↓
Return ServiceRef
```

## Stop 流程

```text
Atomically mark Stopping with message acceptance
  ↓
Reject new Send、Call and After to this Service
  ↓
Drain or discard mailbox by policy
  ↓
Create stop context with timeout
  ↓
Call Service.Stop(ctx)
  ↓
Call Service.Close
  ↓
Force close if timeout
  ↓
Remove registry
  ↓
Wake pending sessions
  ↓
Status = Closed or Failed
```

Stop 是 Mailbox 中的 Runtime 控制消息。Runtime 在同一个消息接受临界区内把状态改为 `Stopping`，并把 Stop 控制消息排在此前已接受的 Command 之后。这样既不会出现“Send 返回成功但消息排在 Stop 后面”，也保证 `Service.Stop`、`Service.Close` 不与 `Handle` 并发。

同一实例只执行一次 Stop 流程。实例仍处于 `Stopping` 或失败清理中时，后续 `Runtime.Stop` 使用调用方 context 等待已有结果，不创建第二个 Stop 控制消息。实例已经移出 Registry 后，后续调用稳定返回 `ErrServiceClosed`。

## 退出阶段

退出分两阶段：

```text
Stop(ctx): 业务清理阶段
Close():  资源释放阶段
```

`Stop(ctx)` 用于保存状态、取消订阅和停止接收外部流量等可失败操作。单独调用 `Runtime.Stop` 且 Runtime 仍为 Running 时，`Stop(ctx)` 可以通过 Send 或 Call 通知仍在运行的依赖方。

`Runtime.Close` 是终止阶段：Runtime 先进入 Closing，再调用各 Service 的 Stop，因此新的 CreateService、Send、Call 和 After 都被拒绝。需要跨 Service 协作的平滑下线必须先由 Runtime Tooling 的 Drain 完成，再进入 `Runtime.Close`。

同一个 Runtime 只执行一次关闭流程。首个 `Runtime.Close` 调用者触发 `Running -> Closing` 并决定最终关闭结果；并发调用者等待该结果，不启动第二次清理。等待者自己的 context 可以提前结束等待并返回其 cause，但不会取消已经开始的关闭流程。Runtime 到达 `Closed` 后，后续调用直接返回已保存的关闭结果，包括首次关闭产生的超时或 Service 清理错误。

`Close()` 用于释放本地资源。`Close()` 不应再发起新的业务 `Call`。

Runtime 关闭会先阻止新的 Service 创建，再等待已经进入 `Init` 的创建流程完成。`Init` 失败或在 Runtime 进入 Closing 后才完成时，Runtime 仍调用该 Service 的 `Close()`，释放初始化过程中已取得的部分资源。

如果 Service 内部有多个清理函数，应由 Service 自己在 `Stop(ctx)` 中编排。Core Runtime 不提供全局 `atexit` 注册表，也不替业务决定清理顺序。

## 超时策略

Runtime 必须为停止流程设置超时：

```go
type ServicePolicy struct {
    StopTimeout      time.Duration
    CloseTimeout     time.Duration
    LifecycleTimeout time.Duration
}
```

- `StopTimeout`：`Service.Stop(ctx)` 的执行期限。
- `CloseTimeout`：`Service.Close()` 的执行期限。
- `LifecycleTimeout`：从 Stop 请求被接受开始计算的整体期限，包含此前 Mailbox 消息的等待时间；默认值是 `StopTimeout + CloseTimeout`。

所有期限都必须来自显式策略或调用方 context，禁止增加未写入策略的固定宽限时间。

超时后 Runtime 必须：

```text
Record stop timeout
  ↓
Mark Failed
  ↓
Wake pending sessions
  ↓
Remove registry
  ↓
Report to Supervisor / Monitor
```

超时不能导致 Service 永久卡在 `ServiceStopping`。

如果当前 handler、`Stop` 或 `Close` 不响应超时，Runtime 只能强制释放自己拥有的 Mailbox、PendingCall、Registry、Timer 和指标状态。Go 不能安全终止业务 goroutine，因此超时后不得与仍在运行的业务清理函数并发调用新的 `Close`。

Runtime 发起的 Init、dispatch、Stop 和 Close 任务必须进入内部任务表。任务表记录 owner、任务类型、开始时间、取消函数和完成句柄。超时只把任务标记为 timed out 并尝试取消；任务记录要保留到函数真正返回，不能因为等待方已经返回就丢失句柄。

## 强制关闭边界

强制关闭只处理 Runtime 所有的结构：

```text
Mailbox
PendingCall
Registry
Timer binding
Monitor state
```

Runtime 不能直接修改业务对象内部状态。

如果业务需要保证持久化完成，必须在 `Stop(ctx)` 内完成，并正确响应 `ctx.Done()`。

## Stop 失败流程

```text
Service.Stop(ctx) returns error or timeout
  ↓
Record error
  ↓
If Stop returned normally, call Service.Close
  ↓
Remove registry
  ↓
Wake pending sessions
  ↓
Supervisor decides restart or destroy
```

Stop 超时或调用方 context 取消时，Runtime 不再并发调用 `Close()`；实例直接标记为 `ServiceFailed` 并释放 Runtime 自有结构。普通 Stop error 仍继续调用 Close，并汇总两阶段错误。

普通 Stop、Close error 分别记录 `service_stop_errors_total`、`service_close_errors_total` 和结构化日志。超时另行记录阶段超时指标。

`Runtime.Close` 自身超时返回 `ErrCloseTimeout`；调用方 context 主动取消则保留 `context.Canceled` 或调用方提供的 cause。关闭期限到达时仍未完成的 Service 确定性标记为 `ServiceFailed`，不能由并发 CAS 的先后决定 `Closed` 或 `Failed`。

## 失败处理

Service panic 不应导致进程退出。

Runtime 捕获 panic 后：

```text
Mark Failed
  ↓
Supervisor decides policy
  ↓
Destroy or Restart
```

## 策略

临时 Battle：

```text
panic -> Destroy
```

Player：

```text
panic -> Snapshot/Restore -> Restart
```

Wallet：

```text
panic -> protect state -> fail fast -> manual or policy recovery
```

## 规则

1. `CreateService` 不返回 Service 指针。
2. `Stop` 后不接受新消息。
3. Pending Call 必须被唤醒。
4. Registry 必须最终删除关闭 Service。
5. Stop 过程必须可观测。
6. `Stop(ctx)` 必须尊重 `ctx.Done()`。
7. `Close()` 不得发起新的业务 `Call`。
8. Service 退出超时后不能永久占用 Registry。
9. Runtime.Close 必须停止全部 Service、汇总停止错误、取消 Timer、失败全部 PendingCall，再停止 Scheduler。
10. Runtime 进入 Closing 后拒绝 CreateService、Send、Call 和 After。
11. Runtime 与 Service 并发创建时，CreateService 不得在 Runtime 进入 Closing 后返回一个仍处于 Running 的实例。
12. Runtime.Close 遇到已经处于 `Stopping` 的 Service 时，必须复用并等待现有停止结果，不能并发启动第二次清理。
13. Service 不得直接创建 goroutine；Runtime 创建的 Service 执行任务必须追踪到真正返回。
14. Runtime.Close 期间的 Stop 不得发起新的 Send、Call 或 After；跨 Service 善后由进入 Closing 前的 Drain 完成。
15. 生命周期整体期限必须由 `LifecycleTimeout` 或调用方 context 明确给出，不能使用隐藏宽限值。
16. Runtime.Close 只能执行一次；并发和重复调用必须复用并返回已保存的关闭结果。
17. Runtime.Stop 遇到仍在清理的同一实例时必须等待现有结果，不能并发执行第二次 Stop 或 Close。
