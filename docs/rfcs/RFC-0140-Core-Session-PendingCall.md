# RFC-0140：Session 与 Pending Call

> 状态：草案  
> 范围：Core Runtime、Cluster  
> 依据：`docs/learn/006-Go-Service-Runtime概要设计与约定.md`

## 目的

本文定义 Call/Reply 的关联机制。

## 核心结论

Session 是 Call 和 Reply 的生命线。

没有 Session，就不要把 Call 做成同步等待。

## 类型草案

```go
type SessionID uint64

type PendingCall struct {
    Session  SessionID
    Source   ServiceRef
    Target   ServiceRef
    Command  CommandID
    Done     chan Result
    Deadline time.Time
}
```

## Call 创建 Session

```text
Call
  ↓
Allocate SessionID
  ↓
Save PendingCall
  ↓
Send Envelope(SessionID)
  ↓
Wait Done or ctx.Done
```

## Reply 使用 Session

```text
Reply
  ↓
Validate Session
  ↓
Create response Envelope
  ↓
Route to Source
  ↓
Wake PendingCall
```

## 超时

超时后：

```text
Remove PendingCall
Return ErrTimeout
```

晚到 Reply：

```text
No PendingCall
  ↓
Drop
  ↓
metrics late_reply_total++
```

## Cluster 场景

跨节点时，Session 必须带回源节点。

远端不需要知道调用方 goroutine，只需要把 Reply 按 Source + Session 发回去。

## 规则

1. Session 必须唯一。
2. PendingCall 必须受 context 控制。
3. Reply 只能一次。
4. Service 停止时必须唤醒相关 PendingCall。
5. 节点断线时必须失败相关 PendingCall。

