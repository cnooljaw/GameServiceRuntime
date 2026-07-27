# 为什么选择 Service

老周在白板上画了三个圆：

```text
Player        Room        Battle
```

小林说：“它们都是结构体。”

“从 Go 语法看是。从服务器设计看，它们首先是三份不同的权威状态。”

## Service 是状态 owner

一份可变状态需要一个 owner。owner 负责：

- 接受哪些 Command；
- 以什么顺序修改状态；
- 创建时如何初始化；
- 停止时如何收敛；
- 向外暴露什么快照；
- 失败后是否以及如何创建替代实例。

在 GSR 中，满足独立寻址和独立生命周期需求的 owner 通常实现为 Service。

```go
type Service interface {
    Init(ServiceContext) error
    Handle(CommandContext, Command) error
    Stop(context.Context) error
    Close() error
}
```

这个接口很小，但每个方法的调用时机非常严格。

## 不是所有对象都应该成为 Service

小林想把 `Timeline` 做成独立 Service。

老周问了三个问题：

1. Timeline 是否需要被其他模块独立寻址？
2. Timeline 是否拥有与 Battle 分离的权威状态？
3. Timeline 是否需要独立恢复和生命周期？

答案都是否。Timeline 属于 Battle，它只负责把未来事件重新变成 Battle Command。

同理：

- `BattleLogic` 是 Battle 内部的规则插件，不是 Service；
- `PlayerModule` 是 Player 的组合单元，不是 Service；
- `Router` 使用显式 ServiceSet 路由，本身不持有业务权威状态；
- `Monitor` 转换 Inspection 副本，不必成为 Core Service。

## 判断公式

可以用这张小表判断：

| 问题 | 是 | 否 |
| --- | --- | --- |
| 是否拥有独立可变权威状态？ | 倾向 Service | 倾向普通对象 |
| 是否需要独立寻址？ | 倾向 Service | 留在 owner 内 |
| 是否有独立生命周期或恢复策略？ | 倾向 Service | 跟随 owner |
| 只是算法、适配器或投影？ | 通常不是 Service | 直接组合 |

“倾向”很重要。Service 不是荣誉称号。多一个 Service 就多一个地址、Mailbox、失败边界和跨状态协作问题。

## 最小 Service

下面的 Counter 只有两个 Command：

```go
const (
    Increment gsr.CommandID = 1
    GetValue  gsr.CommandID = 2
)

type counterService struct {
    value int
}

func (*counterService) Init(gsr.ServiceContext) error {
    return nil
}

func (s *counterService) Handle(ctx gsr.CommandContext, cmd gsr.Command) error {
    switch cmd.ID {
    case Increment:
        delta, ok := cmd.Payload.(int)
        if !ok {
            return errors.New("invalid increment")
        }
        s.value += delta
        return nil
    case GetValue:
        return ctx.Reply(s.value)
    default:
        return gsr.ErrUnknownCommand
    }
}

func (*counterService) Stop(context.Context) error { return nil }
func (*counterService) Close() error               { return nil }
```

`value` 没有 mutex。不是因为它永远不会并发，而是 Runtime 保证同一 Service 的 Handler 串行。

## ServiceRef 切断对象耦合

创建后，调用方得到的是地址：

```go
ref, err := runtime.CreateService(gsr.ServiceSpec{
    Name:    "counter",
    Service: &counterService{},
})
```

其他 Service 不能保存 `*counterService`，只能保存 `ref`：

```go
type scoreService struct {
    counter gsr.ServiceRef
}
```

这条规则带来几个直接收益：

- 本地与远程调用使用同一 Command 模型；
- 旧实例结束后，旧 Ref 明确失效；
- 测试可以观察消息边界；
- Record/Replay 可以记录 Command；
- 状态 owner 不需要暴露内部锁。

## ServiceContext 与 CommandContext

两种 Context 不要混为 Go 标准库的 `context.Context`。

`ServiceContext` 属于整个 Service 生命周期，可用于：

- `Self()`；
- `Send()`；
- `Call()`；
- `After()`；
- `Now()`；
- `Metrics()`。

`CommandContext` 只属于当前 Command，可用于：

- `Self()`；
- `Source()`；
- `Reply()`。

业务扩展 Context，如 `BattleContext`，也是当前 Handler 的能力对象，不能保存到 Handler 外。

## 本章结论

Service 的价值不是“把结构体放进框架”，而是把四件事绑在同一个边界上：

```text
状态 owner + 地址 + Mailbox + 生命周期
```

下一章把这些边界放进完整的三层架构。
