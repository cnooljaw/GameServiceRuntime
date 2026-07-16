# RFC-0100：Service 模型

> 状态：草案  
> 范围：Core Runtime  
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
```

后续可选能力：

```go
type Stateful interface {
    Snapshot() ([]byte, error)
    Restore([]byte) error
}
```

## ServiceContext

Service 通过 `ServiceContext` 使用 Runtime 能力。

```go
type ServiceContext interface {
    Self() ServiceRef
    Send(target ServiceRef, cmd CommandID, payload any) error
    Call(ctx context.Context, target ServiceRef, cmd CommandID, payload any) (any, error)
    After(d time.Duration, cmd CommandID, payload any) (TimerID, error)
    Now() time.Time
    Logger() Logger
    Metrics() Metrics
}
```

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
    Commands *CommandRegistry
}
```

业务代码不能直接访问 `ServiceInstance`。

## 规则

1. Service 状态只能在 `Handle` 中修改。
2. Service 之间只能通过 `ServiceRef` 通信。
3. Service 不能启动常驻 goroutine 修改自身状态。
4. 外部系统要影响 Service，只能投递 Command。
5. Service panic 必须由 Runtime 捕获并交给 Supervisor。

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

