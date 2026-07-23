# GSR 设计决策索引

本文提供长期设计记忆的检索入口，帮助回答“为什么这样设计”。它不定义公开 API，也不替代 RFC；每项结论的完整边界、失败语义和验收条件均以链接的 RFC 为准。

## 使用方式

1. 先按关键词找到相关决策。
2. 阅读“权威来源”中的 RFC，而不是只根据本页摘要修改代码。
3. 若结论影响当前 Phase，再阅读路线图和实现测试。
4. 新结论先写入 RFC，再更新本索引。不要只留在聊天记录、`AGENTS.md` 或 Skill 中。

## 运行时模型

| 决策 | 结论 | 原因摘要 | 权威来源 |
| --- | --- | --- | --- |
| D-001：使用 Service，而非 Actor | GSR 使用 `Service`、`ServiceRef`、`Command`、`Send`、`Call`，不引入 Actor 术语或模型。 | 运行时要表达可寻址状态、Mailbox 串行和显式生命周期，不把框架误解为 Erlang/OTP 的 Actor Tree。 | [术语表](rfcs/RFC-0000-Foundation-Glossary.md)、[设计原则](rfcs/RFC-0001-Foundation-Design-Principles.md) |
| D-002：Core 与业务解耦 | Core 只提供通用运行语义；Tooling 提供工程策略；Business Layer 持有游戏领域状态。 | 让 Runtime 保持可组合、可测试，避免为单一业务或 adapter 扩张内核。 | [设计原则](rfcs/RFC-0001-Foundation-Design-Principles.md)、[冲突裁决](rfcs/RFC-0002-Foundation-Conflict-Resolution.md) |
| D-003：状态只经 Mailbox 变化 | Service 的业务状态只在 Command handler 中修改；跨 Service 只传 `ServiceRef` 与 Command。 | 保持单一状态 owner 与可推理的串行边界，避免对象指针、多锁和旁路状态。 | [Service](rfcs/RFC-0100-Core-Service.md)、[Mailbox](rfcs/RFC-0150-Core-Mailbox.md) |
| D-004：异步必须有 owner | Service 默认不创建 goroutine；Timer 只投递 Command；例外任务必须有生命周期 owner、关闭入口和真实返回追踪。 | 防止 Handler 外状态竞争、关闭泄漏和看似超时却仍在运行的任务。 | [设计原则](rfcs/RFC-0001-Foundation-Design-Principles.md)、[生命周期](rfcs/RFC-0180-Core-Lifecycle.md) |
| D-005：Timer 不是 Service | Timer 是 Core 的未来 Command 投递能力，不执行回调；不把 Timer Wheel 设计成 Service。 | 定时不应绕过 Mailbox，也不应增加没有独立状态或地址价值的运行单元。 | [Timer](rfcs/RFC-0170-Core-Timer.md) |
| D-006：不引入 `SystemServiceID` | 系统 Service 与普通 Service 一样使用动态 `ServiceID` 和稳定 `ServiceName`；只有 `ServiceID(0)` 保留给 Core 节点端点。 | 避免魔法实例 ID 与部署配置耦合，区分实例地址、长期名字和 Runtime 自身的节点级协议。 | [ServiceRef 与寻址](rfcs/RFC-0110-Core-ServiceRef.md) |

## Cluster 与 Tooling

