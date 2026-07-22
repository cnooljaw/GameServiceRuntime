# RFC-0220：Supervisor 与故障恢复

> 状态：待实现
> 目标阶段：Phase 7E
> 范围：Runtime Tooling
> 依赖：[RFC-0100](RFC-0100-Core-Service.md)、[RFC-0110](RFC-0110-Core-ServiceRef.md)、[RFC-0170](RFC-0170-Core-Timer.md)、[RFC-0180](RFC-0180-Core-Lifecycle.md)、[RFC-0192](RFC-0192-Core-Runtime-Inspection.md)、[RFC-0200](RFC-0200-Tooling-Discovery.md)、[RFC-0210](RFC-0210-Tooling-Snapshot.md)

## 目的

本文定义 Service 实例失败后的通知、决策和新实例恢复边界。Supervisor 处理不可变故障事实，不接管 Core 生命周期，不复活已经失败的对象，也不把业务工厂或持久化逻辑塞进普通 Service。

## 已裁决结论

Phase 7E 采用以下规则：

1. Core 捕获 panic 后关闭并移除失败实例；Supervisor 只能创建新实例。
2. 旧 `ServiceRef` 不复用。长期身份通过 `ServiceName`、Discovery 名字或稳定业务 `ServiceKey` 重新绑定。
3. panic 后的内存状态不可信；Supervisor 不在失败对象上临时调用 Snapshot。
4. 自动恢复只能使用最近一次已提交 Snapshot，或从确定的初始状态重建。
5. Handler panic 进入失败通知协议；`Init`、`Stop`、`Close` 的错误或 panic 由对应生命周期 owner 处理。
6. `BattleService` 默认销毁；`WalletService` 默认停止自动恢复并进入人工或业务补偿流程。
7. 重启有单次故障尝试上限、滚动窗口上限和指数退避，不能形成无限创建循环或重启风暴。
8. Supervisor 不吞掉错误；失败通知、恢复安排、抑制、创建失败、发布失败和最终放弃都必须可观测。
9. Supervisor 自身不受本 Supervisor 管理，也不得形成自我重启环。

## 目标

Phase 7E 实现：

- Handler panic 的不可变失败通知。
- 稳定业务 Key、实例 `ServiceRef`、恢复策略和代际之间的关系。
- `RestartNever`、`RestartOnFailure` 和 `DestroyOnFailure`。
- 有界尝试、滚动窗口和指数退避。
- 从最近一次已提交 Snapshot 构造新 Service 的组合根示例。
- 新实例先准备、再登记新代际、最后发布长期名字。
- 注册状态和恢复结果查询，以及 Core Metrics 和结构化日志观测。

`RestartAlways` 不进入第一版。正常 Stop 后重新创建属于部署编排或 Drain，不属于故障恢复。

## 非目标

- 修改或复活旧 Service 实例。
- 在 panic 后读取旧实例状态。
- Go 进程崩溃后的跨进程自动恢复。
- Cluster 选主、跨节点迁移、多副本一致性或完整 Supervisor Tree。
- Wallet 自动修复、丢弃账本错误或重复执行未确认结算。
- 把业务工厂、Snapshot Schema 或重启策略加入 Core `Service` 接口。
- 通过轮询 Metrics 猜测具体实例是否失败。
- 失败通知的磁盘持久队列。
- 远程 Supervisor 协议和 Cluster Codec。第一版 Supervisor、Runner 与被管理 Service 位于同一 Runtime 节点。

## 分层与 owner

```text
Supervised Service decorator
  -> Send immutable FailureNotice Command
  -> re-panic
  -> Core finalizes failed instance

SupervisorService
  -> validate source, key, ref and generation
  -> decide strategy, attempt budget and backoff
  -> submit bounded RecoveryTask

Runner (non-Service lifecycle owner)
  -> wait backoff
  -> Launcher.Prepare: load committed state and CreateService
  -> Call Supervisor: record prepared ref and generation
  -> Launcher.Commit: publish long-lived binding
  -> Call Supervisor: commit or report failure
  -> Launcher.Abort on any uncommitted or stale result
```

职责边界：

