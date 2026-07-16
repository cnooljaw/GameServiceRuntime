# Lifecycle

> 状态：已实现
>
> 规范：[RFC-0180](../../rfcs/RFC-0180-Core-Lifecycle.md)

## 状态

```text
Created -> Starting -> Running -> Stopping -> Closed
                                      └──────> Failed
```

Init 成功后 Service 才进入 Running。Handler、Init、Stop 或 Close panic 会转换为稳定错误并隔离实例。

## Stop

`Runtime.Stop` 先关闭消息接受边界，再把 Stop 控制项放入同一 Mailbox。这样 Stop 不会和正在处理的 Handler 并发。

```text
停止接受新 Command
        ↓
Drain 或 Discard 排队 Command
        ↓
Service.Stop(ctx)
        ↓
Service.Close()
        ↓
清理 Timer、PendingCall、Registry 和 Mailbox
```

并发 Stop 复用同一次结果，不创建第二个清理流程。

## Runtime Close

`Runtime.Close` 先进入 Closing，拒绝 CreateService、Send、Call 和 After，再关闭 Transport、停止 Service、取消 Timer、失败 PendingCall 并关闭 Scheduler。并发 Close 等待首个关闭流程的保存结果。

Go 不能强杀阻塞的 Init、Handle、Stop 或 Close。期限耗尽时 Runtime 释放自己拥有的结构并返回稳定超时错误，但保留任务 owner、类型、开始时间和超时标记，直到函数真实返回。该信息可通过 `Runtime.Inspect` 读取。
