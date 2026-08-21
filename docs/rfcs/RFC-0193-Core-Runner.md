# RFC-0193：Core Runner

> 状态：已接受
> 范围：Core Runtime
> 依赖：[RFC-0100](RFC-0100-Core-Service.md)、[RFC-0160](RFC-0160-Core-Scheduler.md)、[RFC-0180](RFC-0180-Core-Lifecycle.md)、[RFC-0192](RFC-0192-Core-Runtime-Inspection.md)
> 依据：Skynet 的每消息协程和 `skynet.call` 让出调度思想，以及 GSR 中重复出现的有界外部 worker pool

## 目的

本文定义 Runtime 拥有的固定容量 Runner，用于在 Service Mailbox 外执行 HTTP、数据库、Redis、文件系统和 Runtime 生命周期等可能阻塞的工作。

Runner 解决执行资源、取消、关闭、真实返回追踪和结果重新入箱问题。它不拥有业务状态，也不把同一 Service 改成多个可交错执行的 handler。

## 目标

- 提供固定 worker 数和有限队列。
- 提供 `Submit` 异步结果入箱模式。
- 提供 `Await` 原栈恢复模式。
- Runtime 统一拥有 Runner 生命周期并通过 `Inspect` 观测。
- 关闭、取消、panic、队列满和结果投递失败具有稳定语义。
- 替代业务和 Tooling 中重复实现的通用 worker pool 机械代码。

## 非目标

- 不提供动态扩缩容、优先级、work stealing 或分布式任务队列。
- 不保证任务持久化、自动重试、Exactly Once 或业务幂等。
- 不替业务生成 OperationID、TurnRevision、小局身份或其他 fencing。
- 不允许 processor 读取或修改 Service 可变状态。
- 不实现 Skynet 同一 Service 多协程 handler 的交错执行。
- 不保证强杀忽略 `context.Context` 的 Go 函数。
- 不替代包含多阶段 Call、重试、补偿或结果投递失败清理的专用工作流执行器；这类组件可以使用 Core Runner 能表达的部分，但不能为了统一名字丢失原有状态机。

## 分层与依赖

Runner 属于 Core，因为 `game`、`tooling` 和 `examples/nhsk` 已分别重复实现相同的固定 worker、有限队列、取消、关闭和结果 Command 投递规则；这些规则又必须与 Scheduler 许可和 Runtime Close 协同，无法由一个不了解 Runtime 的普通 helper 完整保证。

Core Runner 只理解泛型请求、结果、当前 `CommandContext`、`ServiceRef` 和 `CommandID`，不得引用 `game/`、Tooling 或玩法类型。processor 由组合根注入，可以调用外围 adapter，但不得保存 `CommandContext`、`ServiceContext`、Service 指针或可变业务对象。

业务请求在成功提交后把所有权转给 Runner。请求包含 slice、map、pointer 或其他可变引用时，提交方必须先深拷贝或把它们视为不可变；Core 不进行反射式深拷贝。

## 公开契约

```go
type RunnerName string

type RunnerConfig struct {
    Name      RunnerName
    Workers   int
    QueueSize int
}

type RunnerProcessorFunc[Request, Result any] func(context.Context, Request) (Result, error)

type RunnerResult[Result any] struct {
    Value Result
    Err   error
}

type Runner[Request, Result any] struct { /* private */ }

func NewRunner[Request, Result any](
    runtime *Runtime,
    config RunnerConfig,
    processor RunnerProcessorFunc[Request, Result],
) (*Runner[Request, Result], error)

func (r *Runner[Request, Result]) Submit(
    ctx context.Context,
    target ServiceRef,
    command CommandID,
    request Request,
) error

func (r *Runner[Request, Result]) Await(
    ctx context.Context,
    owner CommandContext,
    request Request,
) (Result, error)

func (r *Runner[Request, Result]) Close(ctx context.Context) error
```

