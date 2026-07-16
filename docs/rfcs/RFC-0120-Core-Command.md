# RFC-0120：Command 模型

> 状态：草案  
> 范围：Core Runtime  
> 依据：`docs/learn/006-Go-Service-Runtime概要设计与约定.md`

## 目的

本文定义 Command、CommandID、Command 声明和类型安全策略。

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

## Command 声明与私有命令集

每个 Service 声明自己支持的 Command。

第一版保留 `Service.Handle` 作为唯一业务处理入口，Service 只通过 `CommandDeclarer` 声明稳定 CommandID：

```go
type CommandDeclarer interface {
    Commands() []CommandID
}
```

创建 Service 时，Runtime 复制 `Commands()` 的结果并构建私有、只读的命令集。GSR 不导出 `CommandRegistry`、`Register` 或其它协议注册入口。重复 CommandID 返回 `ErrCommandAlreadyRegistered`，投递未声明 Command 返回 `ErrCommandNotRegistered`，不会进入 Mailbox。

泛型包装：

```go
type Handler[Req any, Resp any] func(
    ctx CommandContext,
    req Req,
) (Resp, error)
```

泛型 handler 注册属于后续类型安全增强，不阻塞第一版统一 `Service.Handle`。

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

GSR 学习 Skynet 的消息驱动和 Service Runtime 思想，但不照搬 PTYPE。

原因：

1. Go 有类型、接口、泛型和编译期检查，不需要通过 `PTYPE_*` 决定业务分发入口。
2. `Protocol` 属于传输层和编码层，`Command` 才是 Service 能力入口。
3. 本地、Timer、Cluster、Call、Send 都可以统一成同一种 `Envelope`。

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
7. 每个 Service 必须声明至少一个 CommandID，注册后不可在运行期修改。
8. 业务消息只走 Command，不提供其它协议类型、动态注册表或旁路 handler。
