# Service

> 状态：已实现
>
> 规范：[RFC-0100](../../rfcs/RFC-0100-Core-Service.md)

## 模型

Service 是 GSR 的状态和故障隔离边界：

```text
Service = State + Mailbox + Handler + Lifecycle
```

Service 不是 goroutine，也不是可被其他 Service 直接调用的对象。Runtime 持有实例，业务只持有 `ServiceRef`。

## 接口

```go
type Service interface {
    Init(ServiceContext) error
    Handle(CommandContext, Command) error
    Stop(context.Context) error
    Close() error
}

type CommandDeclarer interface {
    Commands() []CommandID
}
```

创建 Service 时，Runtime 复制并冻结 `Commands()` 的结果。空命令集、重复 CommandID 和重复 ServiceName 都会让创建失败。

## 串行规则

- 同一个 Service 的 `Handle`、`Stop` 和 `Close` 不并发执行。
- 业务状态只能在 `Handle` 中响应 Command 修改。
- Service 不直接创建 goroutine；延迟工作使用 Timer，跨职责工作拆成独立 Service。
- Service 之间只通过 `ServiceRef` 和 `Send`/`Call` 通信。

## 已实现边界

`Runtime.CreateService` 完成 ID 分配、Registry 注册、Init 任务追踪和状态切换。Init、Handle、Stop、Close 的 panic 都由 Runtime 隔离；超时任务会保留观测记录，直到函数真实返回。

Supervisor 重启策略和业务状态恢复不属于 Core Service，分别由 [RFC-0220](../../rfcs/RFC-0220-Tooling-Supervisor.md) 和 [RFC-0210](../../rfcs/RFC-0210-Tooling-Snapshot.md) 定义。