`RunnerName` 在一个 Runtime 生命周期内唯一，关闭后不复用。`Workers` 和 `QueueSize` 都必须大于零。Runner 只把异步结果投递给同一 Runtime 的非零本地 `ServiceRef`；跨节点任务应由目标节点自己的组合根执行。

### Submit

`Submit` 使用非阻塞入队：

- 返回 nil 表示任务已被 Runner 接受，不表示 processor 已执行或结果已送达。
- 队列已满立即返回 `ErrRunnerQueueFull`。
- Runner 正在关闭或已经关闭时返回 `ErrRunnerClosed`。
- processor 完成后，Runner 尝试一次 `Runtime.Send(target, command, RunnerResult[Result]{...})`。
- 结果 Command 的 Source 是当前 Runtime 节点端点 `{Node: runtime.NodeID, ID: 0}`。
- 投递失败不重试，只记录指标和日志；业务若需要恢复，必须用自己的操作身份和状态机定义。

多个 worker 可以并行执行任务。队列只定义接受顺序，不定义开始、完成或结果 Command 的顺序。

### Await

`Await` 只能传入当前这一次 `Handle` 收到且仍在有效期内的 `CommandContext`。Core 由该上下文定位当前 Service，并拒绝已返回的 Handler 上下文、错误 Runtime 的上下文和同一次 Handler 的重复并发 Await：

1. Runtime 归还当前 handler 占用的 Scheduler 执行许可。
2. 同一 Service 仍保持 busy，Mailbox 不处理下一条 Command。
3. 其他 Service 可以使用被归还的许可继续运行。
4. processor 返回、调用 context 取消或 Runner 关闭后，Runtime 重新获取许可。
5. 原 handler 在同一 Go 调用栈的 `Await` 返回点继续执行。

`Await` 不把完成结果包装为 Command，也不产生同一 Service 的 handler 重入。它适用于必须保持当前 Service 串行快照、且等待期间不需要处理其他 Command 的短外部工作。需要 Service 在等待期间继续响应消息时，必须使用 `Submit` 和显式 pending 状态。

在 `Init`、`Stop`、`Close`、没有当前有效 `CommandContext` 的普通 goroutine、已返回的 Handler、错误 Runtime 的上下文或已经让出许可的 Handler 中调用 `Await`，返回 `ErrRunnerAwaitNotAllowed`。Service 不得把仍有效的 `CommandContext` 发布给其他 goroutine；该行为同时违反 Service 单线程状态规则，Go Runtime 无法可靠识别调用它的 goroutine 身份。

## 状态与生命周期

```go
type RunnerStatus int

const (
    RunnerRunning RunnerStatus = iota + 1
    RunnerClosing
    RunnerClosed
)
```

`NewRunner` 在 Runtime 内注册名字后才启动固定 worker。Runtime 已经 Closing/Closed 时创建失败。

`Runner.Close` 是幂等操作。第一个调用者原子执行 `Running -> Closing`，停止接受新任务，取消 active processor，并使所有已接受但尚未开始的任务以 `ErrRunnerClosed` 完成；所有调用者等待同一个真实关闭结果。调用者 context 可以提前结束等待，但不能遗失仍在运行的 worker，也不能把 Runner 伪装成 Closed。

最后一个 worker 真实返回后状态才变为 `RunnerClosed`。关闭后的 Runner 继续保留在当前 Runtime 的 Inspection 中，名字不复用。

`Runtime.Close` 进入 Closing 后，先关闭所有 Runner，再执行 Service Stop。这样等待中的 `Await` 能恢复并退出，已接受的 `Submit` 任务也有一次终态完成机会。全局关闭期间 Runtime 已拒绝新 Command，因此异步结果投递可以失败；这是终止语义，不承诺在 Runtime Closing 后继续驱动业务状态机。

如果 processor 忽略 context，Go 无法安全终止它。`Runner.Close` 或 `Runtime.Close` 可以按调用方期限返回，但 Runner 保持 Closing 并继续出现在 Inspection，直到 worker 真实返回。

## 错误与失败语义

Core 增加以下稳定错误：

