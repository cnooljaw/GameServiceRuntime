# RFC-0170：Timer 设计

> 状态：草案  
> 范围：Core Runtime  
> 依据：`docs/learn/005-Skynet设计思想与优雅实现.md`

## 目的

本文定义 Timer 在 GSR 中的角色。

## 核心原则

Timer 只能生成 Command，不能直接修改业务状态。

正确流程：

```text
Timer expires
  ↓
Create Envelope
  ↓
Mailbox
  ↓
Service.Handle
```

## API 草案

```go
type TimerID uint64

type ServiceContext interface {
    After(d time.Duration, cmd CommandID, payload any) (TimerID, error)
}

type TimerManager interface {
    Cancel(id TimerID) error
}
```

## 禁止

禁止：

```go
time.AfterFunc(d, func() {
    battle.state = ...
})
```

使用：

```go
ctx.After(d, CmdExpireShrew, payload)
```

## 第一版实现

第一版可使用 Go timer。

目标是保证语义正确。

## 性能版实现

生产版使用 Timer Wheel。

原因：

```text
大量 Battle * 大量 timer -> 不能每个 timer 一个 goroutine
```

Timer Wheel 复杂度应接近：

```text
O(expired timers)
```

## 与 Timeline 的关系

Core Runtime 提供 Timer。

Game Layer 提供 Timeline。

Timeline 内部使用 Timer，但暴露的是游戏时间轴语义。

## 投递结果与指标

Timer 到期表示开始一次投递尝试，不表示 Command 已经被 Handler 处理。Runtime 必须观察 `Send` 结果，不能静默忽略失败：

| 指标 | 含义 |
|---|---|
| `timers_fired_total` | Timer 到期并开始投递的次数 |
| `timer_deliveries_total` | Command 成功进入目标 Mailbox 的次数 |
| `timer_delivery_errors_total` | Timer 投递失败总数 |
| `timer_delivery_mailbox_full_total` | 目标 Mailbox 满导致的失败数 |
| `timer_delivery_target_closed_total` | 目标已关闭或已经移出 Registry 的失败数 |
| `timer_delivery_runtime_closed_total` | Runtime 已进入关闭阶段导致的失败数 |
| `timer_delivery_other_errors_total` | 不能归入上述类别的失败数 |

目标或 Runtime 关闭属于生命周期竞争中的预期丢弃，只记录指标，不输出错误日志。Mailbox 满由指标表达背压。只有未分类错误输出结构化错误日志。

## 测试

必须覆盖：

- Timer 到期投递 Command。
- Cancel 后不投递。
- Timer 目标 Service 关闭时返回错误或丢弃。
- Timer 不直接执行业务回调。
- Timer 成功投递、Mailbox 满和生命周期竞争都有明确指标。