- Decorator 只捕获 `Handle` panic、发送失败事实并重新 panic。它不决定策略，不执行 Store I/O，不创建实例。
- `SupervisorService` 只在 Mailbox 中维护注册、策略、代际、尝试和状态。Handler 不等待退避，不访问 Store，不调用 `CreateService`。
- `Runner` 是组合根显式持有和关闭的非 Service owner。它拥有有界队列、固定 worker、退避等待、可取消 I/O 和任务真实返回追踪。
- `Launcher` 是 Runner 使用的窄能力，不暴露给普通业务 Service。它封装 Snapshot 加载、业务工厂、`CreateService` 和名字发布适配器。
- Core 不导入 Supervisor 类型；Tooling 单向依赖 Core。

Supervisor 的 `Close` 不关闭 Runner。组合根先停止新业务流量，再关闭 Runtime 和 Runner，并处理两者各自的关闭结果。Runner 关闭超时只能结束等待，不能遗失仍未真实返回的 launcher 任务记录。

## Decorator 与失败事实

```go
type ServiceKey struct {
    Namespace string
    ID        string
}

type FailureKind uint8

const (
    FailureHandlerPanic FailureKind = iota + 1
)

type FailureNotice struct {
    Key        ServiceKey
    FailedRef  gsr.ServiceRef
    Generation uint64
    OccurredAt time.Time
    Kind       FailureKind
}

type DecoratorConfig struct {
    Key        ServiceKey
    Generation uint64
    Supervisor gsr.ServiceRef
}

func Decorate(gsr.Service, DecoratorConfig) (gsr.Service, error)
```

Decorator 必须保持被包装 Service 的 `Commands()`、`Init`、正常 `Handle`、`Stop`、`Close` 顺序和 Reply 语义，不暴露被包装对象。`Handle` defer 只在捕获 panic 时执行：

1. 使用当前 `ServiceContext.Self()` 构造 `FailedRef`。
2. 使用 `ServiceContext.Now()` 构造 `OccurredAt`。
3. 使用 `ServiceContext.Send` 把通知投递给 Supervisor，因此 Envelope `Source` 是失败实例。
4. 投递失败时记录 `supervisor_failure_notice_delivery_errors_total` 和结构化错误日志。
5. 使用原 panic value 重新 panic，由 Core 完成 Call 失败、Close、Registry、Timer 和 PendingCall 清理。

`FailureNotice` 不携带 Service 指针、panic value、堆栈全文、明文 secret 或业务状态。诊断详情由 Core 和 Decorator 写入结构化日志；通知只包含稳定、可校验的分类。

Supervisor 必须同时校验：

- `CommandContext.Source() == FailureNotice.FailedRef`。
- `Key` 已注册且结构有效。
- `FailedRef` 和 `Generation` 等于该 Key 当前已提交实例。
- 当前状态允许从 Running 进入故障决策。

一个失败代际只能触发一次决策。同代际重复通知返回 `ErrDuplicateNotice`，旧代际或旧 `ServiceRef` 返回 `ErrStaleNotice`，其它结构错误返回 `ErrInvalidNotice`。

## 生命周期错误边界

`Init`、`Stop` 和 `Close` 不进入 `FailureNotice`：

- 初次创建和恢复期间的 `Init` error 或 panic 由调用 `CreateService` 的 Launcher 直接得到，分类为准备或创建失败。
- 显式 `Runtime.Stop` 的 Stop/Close error 或 panic 由发起 Stop 的部署编排、Drain 或调用方处理。
- `Runtime.Close` 期间的生命周期失败属于整体终止结果，不能触发新的恢复工作。
- 已经进入停止流程的实例不是一次 Handler 故障；为它发送恢复通知会混淆正常退出与故障恢复。

## 注册、身份与状态

```go
type Registration struct {
    Key        ServiceKey
    Ref        gsr.ServiceRef
    Generation uint64
    Policy     RestartPolicy
}

type ServiceStatus uint8

const (
    ServiceRunning ServiceStatus = iota + 1
    ServiceRestartStopped
    ServiceDestroyed
    ServiceBackoff
    ServicePreparing
    ServicePublishing
    ServiceRecoveryFailed
    ServiceRestartSuppressed
)

type Record struct {
    Registration     Registration
    Status           ServiceStatus
    Attempt          uint64
    AttemptsInFault  int
    RestartsInWindow int
    LastFailure      RecoveryFailure
}
```

