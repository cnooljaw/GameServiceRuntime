# Mailbox

> 状态：已实现
>
> 规范：[RFC-0150](../../rfcs/RFC-0150-Core-Mailbox.md)

## 职责

每个 Service 拥有独立 Mailbox。Command 先入队，再由 Scheduler 串行交给 Handler：

```text
Send / Call / Timer
        ↓
     Mailbox
        ↓
    Scheduler
        ↓
 Service.Handle
```

Mailbox 不执行业务逻辑，也不创建每 Service goroutine。

## 第一版实现

- 使用有界 FIFO 切片队列。
- 普通 Command 达到容量后返回 `ErrMailboxFull`。
- Stop 使用内部控制项进入同一队列，保证和已接受 Command 的顺序关系。
- `DrainMailbox` 处理 Stop 前已接受的 Command；`DiscardMailbox` 在 Stop 时丢弃排队 Command。
- 深度通过 Metrics 和 `Runtime.Inspect` 读取。

关闭 Mailbox 会拒绝新消息并清理队列。Runtime 不暴露 Mailbox 指针，也不允许 Monitor 绕过投递规则读取或修改队列。

Ring Buffer 只在完整路径 Benchmark 证明当前结构成为瓶颈后考虑。
