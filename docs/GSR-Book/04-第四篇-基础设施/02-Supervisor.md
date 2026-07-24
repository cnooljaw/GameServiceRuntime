# Supervisor

> 状态：已实现

“Service panic 了，重新 new 一个就行。”

“新对象从哪里恢复？新地址由谁发布？一分钟崩一百次怎么办？”

Supervisor 处理 Service Handler panic 后的恢复决策。它不修改 Core `Service` 接口，不复活失败对象，也不会在 panic 后读取旧实例状态。

## 为什么需要独立 Supervisor

Core 的职责是隔离失败：Call 返回 `ErrServiceFailed`，失败实例执行 Close，Timer、PendingCall 和 Registry 最终收敛。Core 不知道 Player 应从 Snapshot 恢复、Battle 应销毁，还是 Wallet 应停止自动操作。

这些判断依赖稳定业务身份和风险策略，因此位于 Runtime Tooling：

```text
Core Runtime
  -> 隔离具体 ServiceRef

Supervisor Tooling
  -> 根据 ServiceKey 和 Generation 决定是否创建新实例

Business composition root
  -> 提供 Snapshot factory 和长期名字 publisher
```

## 四个明确的 owner

Supervisor 由四个角色组成：

- Decorator 包装业务 Service。它只在 `Handle` panic 时发送 `FailureNotice`，随后用原值重新 panic。
- Supervisor Service 在 Mailbox 中串行维护注册、代际、策略、尝试预算和状态。
- Runner 是组合根持有的非 Service owner。固定 worker 执行退避、Store I/O、创建和结果重试。
- RuntimeLauncher 使用窄 `RuntimeControl`、`ServiceFactory` 和可选 `BindingPublisher`，不把完整 Runtime 交给业务 Service。

普通 Service 不创建 goroutine，不访问 Snapshot Store，也不调用 `CreateService`。

## 不可变失败事实

Decorator 发送的通知只包含：

```go
type FailureNotice struct {
    Key        ServiceKey
    FailedRef  gsr.ServiceRef
    Generation uint64
    OccurredAt time.Time
    Kind       FailureKind
}
```

Supervisor 同时校验 Command Source、Key、Ref 和 Generation。panic value、堆栈、业务状态和 secret 不进入 Command；诊断详情留在结构化日志中。

通知通过 `ServiceContext.Send` 直接进入 Supervisor Mailbox，因此 Source 是失败实例自身。同代际重复通知只决定一次，旧 Ref 或旧 Generation 不会影响新实例。

## 策略和状态

第一版提供三种策略：

- `RestartNever`：进入 `ServiceRestartStopped`，适合 Wallet 等要求人工或业务补偿的状态。
- `DestroyOnFailure`：进入 `ServiceDestroyed`，适合临时 Battle。
- `RestartOnFailure`：在预算内准备并发布新实例，适合可从已提交状态恢复的 Player。

`MaxAttempts` 限制一个失败代际的 Prepare/Commit 次数，防止 Snapshot 缺失或创建持续失败形成无限循环。`MaxRestarts` 限制 Window 内成功发布的新代际数量，防止实例快速反复崩溃。每次尝试使用有上限的指数退避。

状态可通过 typed Client 查询：

```go
client, err := supervisor.NewClient(runtime, supervisorRef)
err = client.Register(ctx, supervisor.Registration{
    Key:        key,
    Ref:        initialRef,
    Generation: 1,
    Policy:     policy,
})
record, err := client.Get(ctx, key)
```

组合根必须在开放业务流量或发布长期名字前完成注册。

## 两阶段恢复

恢复不能在 Supervisor Handler 中加载 Store 或等待退避。Supervisor 只把有界任务提交给 Runner：

```text
FailureNotice
  -> Supervisor 校验并提交 RecoveryTask
  -> Runner 等待 backoff
  -> Launcher.Prepare 加载 Snapshot、构造并 CreateService
  -> Supervisor 登记 prepared Ref 和下一 Generation
  -> Launcher.Commit 发布长期名字
  -> Supervisor 提交新 Generation
```

