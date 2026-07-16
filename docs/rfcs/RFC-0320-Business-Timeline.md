# RFC-0320：Timeline 设计

> 状态：草案  
> 范围：Business Layer  
> 依据：`docs/learn/007-Game-Service-Runtime详细设计与实现.md`

## 目的

本文定义游戏时间轴 Timeline。

## 定义

Timeline 是 Game Layer 的时间模型。

它不等于 Core Runtime 的 Timer。

```text
Timeline -> Timer -> Command -> Mailbox
```

## API 草案

```go
type Timeline interface {
    Schedule(at time.Time, cmd CommandID, payload any) (TimelineID, error)
    After(d time.Duration, cmd CommandID, payload any) (TimelineID, error)
    Cancel(id TimelineID) error
    Rev() TimelineRev
}
```

## 用途

Timeline 用于：

- 地鼠生成。
- 地鼠过期。
- 回合倒计时。
- 自动托管。
- 重连快照。

## TimelineRev

`TimelineRev` 用于重连和旧消息过滤。

客户端重连时，Battle Snapshot 应包含当前 `TimelineRev`。

## 规则

1. Timeline 只安排 Command。
2. Timeline 不直接修改 BattleState。
3. Timeline 必须支持取消。
4. Timeline 事件应进入 Battle 的 Mailbox。
5. Timeline 状态应可被 Snapshot 描述。