`ServiceKey` 是跨实例稳定身份，`Generation` 是该 Key 在一个 Supervisor 注册生命周期中的已提交实例代际。初始注册要求非零 Generation；每次成功恢复只增加一代。准备或发布失败不消耗 Generation，但消耗本次故障的尝试预算。

第一版注册规则：

1. 组合根先创建 Supervisor，再创建带 Decorator 的业务 Service，最后在开放业务流量或发布长期名字前注册。
2. `Registration.Ref` 必须是与 Supervisor 同节点的具体 `ServiceRef`。
3. 不得注册 Supervisor 自身，也不得重复注册相同 Key。
4. 注册和查询使用 typed Client 通过 Call 进入 Supervisor Mailbox；调用方不能读取内部 map。
5. `RestartNever` 的终态是 `ServiceRestartStopped`；`DestroyOnFailure` 的终态是 `ServiceDestroyed`。二者都不自动创建新实例，但语义不同。

公开 Client：

```go
type CommandCaller interface {
    Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error)
}

func NewClient(CommandCaller, gsr.ServiceRef) (*Client, error)
func (c *Client) Register(context.Context, Registration) error
func (c *Client) Get(context.Context, ServiceKey) (Record, error)
```

## 恢复策略

```go
type RestartStrategy uint8

const (
    RestartNever RestartStrategy = iota
    RestartOnFailure
    DestroyOnFailure
)

type RestartPolicy struct {
    Strategy    RestartStrategy
    MaxAttempts int
    MaxRestarts int
    Window      time.Duration
    MinBackoff  time.Duration
    MaxBackoff  time.Duration
}
```

`RestartOnFailure` 要求全部限制为正数，且 `MaxBackoff >= MinBackoff`：

- `MaxAttempts` 限制一个失败代际最多执行多少次 Prepare/Commit 尝试。创建或发布持续失败最终进入 `ServiceRecoveryFailed`，不会无限循环。
- `MaxRestarts` 限制 `Window` 内成功提交的新代际数量。达到上限后下一次故障进入 `ServiceRestartSuppressed`。
- 第 `n` 次尝试的退避为 `min(MinBackoff * 2^(n-1), MaxBackoff)`，计算必须防止 duration 溢出。
- 恢复成功后清零本次故障尝试数，但保留窗口内成功重启时间。
- `RestartNever` 和 `DestroyOnFailure` 不接受非零恢复限制，避免看似配置但实际不生效。

默认业务建议：

| Service | 默认策略 | 恢复来源 |
|---|---|---|
| BattleService | `DestroyOnFailure` | 不自动恢复；业务决定退款、判负或重开。 |
| PlayerService | `RestartOnFailure` | 最近一次已提交 Snapshot 或权威持久化状态。 |
| WalletService | `RestartNever` | 权威账本和人工或业务补偿流程。 |
| DiscoveryService | `RestartOnFailure` | 空状态或外部配置；内存租约不伪装成已恢复。 |

## Runner 与有界恢复任务

```go
type RecoveryTask struct {
    Supervisor gsr.ServiceRef
    Key        ServiceKey
    FailedRef  gsr.ServiceRef
    Generation uint64
    Attempt    uint64
    Delay      time.Duration
}

type RecoveryExecutor interface {
    Submit(RecoveryTask) error
}

type RunnerConfig struct {
    Workers             int
    QueueSize           int
    AttemptTimeout      time.Duration
    ResultTimeout       time.Duration
    ResultRetryInterval time.Duration
    Logger              *slog.Logger
}
```

Runner 的队列固定容量，`Submit` 不阻塞 Supervisor Handler。队列满返回 `ErrRecoveryQueueFull`，该次注册进入明确失败状态并记录指标；不得改成无界 goroutine 或无界 slice。

