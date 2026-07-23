# ServiceGroup

> 状态：已实现

## 它解决什么

`ServiceRef` 指向一个具体 Service 实例。多个实例承担同一职责时，调用方还需要知道：

- 当前有哪些可用地址。
- 这份地址表属于哪个权威和版本。
- 地址表变化后如何更新本地缓存。
- 根据业务 key、轮询或广播选择哪些实例。

ServiceGroup 用一个完整 ServiceSet 表达这份事实：

```go
type ServiceSet struct {
    Name    GroupName
    Version ServiceSetVersion
    Refs    []gsr.ServiceRef
    Tags    map[string]string
}
```

它属于 `tooling/servicegroup`，不是 Core Runtime 实体。Core 仍然只投递单个目标 `ServiceRef` 的 Command。

## 为什么不放进 Discovery

Discovery 第一版保存两类事实：

```text
活动 Node lease
长期 ServiceName -> 单个 ServiceRef
```

ServiceGroup 保存的是一个职责对应的版本化地址集合，还需要 Watch 和路由策略。把它并入 Discovery 会让“位置事实”和“如何选目标”重新耦合。

因此 GSR 使用独立 `DirectoryService`：

```text
Discovery
  回答长期 DirectoryService 在哪里

DirectoryService
  保存 GroupName -> ServiceSet

RoutingPolicy
  只决定如何使用一个 ServiceSet
```

## 版本为什么有两部分

ServiceSet 版本不是单独的自增整数：

```go
type ServiceSetVersion struct {
    AuthorityEpoch uint64
    Revision       uint64
}
```

Directory 每次构造都会生成新的非零 AuthorityEpoch。同一权威内，每个 Group 的 Revision 从 1 开始递增。

这样可以区分：

```text
旧 Directory 的 revision 20
新 Directory 的 revision 1
```

客户端不能把新权威的 revision 1 当作旧状态。AuthorityEpoch 变化时直接替换完整快照；只有同一 epoch 内才比较 Revision。

## 发布使用 compare-and-set

发布者不指定新版本，只提交期望版本和完整内容：

```go
next, err := directory.Publish(ctx, group, current.Version, refs, tags)
```

Directory 只有在期望版本与当前版本完全相等时才分配下一个 Revision。新 Group 使用零值期望版本，第一次成功发布得到 Revision 1。

Refs 会按 NodeID、ServiceID 排序并去重。空 Refs 是有效的已发布状态，表示该组暂时没有路由目标；它与 `ErrGroupNotFound` 不同。

## Watch 仍然是 Command

Watch 不返回 channel，也不启动后台 goroutine。订阅者必须是一个 Service：

```text
Subscriber Service
  -> Watch(GroupName, Self)
  <- WatchLease + 当前快照
  <- ServiceSetChanged Command
```

Watch lease 包含 Subscriber、AuthorityEpoch、Generation 和 ExpiresAt。续订、取消、重订阅和过期清理都进入 Directory Mailbox；旧 generation 的迟到操作不能删除新 lease。

`ServiceSetChanged` 携带完整 ServiceSet，不是 diff。通知是 best-effort 状态提示，发送失败不会回滚已经成功的 Publish。订阅方可以随时用 Get 重新读取完整事实。

## Router 为什么接收显式 ServiceSet

Router API 不接收 GroupName 后再偷偷查询 Directory：

```go
err := router.Send(set, servicegroup.Hash{}, key, command, payload)
reply, err := router.Call(ctx, set, &servicegroup.RoundRobin{}, "", command, payload)
```

如果 `Send(group, ...)` 内部先做 Directory Call，一个看似异步的动作会暗含同步远程等待，也无法由 Service 决定缓存切换时点。

显式 ServiceSet 让订阅 Service 在自己的 Mailbox 中替换缓存，再把当前快照交给 Router。Directory 负责事实，Service 负责缓存时点，Router 只负责选目标和投递。

## 第一批策略

### Hash

Hash 使用 FNV-1a 64 处理 RoutingKey 的原始 UTF-8 字节，再对稳定排序后的 Refs 取模。第一版不是一致性哈希，成员变化可能重新映射大量 key。

### RoundRobin

每个 `RoundRobin` 实例拥有自己的并发安全计数器。需要独立轮询序列时使用不同实例。

### Broadcast

Broadcast 按 ServiceSet 顺序逐个 Send，不创建 goroutine。某个目标失败不会阻止后续目标；部分失败以 `BroadcastError` 返回目标和底层错误，已经成功的投递不会回滚。

Call 只能选择一个目标。Broadcast Call 返回 `ErrMultipleTargets`。

`Direct` 不是 ServiceGroup policy。已经持有单个 `ServiceRef` 时直接使用 Runtime Send/Call。

## 当前边界

- Directory 是单一内存权威，没有复制、选主或持久化。
- Service 不会自动注册进组。
- Directory 不做健康检查、负载采样或自动扩缩容。
- Router 不维护后台缓存，也不重试 Send/Call。
- Phase 10A 已提供独立 `VisitorRegistryService` 保存强弱访问者 lease；Phase 10B 的 `Drain Guard` 可在旧实例自己的 Mailbox 内拒绝显式的新外部 Command；Phase 10C1 的 `DrainCoordinatorService` 通过 Gateway+Principal+RequestID 串行发布完整新 ServiceSet、只 Guard 被移除的旧 Ref、等待强 Visitor 清零，并记录 `ReadyToStop`。Phase 10C2A 以独立 StopOperation 将同一 Principal 的授权延续到精确 NodeAgent receipt：Coordinator 与组合根有界 Runner 都会强确认 Published ServiceSet，只有 Runner 在 Handler 外调用 `Runtime.Stop`，Gateway 通过显式 Resolve 读取结果。Phase 10C2B 复用终态 StopOperation 的 RequestID，组合根 Blueprint Runner 才能创建新 Ref；Coordinator 只在操作者 Confirm 后以 CAS 保留当前成员并追加该 Ref。它不替代 Directory，不会 Resume Guard 或重新发布旧 Ref，也不引入自动恢复、Desired State、Controller 或 Reconcile。

完整契约见 `RFC-0260`。可运行示例：

```bash
go run ./examples/servicegroup-runtime
```
