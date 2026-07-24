# Command、Send、Call 与 Reply

“我能不能给每个 Service 生成一套漂亮的客户端方法？”

“可以。”老周说，“但先别把三个不同意图揉成一个 RPC。”

## Command 是唯一业务入口

```go
type Command struct {
    ID      CommandID
    Payload any
}
```

Command 表示一次业务输入。它不等于 HTTP request，也不等于数据库事务。

Runtime 内部用 Envelope 携带路由信息：

```go
type Envelope struct {
    Source   ServiceRef
    Target   ServiceRef
    Session  SessionID
    Command  CommandID
    Payload  any
    CallPath []ServiceRef
}
```

业务 Service 只收到 `Command` 和受限的 `CommandContext`。

## Send：我只关心是否被接受

```go
err := runtime.Send(battleRef, StartBattleCommand, struct{}{})
```

成功表示：

- Runtime 仍接受工作；
- 目标 Ref 当前有效；
- Command 已注册；
- Mailbox 接受了 Envelope。

成功不表示 Handler 已完成，更不表示业务成功。

适合 Send 的场景：

- 启动通知；
- 状态变化事件；
- 异步结果回投；
- 不需要当前返回值的业务输入。

## Call：我需要这一条 Command 的结果

```go
value, err := runtime.Call(
    ctx,
    battleRef,
    GetBattleSnapshotCommand,
    struct{}{},
)
```

Call 创建 Session 和 Pending Call，投递 Envelope，然后等待 `Reply`、错误、关闭或超时。

Handler：

```go
func (s *service) Handle(ctx gsr.CommandContext, cmd gsr.Command) error {
    snapshot := s.snapshot()
    return ctx.Reply(snapshot)
}
```

同一 Command 可以被 Send 或 Call 投递。Business Layer 对这种差异做了统一：BattleContext、PlayerContext 以及固定业务 Handler 会把 Send 场景下的 `ErrReplyUnavailable` 视为成功无副作用。

Core 的原始 `CommandContext.Reply` 则保持严格：

- Send 没有 Session，返回 `ErrReplyUnavailable`；
- 第二次 Reply 返回 `ErrReplyTwice`；
- 超时后的迟到 Reply 返回 `ErrReplyExpired`。

## Error 与 Reply

如果 Handler 返回 error，且当前 Call 尚未 Reply，Runtime 把 error 返回调用方：

```go
if request.Amount <= 0 {
    return ErrInvalidAmount
}
```

如果业务拒绝需要稳定数据，例如原因、余额、请求状态，优先 Reply 明确结果：

```go
return ctx.Reply(KickResult{
    Hit:   false,
    Score: s.score,
    Reason: "expired",
})
```

## Service 内 Call

Service 可以通过 `ServiceContext.Call` 调用其他 Service。Runtime 会暂时归还 Scheduler 许可，等待结束后恢复。

```go
value, err := s.service.Call(ctx, target, QueryCommand, query)
```

但这不是分布式锁。尤其不能：

```text
修改一半本地状态
  -> Call 远端
  -> 假设两边形成原子事务
```

跨 Service 工作流应保存本地阶段，再用结果 Command 推进。

## 调用环

```text
A Call B
B Call A
```

如果允许，单 worker 下会直接死锁，多 worker 下也会形成逻辑环。Runtime 在 Envelope 中传播 CallPath，发现目标已在路径中时返回 `ErrCallCycle`。

## 超时案例

```go
ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
defer cancel()

_, err := runtime.Call(ctx, wallet, CommitSettlementCommand, request)
```

返回 `ErrTimeout` 后，Pending Call 被移除。远端可能仍在执行，迟到 Reply 会被计入 `late_reply_total`。

如果请求具有外部副作用，必须带 `RequestID`：

```go
SettlementRequest{RequestID: "settle-42"}
```

重试时查询这一个 RequestID，而不是生成新请求。

## 对照源码

- Command/Envelope：`runtime/types.go`
- Send：`runtime/runtime.go`
- Call/Pending Call：`runtime/call.go`
- 语义测试：`runtime/call_conformance_test.go`
- 业务统一：`game/battle.go`、`game/player.go`

## 本章小结

```text
Send = 接受边界
Call = Send + 等待当前 Reply
Reply = 当前 Command 的一次回应
RequestID = 跨超时和重试的业务幂等键
```

Session 解决传输关联，RequestID 解决业务事实。不要混用。