Runner 固定 worker 数量。每项任务在 Runner goroutine 中等待退避，再使用带 `AttemptTimeout` 的 context 执行 Launcher。恢复结果使用 Runtime 根来源 Call 回 Supervisor；Supervisor 校验来源为同节点 Runtime 根、Key、Generation 和 Attempt。Mailbox 满时 Runner 在 `ResultTimeout` 内按 `ResultRetryInterval` 重试，超时后写结构化日志，不伪造成功。

Runner 不是 Runtime 内部任务，因此不进入 `Runtime.Inspect().Tasks`。它自己的 `Close(ctx)` 必须取消等待和可取消 I/O，并等待 worker 真实返回；调用方 context 超时只结束本次等待，后续 Close 仍可继续等待真实完成。

## 两阶段 Launcher

```go
type LaunchRequest struct {
    Supervisor gsr.ServiceRef
    Key        ServiceKey
    FailedRef  gsr.ServiceRef
    Generation uint64
    Attempt    uint64
}

type Launcher interface {
    Prepare(context.Context, LaunchRequest) (gsr.ServiceRef, error)
    Commit(context.Context, LaunchRequest, gsr.ServiceRef) error
    Abort(context.Context, LaunchRequest, gsr.ServiceRef) error
}
```

阶段语义：

1. `Prepare` 加载最近一次已提交 Snapshot 或确定初始状态，构造带新 Generation Decorator 的 Service，并调用 `CreateService`。新实例此时不得通过长期名字接收业务流量。
2. Runner 把 prepared `ServiceRef` Call 给 Supervisor。Supervisor 校验 Attempt 后先记录待发布的新 Ref 和 Generation。
3. `Commit` 原子或幂等地发布长期名字。成功后 Runner 再通知 Supervisor 把新代际变为 `ServiceRunning`。
4. Prepare 结果迟到、prepared 记录被拒绝、Commit 失败或 commit 结果迟到时，Runner 必须调用 `Abort`。
5. `Abort` 必须幂等地撤销指向该 Ref 的绑定并停止新实例。Abort 失败时禁止继续创建更多实例，状态进入 `ServiceRecoveryFailed`。

提供的 Runtime launcher 使用以下窄 seam：

```go
type RuntimeControl interface {
    CreateService(gsr.ServiceSpec) (gsr.ServiceRef, error)
    Stop(context.Context, gsr.ServiceRef) error
}

type ServiceFactory interface {
    Build(context.Context, ServiceKey, uint64) (gsr.ServiceSpec, error)
}

type BindingPublisher interface {
    Publish(context.Context, ServiceKey, gsr.ServiceRef) error
    Withdraw(context.Context, ServiceKey, gsr.ServiceRef) error
}
```

Factory 返回的 `ServiceSpec.Name` 必须为空；名字只能在 Commit 阶段发布。没有长期名字的 Service 可以省略 `BindingPublisher`，通过 `Record.Registration.Ref` 取得新地址。Snapshot Store I/O 属于 Factory/Launcher 调用路径，实际执行 owner 仍是 Runner，不是 Service Handler。

## 名字发布失败的收敛

第一版不保留孤立运行实例，也不把未发布实例无限挂起：

1. Publish 成功前旧名字可以继续指向已失败的旧 Ref，或保持不存在；不能提前指向 prepared Ref。
2. Publish 失败后 Runner 调用 Abort，先条件撤销 prepared Ref 的绑定，再停止该实例。
3. Abort 成功后可以在剩余尝试预算内重新 Prepare；下一次使用新的 Attempt，但 Generation 不变。
4. Abort 失败时停止自动恢复，避免产生多个可能可达的孤立实例。
5. `BindingPublisher.Publish` 返回 error 不代表调用方可以假设“什么都没发生”；`Withdraw(Key, Ref)` 必须按 Ref 做条件撤销，不能删除已经属于更新实例的绑定。

Discovery adapter 使用 `RegisterName` 更新和带 Ref 的 `UnregisterName` 条件撤销。`Runtime.ResolveRemote` 仍只负责已知节点的本地查询，不承担 Supervisor 或全局名字发布职责。

## 失败通知投递裁决

失败通知处于 panic 路径，第一版采用有界、非阻塞、可观测的 fail-closed 行为：