- `ErrInvalidRunnerConfig`：Runtime、名字、worker、队列或 processor 无效。
- `ErrRunnerNameConflict`：当前 Runtime 已存在同名 Runner。
- `ErrInvalidRunnerTarget`：异步目标不是当前 Runtime 的非零本地 ServiceRef，或 CommandID 为零。
- `ErrRunnerQueueFull`：有限队列不能立即接受任务。
- `ErrRunnerClosed`：Runner 已进入 Closing/Closed，或关闭取消了任务。
- `ErrRunnerAwaitNotAllowed`：`Await` 不在当前 Service 串行 handler 路径。
- `ErrRunnerPanic`：processor panic；worker 捕获 panic 后继续服务后续任务。

`Await` 的调用 context 结束时返回 `context.Cause(ctx)`，不复用只属于 Call/Reply 的 `ErrTimeout`。任务 processor 同时收到该取消原因；即使 processor 忽略取消并迟到返回，已返回的 handler 也不会再次收到结果。

`Submit` 的 context 只约束该任务 processor。任务已经接受后，调用方 context 取消不撤销“最多一次结果投递尝试”；结果中的 `Err` 保存取消原因。

## 并发与所有权

- worker goroutine 由 Runner/Runtime 拥有，数量固定，必须等到真实返回。
- processor 不得直接修改 Service 状态，也不得保存或在 Handler 外使用 `CommandContext`、`ServiceContext`。
- `Submit` 结果只能经 Command 进入目标 Mailbox。
- `Await` 通过持有当前 `serviceInstance` 的调度所有权保持同一 Service 串行，不开放新的状态写入口。
- 多 worker 结果允许乱序；业务使用 `Submit` 时必须按真实领域需要校验阶段、OperationID、TurnRevision 或其他窄身份。
- Core 不自动增加通用 Revision 或幂等表。

## 可观测性

`Runtime.Inspect()` 增加按 `RunnerName` 排序的独立副本：

```go
type RunnerInspection struct {
    Name           RunnerName
    Status         RunnerStatus
    Workers        int
    QueueDepth     int
    Active         int
    Submitted      uint64
    Completed      uint64
    Failed         uint64
    Rejected       uint64
    DeliveryFailed uint64
}
```

Core 不提供独立 `Runner.Inspect()`，保持 `Runtime.Inspect()` 是唯一只读观测入口。

聚合指标至少包括：

- `runner_tasks_submitted_total`
- `runner_tasks_completed_total`
- `runner_tasks_failed_total`
- `runner_tasks_rejected_total`
- `runner_result_delivery_failed_total`

processor panic 和异步结果投递失败记录结构化日志；请求、结果和业务 payload 不进入 Core 日志。

## 验收

必须覆盖：

- 配置校验、同名冲突和关闭后名字不复用。
- `Submit` 非阻塞队列满、精确本地目标、Runtime root Source 和单次结果投递。
- 多 worker 结果允许乱序。
- `Await` 让其他 Service 运行，但暂停同一 Service Mailbox，并在原调用点恢复。
- 非 handler、错误 Runtime、重复让出许可时拒绝 `Await`。
- context 取消、processor panic、worker 继续运行。
- 显式 Close 的取消、幂等、并发和超时后真实任务仍可观测。
- Runtime Close 自动关闭 Runner，并唤醒等待中的 `Await`。
- Inspection 排序、副本、计数和异步投递失败指标。
- Submit/Close 竞争和 Race 检查。

## 阶段收尾

Core Runner 已用于通用账本写入，以及宁海双扣的 AI 请求、回放写入、自定义牌堆加载和诊断导出。它让 Service 不必复制 worker、队列、取消和关闭代码，同时保证异步结果仍经 Command 回到 Mailbox。

本 RFC 不解决任务持久化、自动重试、Exactly Once，也不强行替换带多阶段 Call、补偿或投递失败清理的专用工作流执行器。下一阶段仍需在真实旧 GameMaster 联调中验证外部 adapter 的阻塞边界，并继续按业务状态机决定使用 `Await` 还是 `Submit`。
