# RFC-0220：Supervisor 与故障恢复

> 状态：草案
> 目标阶段：Phase 7E
> 范围：Runtime Tooling
> 依赖：[RFC-0180](RFC-0180-Core-Lifecycle.md)、[RFC-0210](RFC-0210-Tooling-Snapshot.md)

## 目的

本文定义 Service 实例失败后的通知、决策和新实例恢复边界。Supervisor 处理故障事实，不接管 Core 生命周期，也不复活已经失败的对象。

## 已裁决结论

以下结论不再开放：

1. Core 捕获 panic 后会关闭并移除失败实例；Supervisor 只能创建新实例。
2. 旧 `ServiceRef` 不复用。长期身份通过 `ServiceName`、Discovery 名字或业务 Key 重新绑定。
3. panic 后的内存状态不可信，Supervisor 不在失败对象上临时调用 Snapshot。
4. 自动恢复只能使用最近一次已提交 Snapshot，或从确定的初始状态重建。
5. Battle 默认销毁；Wallet 默认停止自动重启并进入人工或业务补偿流程。
6. 重启必须有窗口、次数上限和退避，不能形成无限重启循环。
7. Supervisor 不吞掉错误；失败通知、重启尝试、抑制和最终放弃都必须可观测。

## 目标

Phase 7E 计划解决：

- Handler panic 的不可变失败通知。
- 稳定业务 Key、旧 `ServiceRef`、策略和重启代际之间的关系。
- `RestartNever`、`RestartOnFailure` 和 `DestroyOnFailure` 的最小策略。
- 受限次数、窗口和退避。
- 从最近一次已提交 Snapshot 构造新 Service。
- 新实例成功后再更新长期名字；恢复失败时保留明确失败状态。

`RestartAlways` 暂不进入第一版。正常 Stop 后重新创建属于部署编排或 Drain，不属于故障恢复。

## 非目标

- 修改或复活旧 Service 实例。
- 在 panic 后读取旧实例状态。
- Go 进程崩溃后的跨进程自动恢复。
- Cluster 选主、跨节点迁移或多副本一致性。
- Wallet 自动修复、丢弃账本错误或重复执行未确认结算。
- 把业务工厂、Snapshot Schema 或重启策略加入 Core `Service` 接口。
- 通过轮询 Metrics 猜测具体实例是否失败。

## 分层方向

候选流程：

```text
Supervised Service decorator
  -> emit immutable FailureNotice as Command
  -> re-panic
  -> Core finalizes failed instance

SupervisorService
  -> validate notice source and generation
  -> apply restart window and backoff
  -> request recovery through narrow launcher seam
  -> load committed Snapshot
  -> construct and CreateService(new instance)
  -> publish new long-lived binding
```

Core 不导入 Supervisor 类型。Decorator 是 Tooling 组合对象，它可以包装一个 Service 的 `Handle`，但不能暴露被包装对象，也不能改变正常 Command 的顺序和 Reply 语义。

## 身份与失败事实

失败通知至少需要：

```go
type ServiceKey struct {
    Namespace string
    ID        string
}

type FailureNotice struct {
    Key        ServiceKey
    FailedRef  gsr.ServiceRef
    Generation uint64
    OccurredAt time.Time
    Kind       FailureKind
}
```

`FailureNotice` 不携带 Service 指针、panic value、堆栈全文、明文 secret 或业务状态。诊断详情写入结构化日志；通知只使用稳定、可校验的分类。

Supervisor 必须校验 Command `Source()` 与 `FailedRef` 一致，并拒绝旧 Generation 的迟到通知。一个失败代际只能触发一次恢复决策。

## 策略基线

```go
type RestartStrategy uint8

const (
    RestartNever RestartStrategy = iota
    RestartOnFailure
    DestroyOnFailure
)

type RestartPolicy struct {
    Strategy    RestartStrategy
    MaxRestarts int
    Window      time.Duration
    MinBackoff  time.Duration
    MaxBackoff  time.Duration
}
```

默认建议：

| Service | 默认策略 | 恢复来源 |
|---|---|---|
| BattleService | `DestroyOnFailure` | 不自动恢复；业务决定退款、判负或重开。 |
| PlayerService | `RestartOnFailure` | 最近一次已提交 Snapshot 或持久化状态。 |
| WalletService | `RestartNever` | 权威账本和人工/业务补偿流程。 |
| DiscoveryService | `RestartOnFailure` | 空状态或外部配置；当前内存租约不会伪装成已恢复。 |

## 当前实现约束

现有 Core 在 Handler panic 后同步执行 Close、清理 Timer 和 PendingCall、移出 Registry，并记录 Metrics。`Runtime.Inspect()` 不保留失败历史，也不提供失败事件流。

因此 Supervisor 不能靠 Monitor 轮询可靠恢复具体实例。第一版优先评估 Tooling decorator 在 `Handle` 的 defer 中发送失败 Command、随后重新 panic 的方案。该方案能覆盖 Handler panic，但不能覆盖 Init 期间的通知，因为 Service 尚未 Running；初次创建和重启期间的 Init 错误必须由发起 `CreateService` 的 launcher 直接处理。

## 开放问题

以下问题会改变公开接口，必须在 Phase 7E 实施计划前裁决；在此之前本 RFC 保持“草案”：

1. `Stop`、`Close` 返回错误或 panic 是否进入同一失败通知协议，还是只由调用 `Runtime.Stop` 的编排者处理。
2. 窄 launcher seam 如何在不把完整 `Runtime` 暴露给普通业务 Service 的前提下调用 `CreateService`。
3. 持久化 Store IO 和退避等待由哪个非 Service owner 执行；不得在 Service 中创建 goroutine，也不得用长 Handler 占住 Scheduler 许可。
4. 失败通知 Mailbox 满或 Supervisor 不可用时，采用日志加 Metrics 后放弃，还是需要独立有界持久队列。
5. 长期名字更新失败时，新实例应停止、保持孤立，还是进入可重试的未发布状态。

## 错误与可观测性要求

后续稳定错误至少区分：无效策略、无效通知、旧代际、重启被抑制、Snapshot 不存在、恢复失败、创建失败和名字发布失败。

指标至少区分：收到失败通知、重复/迟到通知、已安排重启、重启成功、重启失败、超过窗口被抑制和通知投递失败。错误文本不能作为策略分支依据。

## 验收要求

Phase 7E 的实施计划必须覆盖：

- Handler panic 后旧实例被 Core 清理，PendingCall 得到 `ErrServiceFailed`。
- 同一代际重复通知只触发一次决策。
- 旧代际迟到通知不影响新实例。
- `RestartNever`、`DestroyOnFailure` 和 `RestartOnFailure`。
- 窗口、上限、退避、连续创建失败和 Snapshot 不存在。
- 从已提交 Snapshot 创建新实例，且 `ServiceRef` 变化。
- 新实例成功前不替换长期名字；发布失败有确定收敛行为。
- Supervisor 自身失败不会形成自我重启环。
- 无 Service goroutine、无 Core 对 Tooling 的反向依赖，并通过 Race Detector。
