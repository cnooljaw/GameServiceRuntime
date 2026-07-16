# Cluster

> 状态：已实现
>
> 规范：[RFC-0190](../../rfcs/RFC-0190-Core-Cluster-Data-Plane.md)

## 数据面

Cluster Data Plane 只解决跨节点 Envelope 投递。业务继续使用同一组 API：

```go
runtime.Send(target, command, payload)
runtime.Call(ctx, target, command, payload)
```

Runtime 根据 `ServiceRef.Node` 选择本地 Mailbox 或 ClusterTransport。Service 不需要区分本地和远程目标。

```text
Envelope
   ↓
Local Router ──────────────> Mailbox
   └─> Codec -> Transport -> Remote Router -> Mailbox
```

## 语义

- 远程 Send 是异步 push，不增加投递 ACK。
- 远程 Call 使用 Session 和 PendingCall 等待 Reply。
- CallPath 随请求跨节点传递，用于拒绝同步调用环。
- Reply 校验原始 caller、responder、Command 和 Session。
- 节点断开会失败指向该节点的 PendingCall。
- 稳定 Runtime 错误跨节点后仍支持 `errors.Is`；未知业务错误返回 `RemoteError`。

控制面、Discovery、ServiceGroup 和节点运维不属于数据面，不能进入 `ClusterTransport`。