Factory 返回的 `ServiceSpec.Name` 必须为空。需要长期名字时，由 `BindingPublisher.Publish` 在 Commit 阶段更新；没有长期名字时可以省略 Publisher，通过 `Record.Registration.Ref` 获取新地址。

Publish 返回 error 可能表示结果不确定。Runner 总是调用 `Withdraw(Key, Ref)` 条件撤销，再调用 `Runtime.Stop`。撤销只匹配 prepared Ref，不能误删更晚的新绑定。Abort 失败会停止后续自动恢复，避免制造多个可达孤立实例。

## Publish 与 committed 的并发窗口

名字发布成功到 committed 结果进入 Supervisor Mailbox 之间，新实例可能立刻收到业务 Command。若它在这个窗口再次 panic，Supervisor 会先把 prepared Ref 提升为已经对外运行过的失败 Generation，再决定下一次恢复。

迟到的 committed 结果会被拒绝，Runner 条件撤销旧 prepared Ref。下一次任务使用更高 Generation；已经处理过业务 Command 的 Generation 永不复用。

started、prepared 和 committed 确认对相同 Task/Ref 幂等，因此 Mailbox 满或响应丢失后的重试不会误杀已经提交的实例。

## Snapshot 组合

Supervisor 不要求所有 Service 都使用 Snapshot。业务 Factory 决定恢复来源：

```go
factory := supervisor.ServiceFactoryFunc(func(ctx context.Context, key supervisor.ServiceKey, generation uint64) (gsr.ServiceSpec, error) {
    saved, err := snapshots.Load(ctx, snapshot.Key{Namespace: key.Namespace, ID: key.ID})
    if errors.Is(err, snapshot.ErrSnapshotNotFound) {
        return gsr.ServiceSpec{}, errors.Join(supervisor.ErrSnapshotNotFound, err)
    }
    service, err := newPlayerService(saved.State)
    return gsr.ServiceSpec{Service: service}, err
})
```

Store I/O 在 Runner worker 中执行。失败对象不会被调用 Snapshot；恢复只读取 panic 前已经提交的状态。

## 失败通知的可靠性边界

Decorator 处于 panic defer，不能阻塞、访问磁盘或创建重试 goroutine。它只尝试一次 Send：

- 成功时由 Supervisor 决策。
- Mailbox 满、Supervisor 已关闭或 Runtime 正在关闭时，记录 `supervisor_failure_notice_delivery_errors_total` 和结构化日志，然后继续让 Core 隔离实例。

投递失败时自动恢复不保证发生。这是 fail-closed 的显式边界；未来若要求跨进程可靠恢复，需要带 fencing 的持久故障日志，而不是 panic 路径中的无界队列。

## 生命周期与关闭

`Init`、`Stop`、`Close` 的错误不进入 Handler panic 通知。创建期间的 Init 失败由 Launcher 得到；显式 Stop 和 Runtime.Close 的错误由发起它们的生命周期 owner 处理。

Supervisor Service 的 Close 不关闭 Runner。组合根显式关闭 Runner；`Runner.Close(ctx)` 会取消退避和可取消 I/O，并等待固定 worker 中的 launcher 调用真实返回。调用方 context 超时只结束等待，后续 Close 仍可继续等待真实结束。

## 可观测性

Supervisor 指标进入 Core Metrics，并通过 `Runtime.Inspect().Metrics` 读取：

- 失败通知接收、投递失败、重复和迟到。
- 恢复安排、成功、失败和抑制。

`Client.Get` 返回的是 Supervisor 的策略状态，不替代 Runtime Inspection。失败通知投递失败后，旧 Record 可能仍显示 Running；运维必须根据投递失败指标告警。

## 当前限制

- Supervisor、Runner 与被管理 Service 必须同节点。
- 不提供跨进程恢复、持久故障队列、选主、多副本或完整 Supervisor Tree。
- 不提供内置数据库 Store，也不替 Wallet 自动补偿。
- `BindingPublisher` 由组合根按本地目录或 Discovery 需求实现；Supervisor 不强制依赖 Discovery。

可运行示例：

```bash
go run ./examples/supervisor-runtime
```

示例会输出旧/new `ServiceRef`、`Generation 1 -> 2`，以及从已提交 Snapshot 恢复的 revision 和 value。
