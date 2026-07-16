# Timer

> 状态：已实现
>
> 规范：[RFC-0170](../../rfcs/RFC-0170-Core-Timer.md)

## 统一投递

Timer 不执行业务回调，只在到期时生成 Command：

```go
id, err := runtime.After(target, delay, commandID, payload)
err = runtime.Cancel(id)
```

```text
Timer 到期 -> Envelope -> Mailbox -> Scheduler -> Service.Handle
```

因此定时逻辑和普通消息遵守相同的命令声明、Mailbox 容量、Service 串行和生命周期规则。

## 生命周期

- `Cancel` 幂等地移除 Timer。
- Service 停止时取消指向该 Service 的 Timer。
- Runtime 关闭时取消全部 Timer。
- Timer 从管理表移除后才尝试投递，避免重复触发。
- 目标关闭、Mailbox 满和 Runtime 关闭等投递失败按原因记录指标。

第一版使用 Go Timer。Timer Wheel 只有在批量 Timer 基准和 profile 显示必要时才引入。
