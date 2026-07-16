# RFC-0150：Mailbox 设计

> 状态：草案  
> 范围：Core Runtime  
> 依据：`docs/learn/006-Go-Service-Runtime概要设计与约定.md`

## 目的

本文定义 Mailbox 的职责、第一版实现和后续性能方向。

## 定义

Mailbox 是每个 Service 的消息队列。

所有外部影响都必须先进入 Mailbox，再由 Scheduler 调度执行。

## 第一版实现

```go
type Mailbox struct {
    queue chan Envelope
}
```

第一版优先正确性。

## 性能版方向

后续可改为 ring buffer：

```go
type Mailbox struct {
    buf  []Envelope
    head uint64
    tail uint64
}
```

## 入队规则

1. `Send` 入队失败返回错误。
2. `Call` 入队失败立即失败，不无限等待。
3. Timer 到期也走同一套入队逻辑。
4. Cluster 收到远程 Envelope 后也走同一套入队逻辑。

## 满队列策略

第一版：

```text
Mailbox full -> ErrMailboxFull
```

后续可按 Command 配置：

- 丢弃低优先级事件。
- 拒绝新 Call。
- 记录慢 Service。
- 触发 Monitor 告警。

## 与 Scheduler 的关系

Mailbox 入队后，如果 Service 当前不在 ready 状态，则推入 ReadyQueue。

必须避免同一个 Service 重复入 ReadyQueue。

## 规则

1. Service 状态只能在消费 Mailbox 时修改。
2. Mailbox 不能暴露给业务。
3. Mailbox 不负责业务重试。
4. Mailbox 长度必须进入指标。
5. Mailbox 等待时间必须可观测。

## 反模式

禁止：

```go
go func() {
    service.state = newState
}()
```

使用：

```go
runtime.Send(serviceRef, CmdUpdateState, payload)
```

