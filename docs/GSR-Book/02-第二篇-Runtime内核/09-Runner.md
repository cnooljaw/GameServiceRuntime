# Core Runner：等待外部工作，但不放弃 Service 顺序

Service 的业务状态只能在 Mailbox Handler 中修改，但数据库、Redis、HTTP 和文件系统调用又可能阻塞。让每个业务模块各自维护 goroutine、队列和关闭状态，会把同一套并发问题复制到整个项目。

Core Runner 把这部分收敛到 Runtime：固定 worker、有限队列、取消、关闭、panic 隔离和 Inspection 由框架统一拥有；业务仍决定任务内容、pending 状态和结果是否过期。

## 创建

```go
runner, err := gsr.NewRunner(runtime, gsr.RunnerConfig{
    Name:      "profile-store",
    Workers:   4,
    QueueSize: 128,
}, func(ctx context.Context, request SaveProfile) (SaveResult, error) {
    return store.Save(ctx, request)
})
```

名字在一个 Runtime 生命周期内唯一，Runner 关闭后也不复用。processor 在 Service Mailbox 外运行，不得读取或修改 Service 状态，也不得保存 `CommandContext`、`ServiceContext` 或 Service 指针。

## Submit：让 Handler 返回

```go
err := runner.Submit(ctx, battleRef, applySaveResultCommand, request)
```

`Submit` 是非阻塞入队。返回 nil 只表示有限队列已经接收任务，不表示外部工作完成。processor 完成后，Runtime 把下面的 payload 发送到精确的本地 `ServiceRef`：

```go
gsr.RunnerResult[SaveResult]{
    Value: result,
    Err:   err,
}
```

目标 Service 在新的 Handler 中处理结果。多个 worker 可以乱序完成，因此业务只在确实需要时检查当前阶段、OperationID、TurnRevision 或其他窄身份；Core 不生成通用 Revision，也不提供 Exactly Once。

`Submit` 适合等待期间仍要处理 Mailbox 的业务，例如牌局 AI、回放写入、自定义牌堆读取和诊断导出。

## Await：暂停本次 Handler

```go
func (s *ProfileService) Handle(commandContext gsr.CommandContext, command gsr.Command) error {
    result, err := s.runner.Await(ctx, commandContext, request)
    if err != nil {
        return err
    }
    s.apply(result)
    return nil
}
```

`Await` 必须接收当前这一次 Handler 的 `CommandContext`。等待期间：

1. Runtime 归还当前 Handler 占用的 Scheduler 许可，其他 Service 可以继续运行。
2. 当前 Service 仍保持 busy，它的下一条 Mailbox Command 不会重入。
3. 外部工作完成后，Runtime 重新取得许可，代码从原 `Await` 调用点继续。

过期上下文、其他 Runtime 的上下文和同一次 Handler 的重复 Await 都会返回 `ErrRunnerAwaitNotAllowed`。Service 不能把仍有效的 `CommandContext` 发布给其他 goroutine。

这与 Skynet 的共同点是等待期间归还执行资源并从原调用点恢复；不同点是 GSR 不让同一 Service 的其他 Handler 在等待期间交错执行，因此状态推理仍保持单 Handler 串行。

## 怎么选择

| 需求 | 选择 | 业务责任 |
|---|---|---|
| 必须保留当前状态快照，结果回来前同一 Service 不能前进 | `Await` | 传入当前 `CommandContext`，处理取消和外部错误 |
| 等待期间仍要响应玩家、Timer 或控制命令 | `Submit` | 先记录 pending，结果 Command 中检查必要的业务身份 |
| 多阶段 Call、重试、补偿或投递失败清理 | 专用工作流执行器 | 保留显式状态机，不把补偿塞进通用 Runner |

## 关闭与观测

`Runner.Close(ctx)` 停止接收任务、取消 processor，并等待固定 worker 真实返回。调用方超时只结束等待；忽略 context 的 processor 仍保持 `closing`，不能被伪装成已经退出。

`Runtime.Close` 会先关闭自己拥有的 Runner，再停止 Service。`Runtime.Inspect().Runners` 和 Monitor 报告提供状态、worker 数、队列深度、active、完成、失败、拒绝和结果投递失败计数。

Runner 不提供任务持久化、自动重试、强制终止 Go 函数或跨节点任务队列。它解决的是框架共同拥有的执行机制，不接管业务事实。

正式契约见 [RFC-0193：Core Runner](../../rfcs/RFC-0193-Core-Runner.md)。
