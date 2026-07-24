# 为什么不是 Actor 框架

“一个对象一个 Mailbox，消息串行处理，这不就是 Actor 吗？”

小林的问题很合理。老周回答：“实现手段相似，不等于产品契约相同。你把它叫 Actor，读者会自动带来一整箱预期。”

那只箱子里可能装着：

- Actor 层级与监护树；
- 位置透明；
- 自动迁移；
- 进程式隔离；
- `spawn`、`ask`、`tell`；
- 失败后自动重启。

GSR 没有承诺这些东西。

## 名字会创造预期

GSR 的核心术语只有：

```text
Service
ServiceRef
Command
Send
Call
Reply
```

`Service` 是可寻址的状态 owner。`ServiceRef` 是地址。`Command` 是业务入口。`Send` 和 `Call` 是两种调用意图。

这里故意不使用：

```text
Actor
ActorRef
Spawn
Ask
Tell
PTYPE
```

不是因为这些词不好，而是因为它们会模糊 GSR 已经冻结的边界。

## 一个看似方便的错误版本

假设 Room 直接持有 Battle 指针：

```go
type Room struct {
    battle *Battle
}

func (r *Room) Kick(player string, target int) Result {
    return r.battle.Kick(player, target)
}
```

单元测试很舒服。上线后问题来了：

- Room 和 Battle 是否在同一个节点？
- Battle 正在 Stop 时，这个指针是否还能调用？
- 两个 goroutine 同时调用 `Kick`，谁负责加锁？
- 恢复出一个新 Battle 后，旧指针如何失效？
- Replay 时如何重放一次普通方法调用？

换成 GSR 的表达：

```go
result, err := runtime.Call(
    ctx,
    battleRef,
    KickCommand,
    KickRequest{Player: "alice", Target: 7},
)
```

调用方只知道 `ServiceRef` 和 Command。状态修改必须进入目标 Mailbox。

## “位置透明”也不能说得太轻松

本地与远程 Command 在业务语义上尽量一致，但成本并不透明：

```text
本地：对象引用 payload + 进程内队列
远程：编码 + TCP framing + 网络 + 解码 + 远程错误
```

因此 GSR 不鼓励开发者忘记边界。跨节点要配置 Transport 和 Codec；动态寻址要显式使用 Discovery 或 ServiceGroup。错误和超时也必须被业务流程处理。

## Supervisor 不是父 Actor

GSR 的 `tooling/supervisor` 是可选恢复工具。它检测已定义的失败，按预算创建替代实例，再通过发布接口更新地址。

它不意味着：

- 每个 Service 必须有父节点；
- Runtime 自动构造 Supervisor Tree；
- panic 后原实例原地复活；
- 自动恢复一定成功。

旧实例一旦结束，它的 `ServiceRef` 就不会变成新实例。恢复得到的是新的 Ref。

## 从 Skynet 学什么

GSR 学习的是 Skynet 基础层的清晰：

- 消息原语保持通用；
- agent、watchdog 等上层 Service 按职责直接使用原语；
- Runtime 不知道具体业务；
- 服务实例拥有自己的状态和生命周期。

GSR 没有选择把所有业务包装成另一套“看起来像本地对象”的框架。Battle 和 Player 只有在需要向插件式 Logic/Module 暴露受限能力时，才提供领域 Context。

## Go 让边界更硬

Lua coroutine 与 Go goroutine 的运行和取消模型不同。Go 不能从外部安全终止任意业务函数。

所以 GSR 增加了明确约束：

- Service 不直接创建 goroutine；
- Runtime 创建的任务必须追踪到真实返回；
- 超时只表示调用方不再等待，不表示目标函数已停止；
- Stop、Close 与 Handle 不并发；
- Timer 只能投递 Command。

这些不是为了“把 Go 写得保守”，而是为了让关闭、恢复和竞态检查有可验证的答案。

## 本章结论

如果只看 Mailbox，GSR 与 Actor 系统有亲缘关系；如果看公开承诺，GSR 选择了更窄的 Service Runtime。

判断一个设计是否属于 GSR，不问“像不像 Actor”，而问：

1. 谁拥有状态？
2. 状态是否只经 Command 修改？
3. 跨 Service 是否只持有 `ServiceRef`？
4. 生命周期和异步任务是否有 owner？

下一章我们正面回答：为什么选 Service。