- Decorator 只尝试一次 `ServiceContext.Send`，不等待、不重试 Store、不创建 goroutine。
- Mailbox 满、Supervisor 已关闭或 Runtime 正在关闭时，记录 `supervisor_failure_notice_delivery_errors_total` 和结构化日志，然后继续重新 panic。
- 不增加独立磁盘队列；它会引入序列化、磁盘故障、重放身份和关闭顺序等新协议，超出 Phase 7E。
- 投递失败时 Core 仍确定性隔离旧实例，但自动恢复不保证发生。运维必须根据指标告警并通过编排或人工恢复，不能把 Supervisor 的旧 Running 记录当成实例仍存活的证明。

这是明确的可靠性边界，不是静默丢弃。若未来需要跨进程可靠恢复，应设计持久化故障日志和 fencing，而不是在 panic defer 中临时补一个无界队列。

## 错误与可观测性

稳定错误至少包括：

- `ErrInvalidConfig`、`ErrInvalidKey`、`ErrInvalidPolicy`、`ErrInvalidRegistration`。
- `ErrAlreadyRegistered`、`ErrServiceNotRegistered`。
- `ErrInvalidNotice`、`ErrDuplicateNotice`、`ErrStaleNotice`。
- `ErrRestartSuppressed`、`ErrRecoveryQueueFull`、`ErrRunnerClosed`。
- `ErrSnapshotNotFound`、`ErrRecoveryFailed`、`ErrCreateFailed`、`ErrNamePublishFailed`、`ErrAbortFailed`、`ErrStaleRecovery`。

错误文本不能作为策略分支依据。跨 Runner Command 只传稳定 `RecoveryFailure` 分类，原始错误留在结构化日志中，不进入状态快照或 Cluster wire。

Core Metrics 名称：

| 指标 | 含义 |
|---|---|
| `supervisor_failure_notices_total` | 通过校验并开始决策的失败通知数。 |
| `supervisor_failure_notice_delivery_errors_total` | Decorator 无法把通知送入 Supervisor Mailbox 的次数。 |
| `supervisor_failure_notices_duplicate_total` | 同代际重复通知数。 |
| `supervisor_failure_notices_stale_total` | 旧 Ref 或旧 Generation 通知数。 |
| `supervisor_restarts_scheduled_total` | 成功提交给 Runner 的恢复尝试数。 |
| `supervisor_restarts_succeeded_total` | 完成名字发布并提交新代际的次数。 |
| `supervisor_restarts_failed_total` | Prepare、Commit、Abort 或结果处理失败次数。 |
| `supervisor_restarts_suppressed_total` | 因窗口或预算停止自动恢复的次数。 |

这些指标通过 `Runtime.Inspect().Metrics` 读取，不新增第二套 Core 观测入口。`Record` 是 Supervisor 自身状态查询，不替代 Runtime Inspection。

## 验收

Phase 7E 必须覆盖：

1. Handler panic 后 Decorator 发送不可变通知并重新 panic；旧实例被 Core 清理，PendingCall 得到 `ErrServiceFailed`。
2. 通知 Source、Key、Ref、Generation 校验；同代际重复通知只触发一次决策，旧代际不影响新实例。
3. `RestartNever`、`DestroyOnFailure` 和 `RestartOnFailure` 的不同终态。
4. `MaxAttempts`、`MaxRestarts`、Window、指数退避、连续创建失败和 Snapshot 不存在。
5. Runner 队列有界、关闭可取消退避、任务记录保留到真实返回。
6. 从已提交 Snapshot 创建新实例，且 `ServiceRef` 变化、Generation 增加。
7. prepared Ref 先登记、长期名字后发布；发布失败撤销绑定并停止新实例。
8. Abort 失败停止后续自动恢复，迟到 prepared/commit 结果不能覆盖当前状态。
9. 失败通知 Mailbox 满和 Supervisor 不可用时有指标与日志，不阻止 Core 清理。
10. Supervisor 自身不能注册，也不会形成自我重启环。
11. 无 Service goroutine、无 Core 对 Tooling 的反向依赖。
12. `go test ./...`、`go vet ./...`、`go test -race ./...` 和关键并发重复测试通过。
