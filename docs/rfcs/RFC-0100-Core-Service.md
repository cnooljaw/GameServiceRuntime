# RFC-0100：Service 模型

> 状态：已接受
> 范围：Core Runtime
> 依赖：[RFC-0000](RFC-0000-Foundation-Glossary.md)、[RFC-0001](RFC-0001-Foundation-Design-Principles.md)、[RFC-0002](RFC-0002-Foundation-Conflict-Resolution.md)
> 依据：`docs/learn/006-Go-Service-Runtime概要设计与约定.md`

## 目的

本文定义 Service 的职责、生命周期入口和禁止行为。

## 定义

Service 是可寻址的运行时实体。

它拥有：

- 状态。
- Mailbox。
- Command 处理入口。
- 生命周期。
- Runtime 分配的 ServiceRef。

它不拥有：

- Runtime 内部 Registry。
- Scheduler。
- Cluster 连接。
- 其他 Service 的对象指针。

## 接口草案

```go
type Service interface {
    Init(ctx ServiceContext) error
    Handle(ctx CommandContext, cmd Command) error
    Stop(ctx context.Context) error
    Close() error
}

type CommandDeclarer interface {
    Commands() []CommandID
}

// StartupCommandDeclarer optionally declares one Command that Runtime enqueues
// after Init succeeds and the Service has entered Running.
type StartupCommandDeclarer interface {
    StartupCommand() (Command, bool)
}
```

## CommandContext

`CommandContext` 暴露当前 Command 的目标、来源和 Reply 能力：

```go
type CommandContext interface {
    Self() ServiceRef
    Source() ServiceRef
    Reply(any) error
}
```

`Source` 是消息来源地址，不是认证凭据。Service 用它维护调用归属、租约 owner、审计和错误状态约束。Skynet dispatch 同样把 `source` 交给 Service handler。

Runtime 外部调用者统一表示为 `{Node: localNode, ID: 0}`。本地 Send、Call 与跨节点 Send、Call 必须使用相同表示；Service 发起的消息使用该 Service 自己的 `ServiceRef`。

Snapshot 不扩展 Core `Service` 接口。运行中状态只能由进入 Mailbox 的 Command 读取或修改：快照采集通过只读 Call 串行进入目标 Service；恢复由外层组合根在 `CreateService` 前构造新实例，不对运行中实例直接调用 `Restore`。具体契约见 [RFC-0210](RFC-0210-Tooling-Snapshot.md)。

## ServiceContext

Service 通过 `ServiceContext` 使用 Runtime 能力。

```go
type ServiceContext interface {
    Self() ServiceRef
    Send(target ServiceRef, cmd CommandID, payload any) error
    Call(ctx context.Context, target ServiceRef, cmd CommandID, payload any) (any, error)
    After(d time.Duration, cmd CommandID, payload any) (TimerID, error)
    Now() time.Time
    Logger() *slog.Logger
    Metrics() Metrics
}
```

`ServiceContext.Call` 只能在 `Handle` 或单独 `Runtime.Stop` 触发的串行执行路径中调用。Service 不得从自建 goroutine 使用保存下来的 Context；Runtime 依靠当前 Service 的执行许可实现 Call 等待时的让出与恢复。`Runtime.Close` 进入 Closing 后，所有新的 Send、Call 和 After 都被拒绝。

Service 实现不得直接创建 goroutine。异步业务使用 Command、Timer 或独立 Service；Runtime 创建的 Service 执行任务由内部任务表追踪。第一版不公开 `Fork` 或 `Go` API。

`StartupCommandDeclarer` 是启动后异步工作的唯一通用入口。它返回 `ok=false` 时不投递；返回的 Command 必须已经由 `Commands` 声明。Runtime 在 `Init` 成功、实例进入 `Running` 后，以该 Service 自身为 Source 把它放入 Mailbox，再从 `CreateService` 返回。它不替代 `Init`，也不保证 Handler 在 `CreateService` 返回前完成。声明必须只依赖创建时已冻结的配置，不能读取或修改运行期状态。

禁止在 `ServiceContext` 暴露：

- `Registry`
- `Scheduler`
- `Mailbox`
- 所有 Service 列表
- Cluster 内部连接

## ServiceInstance

Runtime 内部使用 `ServiceInstance` 包装业务 Service。

```go
type ServiceInstance struct {
    Ref      ServiceRef
    Handler  Service
    Mailbox  *Mailbox
    Status   ServiceStatus
    Policy   ServicePolicy
    commands *commandSet
}
```

业务代码不能直接访问 `ServiceInstance`。

## 规则

1. Service 状态只能在 `Handle` 中修改。
2. Service 之间只能通过 `ServiceRef` 通信。
3. Service 不能直接创建 goroutine；无论是否修改自身状态，都不能产生脱离 Runtime 追踪的异步任务。
4. 外部系统要影响 Service，只能投递 Command。
5. `Init`、`Handle`、`Stop`、`Close` 中的 panic 都必须由 Runtime 捕获；第一版标记 `Failed` 并隔离实例，后续交给 Supervisor 决定恢复策略。
6. Service 必须通过 `CommandDeclarer` 声明可接收的 CommandID。
7. Service 不得同步 Call 自己，Runtime 返回 `ErrCallCycle`。
8. Runtime 必须向 Handler 提供准确的 Command Source，本地与远程不能使用不同的零值约定。
9. 需要在启动后注册、重试或等待外部依赖的 Service，应使用 `StartupCommandDeclarer` 声明的启动 Command，再由 Handler 推进状态；不得在 `Init` 中阻塞或创建 goroutine。

## 反模式

禁止：

```go
type BattleService struct {
    player *PlayerService
}
```

使用：

```go
type BattleService struct {
    players map[PlayerID]ServiceRef
}
```

禁止：

```go
type BattleService struct {
    runtime *ServiceRuntime
    registry *Registry
}
```

使用：

```go
type BattleService struct {
    ctx ServiceContext
}
```

## 与 Skynet 的关系

Skynet 中的 `skynet_context` 给了我们关键启发：Service 是 Runtime 管理的上下文，而不是普通对象。

Go 版保留这个思想，但用明确接口和类型系统约束边界。
