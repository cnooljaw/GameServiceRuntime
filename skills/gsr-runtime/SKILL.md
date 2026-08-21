---
name: gsr-runtime
description: 当实现或评审 GSR 的 Service、Command、Mailbox、Scheduler、Call/Reply、Timer、生命周期、Cluster、Runtime Inspection、Discovery、Monitor、ServiceGroup、Controller、Drain 及相关 RFC 时使用，尤其适用于并发、超时、任务所有权、阶段演进和 Core/Tooling 边界裁决。
---

# GSR Runtime

## 工作流

1. 先读 `docs/SUMMARY.md`、`docs/DECISIONS.md`、本次修改对应的 RFC；处理已实现能力时再读 `docs/TODO.md`。
2. 进入新 Phase 时，先审核目标 RFC、相邻 RFC、当前实现和 `RFC-0500`。缺少 owner、公开契约、失败语义或验收条件时，先补 RFC，再写测试或代码。
3. 裁决顺序是：RFC -> Skynet 设计规则 -> Skynet 源码实现。源码只用于补足未说明的语义，不照搬历史实现；结论写回 RFC 和决策索引。
4. 先确认修改属于 Core、Tooling 还是 Business；外层不得把领域类型或管理 API 压入 Core。
5. 做跨文件评审或影响分析时，先运行 `codegraph sync` 和 `codegraph status`，再用 `query`、`callers`、`callees`、`impact` 定位；图结果只用于导航，最终以源码和测试为准。
6. 先写失败测试，再做最小实现。新增导出 API 时同步更新 RFC，私有实现不因测试方便而公开。
7. 先完成一个可验证的纵向切片，再从“RFC 契约是否满足”和“模块职责是否优雅”两个维度审查。收尾时说明业务作用、非目标和下一阶段依赖。
8. 完成后运行 `go test ./...`、`go vet ./...`、`go test -race ./...`；并发敏感测试增加重复次数。

## Core 不变量

- Core 只理解 Service、ServiceRef、Command、Envelope、Mailbox、Scheduler、Timer、生命周期和 Cluster 数据面；不引入 Actor、PTYPE 或业务领域类型。
- 业务入口只有 Command，`Service.Handle` 是唯一分发入口。Runtime 不维护 per-Service Command 白名单；Service 内部按可读性选择 `switch` 或私有函数表，不公开全局可变 Registry。
- Command 必须经过 Mailbox；同一 Service 的 `Handle`、`Stop`、`Close` 不并发。Send/After 与 Stop 共享明确的消息接受边界。
- 业务状态变化只在 Mailbox handler 中发生；跨 Service 只用 ServiceRef + Send/Call，不持有对象指针或用多把锁协调状态。
- Service 不创建 goroutine。AST 门禁检查显式 Service 类型所有方法中的直接 `go` 语句；自由函数间接启动仍需评审。Runtime 创建的 Init、dispatch、Stop、Close 等任务必须有 owner、类型、完成句柄，并追踪到函数真实返回。
- 超时只能标记任务、尝试取消并释放 Runtime 自有结构；Go 不能强杀仍在运行的业务函数，也不能因此丢失任务句柄。
- Call 必须校验 Reply 来源、处理超时和迟到 Reply、拒绝同步调用环；Service 等待 Call 时要归还并恢复 Scheduler 执行许可。
- Timer 只生成 Command；取消和目标关闭必须清理绑定，投递失败需要可观测。
- 生命周期结束时收敛 Mailbox、PendingCall、Timer、Registry 和 Name；重复或并发 Stop/Close 的语义必须明确且有测试。

## 已冻结的阶段边界

- `Runtime.ResolveRemote` 只在已知节点查询其本地 `ServiceName`；动态名字、租约和节点事实由 Discovery 处理。不要把它扩展成全局目录、ServiceGroup 查询或路由入口。
- 系统 Service 同样使用动态 `ServiceID` 加稳定 `ServiceName`，不引入 `SystemServiceID`；只有 `ServiceID(0)` 是 Core 的节点端点，不分发给 Service。
- Discovery 保存节点 lease 和长期 ServiceName，不保存 ServiceSet、不选择路由、不引入 Gossip。ServiceGroup 的事实由 `DirectoryService` 持有，Router 只路由调用方显式持有的 ServiceSet。
- Timer 只在未来投递 Command；不要把 Timer Wheel 包装成 Service，也不要让 Timer 执行业务回调。
- 生产代码中的直接 `go` 默认禁止。外部阻塞工作优先使用 Core Runner：必须保持当前 Service 串行快照时把本次 `CommandContext` 传给 `Await`，允许继续处理 Mailbox 时用 `Submit` 并在结果 Command 中做业务 fencing。只有 Runtime 内部任务、Transport/连接 Adapter 的 I/O owner、或 Core Runner 无法表达且经 RFC 裁决的固定外部任务可以例外；例外必须有明确生命周期 owner、取消或关闭入口、真实返回等待和泄漏测试，且不得在 Handler 外修改 Service 状态或保存、使用 `CommandContext`、`ServiceContext`。
- Supervisor 是可选 Tooling 的故障恢复组件，不替代 Runtime 的 Create/Stop 生命周期，也不假设 Erlang/OTP 的 Supervisor Tree。只有实际承担监控、恢复和重启策略的组件才使用该名称。
- Phase 8 的 Discovery/Observer 描述 Observed State；Phase 10 才引入 Desired State、Controller、Reconcile 与 NodeAgent 执行动作。Controller 决策，NodeAgent 执行，Runtime 只提供能力。

## Cluster 与 Tooling 边界

- 基础 Cluster 默认使用静态节点配置和 `Runtime.ResolveRemote`；只有调用方不应知道节点、需要动态迁移或控制面目录时才启用 Discovery。
- 当前安全前提是“信任集群节点，不信任错误程序状态”：可信内网不等于省略 Source、租约 owner、代际和错误校验。
- `Runtime.Inspect()` 是 Core 唯一只读观测入口。`MetricsSnapshot` 只能从 `Inspect().Metrics` 取得，不为 Tooling 增加独立 Runtime getter。
- Monitor 只转换 Inspection 副本；不创建 Service、goroutine、Timer、HTTP 或远程 Command。JSON、Prometheus 和管理面协议都留在 adapter。
- 远程 Call 结果指标在统一返回出口记录，每个进入远程 Call 路径的请求只能得到一个成功或失败结果；本地 Call 和进入路径前的拒绝不计数。

## 评审清单

- 同时对照 RFC 和代码，不把未来 Cluster、Tooling 或业务能力误判为当前 Core 缺陷。
- 重点检查接受边界、串行保证、超时后残留任务、PendingCall 唤醒、资源清理和错误可观测性。
- 覆盖正常、重复、迟到、取消、超时、panic、关闭竞争和 Runtime 并发创建场景。
- 检查 Tooling 是否只消费公开副本，以及 Core 是否出现为单一 adapter 增加的 getter、网络协议或领域类型。
- 性能优化先建立完整 Send/Call/Timer 基准；没有数据时不引入 Ring Buffer、对象池或 Timer Wheel。