| 决策 | 结论 | 原因摘要 | 权威来源 |
| --- | --- | --- | --- |
| D-007：`ResolveRemote` 只做节点级查询 | 已知节点上的长期名字通过 `Runtime.ResolveRemote` 查询其本地实例；它不是全局目录或路由 API。 | 静态 bootstrap 与动态发现分层，避免把节点级能力膨胀为控制面。 | [Cluster Data Plane](rfcs/RFC-0190-Core-Cluster-Data-Plane.md)、[Discovery](rfcs/RFC-0200-Tooling-Discovery.md) |
| D-008：Discovery 属于 Tooling，且不做 Gossip | Discovery 保存节点 lease 和长期 ServiceName；不保存 ServiceGroup、不决定路由、不复制为 Gossip。 | 第一版先提供单一、可验证的权威事实，避免选主、一致性和传播协议过早进入 Runtime。 | [Discovery](rfcs/RFC-0200-Tooling-Discovery.md) |
| D-009：Supervisor 是可选恢复策略 | Runtime 负责实例 Create/Stop；`Supervisor` 只在真正承担失败监控、恢复预算和重启策略时作为 Tooling 存在，不是 Core 生命周期替身或 OTP Tree。 | 避免承诺不存在的自动恢复语义，同时允许对已定义的 panic 失败路径做受限恢复。 | [Supervisor](rfcs/RFC-0220-Tooling-Supervisor.md) |
| D-010：Observed State 先于 Desired State | Discovery、NodeAgent、Observer 只描述当前事实；Controller 负责 Desired State 与 Reconcile，NodeAgent 只执行动作。 | 将“看见系统”与“改变系统”的权限、失败处理和策略分开，防止只读观测面悄然变成运维控制面。 | [Control Plane](rfcs/RFC-0250-Tooling-Cluster-Control-Plane.md)、[Drain 与热更新](rfcs/RFC-0270-Tooling-Drain-Hot-Reload.md)、[路线图](rfcs/RFC-0500-Roadmap.md) |
| D-011：ServiceGroup 独立于 Discovery | `DirectoryService` 是 ServiceSet 的唯一事实 owner；Router 只使用调用方显式持有的快照。 | 避免 Discovery 兼任目录和负载策略，也避免看似异步的 Send 隐含同步远程查询或后台缓存。 | [ServiceGroup](rfcs/RFC-0260-Tooling-ServiceGroup-Routing.md) |
| D-012：Drain 使用入口 guard、版本切换与访问者 lease | Drain Guard 在旧实例自己的 Mailbox 内拒绝显式外部 Command；受控操作在新实例就绪后发布更高 ServiceSet，再等待 VisitorRegistryService 的强 lease 清零，最后才 Stop。Guard 开始后不得重新发布原旧 Ref；恢复需新实例和更高版本。 | 热更新不是进程内代码补丁，缓存旧 ServiceRef 不能只靠切流阻止；不可逆 Guard 使“原 Ref 回滚”不再成立，访问关系不能散落在调用方共享 map，更不能污染 Core 最小接口。 | [Drain 与热更新](rfcs/RFC-0270-Tooling-Drain-Hot-Reload.md)、[Drain Guard](rfcs/RFC-0271-Tooling-Drain-Guard.md)、[受控 Drain 操作](rfcs/RFC-0272-Tooling-Controlled-Drain-Operation.md) |

## 演进与协作

| 决策 | 结论 | 原因摘要 | 权威来源 |
| --- | --- | --- | --- |
| D-013：分阶段只引入一个核心问题 | Phase 10A 先冻结 Visitor lease，Phase 10B 再独立交付入口 Drain Guard；ServiceSet 切换、回滚、Desired State、Controller、Reconcile 与修改型 NodeAgent 动作进入后续带操作身份的契约。 | 将复杂度拆成可验收的能力，避免把自动恢复、调度和策略塞入 Discovery 或 Core，也不让无审计的 decorator 承担未知提交恢复。 | [路线图](rfcs/RFC-0500-Roadmap.md)、[Drain 与热更新](rfcs/RFC-0270-Tooling-Drain-Hot-Reload.md)、[Drain Guard](rfcs/RFC-0271-Tooling-Drain-Guard.md) |
| D-014：聊天结论必须回写 | 聊天用于探索；稳定结论先进入 RFC，再进入本索引，最后按需同步路线图、`AGENTS.md` 和 Skill。 | 让归档后的设计理由仍可检索，避免后续实现靠重新推理或误读历史上下文。 | [RFC 生命周期](rfcs/RFC-0003-Foundation-RFC-Lifecycle.md)、[Codex 开发指南](GSR-Book/06-第六篇-实践/02-Codex开发指南.md) |
| D-015：修改型 Drain 先保存操作事实 | Phase 10C1 的 Coordinator 以 Gateway source、Principal 与 RequestID 约束 Start/Resolve，并保存 Directory/Guard/Visitor 的未知或已确认阶段；它只到 ReadyToStop。 | 超时不能证明 Publish 或 Begin 未发生，且 Runtime.Stop 需要节点级生命周期 owner；把操作事实、动作授权和执行 Stop 分开，才能避免脚本式重试破坏版本或重新接流被 Guard 的旧 Ref。 | [受控 Drain 操作](rfcs/RFC-0272-Tooling-Controlled-Drain-Operation.md) |
