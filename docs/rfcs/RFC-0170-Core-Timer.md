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

## 测试

必须覆盖：

- Timer 到期投递 Command。
- Cancel 后不投递。
- Timer 目标 Service 关闭时返回错误或丢弃。
- Timer 不直接执行业务回调。

