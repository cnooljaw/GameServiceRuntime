# Drain、Stop 与恢复：热更新是一条状态机

“把 Directory 指向新实例，再 Stop 旧实例，热更新完成。”

“缓存旧 Ref 的调用方呢？正在访问旧实例的 Service 呢？Publish 超时但其实成功了呢？”

## 完整流程

```text
创建并验证 new
  -> CAS 发布更高 ServiceSet
  -> Guard old
  -> 等待 strong visitor lease 清零
  -> ReadyToStop
  -> NodeAgent receipt
  -> NodeStopRunner 调用 Runtime.Stop
  -> Stop receipt
```

每一步都有可能超时或结果未知，因此流程由 RequestID 驱动。

## Visitor lease

`tooling/drain` 的 `VisitorRegistryService` 保存显式访问关系：

```go
lease, err := client.Acquire(ctx, target, visitor, false)
err = client.Renew(ctx, lease)
err = client.Release(ctx, lease)
```

强 lease 阻止 Stop；弱 lease 只用于观测。

访问关系不能散落在每个调用方的共享 map，也不能压入 Core Service 接口。

## 入口 Guard

旧实例用 decorator 包装：

```go
guarded, err := drain.Decorate(service, drain.GuardConfig{})
```

`BeginDrainCommand` 进入旧实例自己的 Mailbox。Guard 生效后拒绝新的外部业务 Command，但允许已接受工作和内部收敛 Command 按 RFC 规则处理。

Guard 是不可逆的。原旧 Ref 不能重新发布接流。

## DrainCoordinator

```go
operation, err := client.Start(ctx, control.StartDrainRequest{
    RequestID: "drain-42",
    Principal: "operator-alice",
    Group:     "battle",
    Expected:  current.Version,
    NextRefs:  []gsr.ServiceRef{newRef},
})
```

主要阶段：

```text
preparing
publish_unknown
guarding
waiting_visitors
ready_to_stop
conflict
superseded
```

如果 Directory Call 超时，进入 `publish_unknown`，不能盲目重新发布。操作者调用 `Resolve`，Coordinator 查询当前 Directory 事实再裁决。

## Stop 由谁执行

Coordinator 到 `ReadyToStop` 只表示已授权且前置条件成立。

`NodeAgentService` 保存本地 `NodeStopReceipt`，`NodeStopRunner` 才持有：

```go
type NodeStopRuntime interface {
    Stop(context.Context, gsr.ServiceRef) error
}
```

Runner 关闭时等待真实 Stop 返回。Coordinator 根据 receipt 收敛 `StopOperation`。

## 为什么不能“回滚旧实例”

旧实例已经 Guard，甚至可能已经 Stop。它的内存状态和外部副作用不能安全回到发布前。

恢复流程选择：

```text
BlueprintID
  -> RecoveryRunner 创建替代实例
  -> NodeAgent 保存 RecoveryReceipt
  -> 操作者 Confirm
  -> Coordinator CAS 发布更高版本
```

恢复永远创建新 Ref，不复活旧 Ref。

## 人工确认

创建替代实例成功不代表应该立刻对外。操作者可以先检查：

- Snapshot/配置版本；
- 新 Ref 的健康；
- 目标组版本；
- 已创建 receipt；
- 审计记录。

然后：

```go
operation, err := client.ConfirmRecovery(
    ctx,
    requestID,
    principal,
)
```

## 可运行例子

基础 Visitor lease：

```bash
go run ./examples/drain-runtime
```

ServiceSet 切换：

```bash
go run ./examples/servicegroup-runtime
```

更完整的 Stop/Recovery 契约在 `tooling/control/*_test.go`。

## 当前边界

- 不提供自动滚动发布 Controller；
- 不自动选择放置节点；
- 不保证跨进程持久化操作记录；
- 不把已 Guard 的旧 Ref 回滚；
- 不把 Stop 失败解释为目标一定仍运行。

## 本章小结

热更新不是一次指针替换，而是一条有身份、有版本、有未知结果、有人工确认的状态机。复杂度不会消失，只能被放到正确的 owner。
