# RFC-0130：Send、Call 与 Reply

> 状态：草案  
> 范围：Core Runtime  
> 依据：`docs/learn/006-Go-Service-Runtime概要设计与约定.md`

## 目的

本文定义 GSR 的消息投递语义。

## 核心结论

Send 和 Call 共用同一条 Command Pipeline。

差异只在是否创建 Session。

## Envelope

```go
type Envelope struct {
    Source  ServiceRef
    Target  ServiceRef
    Session SessionID
    Command CommandID
    Payload any
    CallPath []ServiceRef
}
```

## Send

```go
err := runtime.Send(ref, CmdShrewSpawn, event)
```

语义：

- 异步投递。
- 不等待业务结果。
- `Session = 0`。
- 可能因为 Service 不存在、Mailbox 满、远端不可用而返回错误。

## Call

```go
result, err := runtime.Call(ctx, ref, CmdKickShrew, req)
```

流程：

```text
Allocate Session
  ↓
Save PendingCall
  ↓
Route Envelope
  ↓
Wait Reply or ctx timeout
  ↓
Delete PendingCall
```

## Reply

Reply 只允许在 `Call` 进入的 Command 中使用。

```go
type CommandContext interface {
    Reply(value any) error
}
```

规则：

1. `Session = 0` 时 Reply 返回错误。
2. 同一个 Session 最多 Reply 一次。
3. 超时后的 Reply 丢弃并记录指标。
4. Reply 按 `caller + responder + Session` 路由；caller 必须等于原请求的 Source，responder 必须等于原请求的 Target，任一地址不匹配都不得完成 PendingCall。
5. Send 场景 Reply 返回 `ErrReplyUnavailable`，重复 Reply 返回 `ErrReplyTwice`，超时后的 Reply 返回 `ErrReplyExpired`。
6. Handler 返回 error 且尚未 Reply 时，Runtime 用同一个 Session 结束 PendingCall。

## 与 Skynet PTYPE_RESPONSE 的关系

Skynet 的 `call` 会通过 `PTYPE_RESPONSE` 把结果送回调用方。业务代码通常不会主动发送 `PTYPE_RESPONSE`，它是 Runtime 的自动响应机制。

GSR 采用同样的隐藏原则，但不暴露 `PTYPE_RESPONSE` 名称。

```text
Call
  ↓
Envelope(Session > 0)
  ↓
Command Handler
  ↓
Reply
  ↓
PendingCall
```

业务 handler 只通过 `CommandContext.Reply` 或返回值表达响应。Runtime 负责逻辑响应路由：本地 Reply 可以直接完成 PendingCall；跨节点 Reply 由 Transport 的内部响应帧携带 responder、caller 和 Session 返回。响应机制不暴露给业务，也不是第二种 Command 或业务协议类型。

## 错误

```go
var (
    ErrTimeout         error
    ErrReplyTwice      error
    ErrServiceNotFound error
    ErrServiceClosed   error
    ErrMailboxFull     error
    ErrReplyUnavailable error
    ErrReplyExpired     error
    ErrCallCycle        error
    ErrCallNotAllowed   error
)
```

## 为什么不用 Request

`Request` 容易被理解成 HTTP/RPC。GSR 采用 `Call`，因为它更接近 Skynet `call` 的 Session 语义。

## 测试

必须覆盖：

- Send 不等待结果。
- Call 能收到 Reply。
- Call 超时。
- Reply 两次失败。
- Send 场景 Reply 失败。
- Target 不存在。
- Handler error 返回给 Call 方。
- Call 链出现同步环时立即失败。
- 错误 caller 或 responder 不能完成 PendingCall。
