# Session 与 Pending Call：传输关联不是业务幂等

小林给每个结算请求加了 `SessionID`，认为重复结算问题解决了。

老周问：“Call 超时后重新调用，新的 Session 还是原来的业务请求吗？”

答案是否定的。

## Session 的生命周期

每次 Call 创建新的 Session：

```text
Call
  -> pendingCalls.create
  -> SessionID
  -> Envelope
  -> Reply(SessionID)
  -> pendingCalls.complete
```

Pending Call 记录：

- caller；
- target；
- CommandID；
- result channel。

Reply 必须同时匹配这些字段，不能只猜 Session 数字。

## 本地与远程

本地 Reply 直接完成 Pending Call。

远程 Reply 经过 WireEnvelope：

```text
Response = true
Source = responder
Target = caller
Session = original session
Command = original command
```

节点断开时，与该节点相关的 Pending Call 都返回 `ErrRemoteUnavailable`。

## 超时与迟到 Reply

```go
ctx, cancel := context.WithTimeout(context.Background(), time.Second)
defer cancel()
value, err := runtime.Call(ctx, target, command, payload)
```

超时后 Pending Call 被移除。目标稍后 Reply 时：

```text
找不到匹配 pending
  -> ErrReplyExpired
  -> late_reply_total +1
```

它不会错误地完成另一个 Call。

## Session 不能用于幂等

同一业务请求的两次传输：

```text
第一次 Session = 101
第二次 Session = 102
```

所以业务 payload 必须有稳定 RequestID：

```go
type SettlementRequest struct {
    RequestID game.RequestID
    // ...
}
```

Wallet 按 RequestID 保存 pending/committed/rejected。即使调用方换了 Session，也能查询同一业务事实。

## Source 与权限

CommandContext 的 `Source()` 来自 Envelope。Tooling 和业务 Service 会校验来源：

- Discovery 只允许 lease owner 修改自己的记录；
- Directory 只允许可信 PublisherNode 发布；
- Wallet 只接受指定 RunnerNode 的 ledger result；
- Battle 把自身 Ref 冻结进 SettlementRequest。

Session 负责关联结果，Source 负责说明谁发来，二者职责不同。

## 调用路径

CallPath 用于拒绝同步调用环。它不作为业务 trace ID，也不持久化为 RequestID。

## 对照源码

- `runtime/call.go`
- `runtime/types.go`
- `runtime/source_test.go`
- `runtime/pending_test.go`

## 本章小结

```text
SessionID：一次 Call/Reply 的临时关联
RequestID：跨超时、重试和恢复的业务身份
Source：这条 Command 从谁而来
CallPath：同步调用经过了谁
```

四个概念分开，失败语义才不会混乱。
