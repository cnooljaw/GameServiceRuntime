# Service：四个方法背后的运行契约

“接口只有四个方法，看起来不难。”小林说。

老周点头：“难的从来不是方法数量，而是谁在什么时候调用它们。”

## 接口

`runtime/service.go` 定义：

```go
type Service interface {
    Init(ServiceContext) error
    Handle(CommandContext, Command) error
    Stop(context.Context) error
    Close() error
}
```

Runtime 对同一实例保证：

```text
Init
  -> Handle
  -> Handle
  -> ...
  -> Stop
  -> Close
```

`Handle`、`Stop`、`Close` 不并发。Service 的业务状态因此可以只在这些串行入口内修改。

## Init 做什么

`Init` 接收 `ServiceContext`，适合保存 Runtime 提供的长期能力：

```go
type notifierService struct {
    service gsr.ServiceContext
    target  gsr.ServiceRef
}

func (s *notifierService) Init(ctx gsr.ServiceContext) error {
    s.service = ctx
    return nil
}
```

`Init` 不应：

- 创建不受追踪的 goroutine；
- 调用尚未进入 Running 状态的自己；
- 泄漏半初始化对象；
- 执行业务状态迁移来模拟第一条 Command。

如果 Service 需要进入 Running 后自动收到一条 Command，可以额外实现：

```go
type StartupCommandDeclarer interface {
    StartupCommand() (Command, bool)
}
```

返回的 Command 也必须出现在 `Commands()` 中。

## Commands 是入口白名单

Service 通常实现 `CommandDeclarer`：

```go
func (*counterService) Commands() []gsr.CommandID {
    return []gsr.CommandID{Increment, GetValue}
}
```

Runtime 在 `CreateService` 时复制并冻结这组 ID。未注册 Command 在进入 Mailbox 前就返回 `ErrCommandNotRegistered`。

这比在每个 Handler 的 `default` 分支才发现错误更早，也避免运行期修改“路由表”。

## Handle 只处理一条 Command

```go
func (s *counterService) Handle(ctx gsr.CommandContext, cmd gsr.Command) error {
    switch cmd.ID {
    case Increment:
        s.value += cmd.Payload.(int)
        return nil
    case GetValue:
        return ctx.Reply(s.value)
    default:
        return gsr.ErrCommandNotRegistered
    }
}
```

真实代码必须检查 payload 类型，示例省略检查只是为了突出结构。

Handler 返回 error 时，Runtime 记录错误；如果这是一条尚未 Reply 的 Call，错误会返回调用方。Handler panic 时，Runtime 返回 `ErrServiceFailed`，记录指标并关闭失败实例。

## Stop 与 Close 分工

`Stop` 是 Runtime 串行调度的清理阶段，可以接收取消信号：

```go
func (s *service) Stop(ctx context.Context) error {
    return nil
}
```

它不应该制造新的业务终态。Battle 的“结束”应由 `FinishBattleCommand` 完成，不应等 Runtime Stop 时才结算。

`Close` 释放 Service 持有的能力和资源：

```go
func (s *service) Close() error {
    s.service = nil
    return nil
}
```

外部连接 adapter 或 worker pool 如果属于组合根，应由组合根关闭，而不是塞进普通业务 Service。

## CreateService 的完整含义

```go
ref, err := runtime.CreateService(gsr.ServiceSpec{
    Name:    "counter",
    Service: &counterService{},
    Policy: gsr.ServicePolicy{
        StopTimeout:  time.Second,
        CloseTimeout: time.Second,
        Mailbox:      gsr.DrainMailbox,
    },
})
```

Runtime 会：

1. 验证 Service 与 Command 集；
2. 分配新的 `ServiceRef`；
3. 先注册实例，状态为 Starting；
4. 追踪并调用 `Init`；
5. 切换为 Running；
6. 如果存在 Startup Command，把它送入自己的 Mailbox。

任一步失败都会尝试 Close，并让旧 Ref 不再可用。

## 对照源码

- 接口与 Context：`runtime/service.go`
- 创建与启动：`runtime/runtime.go`
- goroutine 规则：`runtime/service_goroutine_policy_test.go`
- 生命周期一致性：`runtime/lifecycle_conformance_test.go`

## 本章小结

Service 不是普通接口加一个调度器。它是 Runtime 对顺序、入口、失败和清理做出的整体承诺。写 Service 时，先设计 Command 和状态机，再实现四个生命周期方法。
