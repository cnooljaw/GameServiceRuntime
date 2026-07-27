# RFC-0120：Command 模型

> 状态：已接受
> 范围：Core Runtime
> 依赖：[RFC-0100](RFC-0100-Core-Service.md)
> 依据：`docs/learn/006-Go-Service-Runtime概要设计与约定.md`

## 目的

本文定义 Command、CommandID、Service 分发和类型安全策略。

## 定义

Command 是 Service 的能力入口。

它不是：

- RPC Method。
- HTTP Request。
- DDD/CQRS Command。

它是 Runtime Message Entry。

## 类型草案

```go
type CommandID uint32

type Command struct {
    ID      CommandID
    Payload any
}
```

第一版允许 `Payload any`，后续通过泛型和代码生成加强类型安全。

## Command 分发

`Service.Handle` 是唯一业务处理入口：

```go
func (s *Service) Handle(ctx CommandContext, cmd Command) error
```

Runtime 不要求 Service 额外声明 Command 清单，也不维护每个 Service 的 Command 白名单。`CreateService` 已经把一个 Service 实例与 `Handle` 入口绑定；再要求 `Commands()` 会让同一个能力分别出现在声明清单和 Handler 中，增加重复、漂移和 Decorator 转发成本。

命令较少时，Service 直接使用 `switch`。命令较多时，可以在 Service 内部使用私有、创建后不再修改的函数表：

```go
type commandHandler func(CommandContext, any) error

func (s *PlayerService) Handle(ctx CommandContext, cmd Command) error {
    handler, ok := s.handlers[cmd.ID]
    if !ok {
        return fmt.Errorf("%w: %d", ErrUnknownCommand, cmd.ID)
    }
    return handler(ctx, cmd.Payload)
}
```

函数表属于 Service 实现，不是 Core Registry。GSR 不导出 `CommandRegistry`、`Register` 或动态协议注册入口，也不要求所有 Service 使用同一种分发写法。

未知 Command 会按普通消息进入 Mailbox，再由 `Handle` 返回 `ErrUnknownCommand`：

- `Call` 在尚未 Reply 时收到该 Handler error。
- `Send` 和 Timer 已经完成异步接受，Handler error 通过日志与 Metrics 观测。
- `Send` 或 `After` 返回成功只表示 Runtime 接受了消息或定时意图，不表示目标业务支持该 Command。

泛型包装：

```go
type Handler[Req any, Resp any] func(
    ctx CommandContext,
    req Req,
) (Resp, error)
```

泛型 handler 包装可以由业务包或代码生成提供，不进入 Core，也不阻塞统一 `Service.Handle`。

## CommandID 管理

禁止散乱手写数字。

推荐维护：

```yaml
battle:
  KickShrew: 10001
  Reconnect: 10002
player:
  AddCoin: 20001
wallet:
  CommitSettlement: 30001
```

生成：

```go
const (
    CmdKickShrew        CommandID = 10001
    CmdReconnect        CommandID = 10002
    CmdAddCoin          CommandID = 20001
    CmdCommitSettlement CommandID = 30001
)
```

## Command 与 Send/Call

Command 不决定同步还是异步。

投递方式决定：

```text
Send -> Command + Session=0
Call -> Command + Session>0
```

因此同一个 `CmdKickShrew` 可以被 `Send` 或 `Call` 投递，具体是否允许由业务语义和 handler 约束。

## 与 Skynet 的关系

Skynet 在 `skynet.lua` 中定义了多种 Protocol Type，例如：

```text
PTYPE_TEXT
PTYPE_RESPONSE
PTYPE_MULTICAST
PTYPE_CLIENT
PTYPE_SYSTEM
PTYPE_HARBOR
PTYPE_SOCKET
PTYPE_ERROR
PTYPE_LUA
```

这些协议类型并不都属于业务开发接口。实际业务 Service 主要使用 `PTYPE_LUA`。`PTYPE_RESPONSE` 由 Runtime 自动完成 `call` 的响应，`PTYPE_SOCKET`、`PTYPE_HARBOR`、`PTYPE_SYSTEM` 等更多属于 Runtime 或系统服务内部。

Skynet 业务 Service 通常使用 `skynet.dispatch("lua", handler)` 注册一个 Lua 消息入口，再在 handler 内用 `CMD[cmd]` 分发业务命令。Skynet Runtime 不要求每个 Service 再提交一份 `CMD` 清单。

GSR 学习 Skynet 的消息驱动和 Service Runtime 思想，但不照搬 PTYPE：

原因：

1. Go 有类型、接口、泛型和编译期检查，不需要通过 `PTYPE_*` 决定业务分发入口。
2. `Protocol` 属于传输层和编码层，`Command` 才是 Service 能力入口。
3. 本地、Timer、Cluster、Call、Send 都可以统一成同一种 `Envelope`。
4. Go 的 `Service.Handle` 已经对应 `dispatch("lua", handler)`；Service 内部的 `switch` 或函数表对应 Skynet 的 `CMD` 表，不需要 Runtime 再维护能力清单。

GSR 的内部消息模型固定为：

```text
Envelope
  ↓
Command Dispatcher
  ↓
Command Handler
  ↓
Service
```

`CommandID` 是唯一的业务分发键。Transport 可以根据 `CommandID` 和编解码配置选择 payload 编码方式，但这个选择不暴露为业务可见的 `ProtocolID`，也不形成第二套业务分发机制。

## 规则

1. Service 只能通过 Command 暴露能力。
2. 不允许跨 Service 调对象方法。
3. CommandID 必须稳定。
4. Command 名称要表达业务能力，不表达投递方式。
5. protobuf 只用于跨节点 payload 编码，不强制内部 handler 使用 protobuf。
6. 不设计业务可见的 `PTYPE_*` 或 `ProtocolID` 分发层。
7. 未知 Command 由 `Service.Handle` 返回 `ErrUnknownCommand`；Runtime 不做 per-Service Command 准入检查。
8. 业务消息只走 Command，不提供其它协议类型、全局动态注册表或旁路 handler。
9. Service 内部可以使用 `switch` 或私有函数表，优先选择最直观、最容易阅读的实现。
