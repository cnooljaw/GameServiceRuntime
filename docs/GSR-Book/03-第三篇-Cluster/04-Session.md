# Session

> 状态：已实现
>
> 规范：[RFC-0140](../../rfcs/RFC-0140-Core-Session-PendingCall.md)、[RFC-0190](../../rfcs/RFC-0190-Core-Cluster-Data-Plane.md)

## 作用

Session 只关联一次 Call 和 Reply，不表示玩家登录会话，也不承担业务幂等。

```text
Caller 创建 PendingCall
        ↓ SessionID
Envelope -> Local 或 Remote Service
        ↓ Reply
校验 caller + responder + Command + Session
        ↓
完成 PendingCall
```

## 分配与校验

- `SessionID=0` 表示 Send，不等待 Reply。
- 分配回绕时跳过 0，并且不能覆盖仍在等待的 Session。
- Reply 来源与原始调用不匹配时丢弃，不能完成 PendingCall。
- Call context 超时后移除 PendingCall；迟到 Reply 只增加指标。
- Service 关闭、Runtime 关闭或远端节点断开会用明确错误失败相关 PendingCall。

## 与业务会话的区别

登录连接使用 `SessionIdentity`、LoginTicket 或 RequestID，由 LoginService 和 SessionRegistry 管理。它们不能复用 Core `SessionID`。

业务重试和幂等应使用稳定 `RequestID`；Call 超时不代表远端一定没有执行。
