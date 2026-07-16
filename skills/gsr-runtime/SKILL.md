---
name: gsr-runtime
description: 当实现或评审 GSR 的 Service、Command、Mailbox、Scheduler、Call/Reply、Timer、生命周期、Cluster 数据面及相关 RFC 时使用，尤其适用于并发、超时、任务所有权和 Core 边界裁决。
---

# GSR Runtime

## 工作流

1. 先读 `docs/SUMMARY.md`、本次修改对应的 RFC；处理已实现能力时再读 `docs/TODO.md`。
2. 裁决顺序是：RFC -> Skynet 设计规则 -> Skynet 源码实现。源码只用于补足未说明的语义，不照搬历史实现；结论写回 RFC。
3. 先确认修改属于 Core、Tooling 还是 Business；外层不得把领域类型或管理 API 压入 Core。
4. 先写失败测试，再做最小实现。新增导出 API 时同步更新 RFC，私有实现不因测试方便而公开。
5. 完成后运行 `go test ./...`、`go vet ./...`、`go test -race ./...`；并发敏感测试增加重复次数。

## Core 不变量

- Core 只理解 Service、ServiceRef、Command、Envelope、Mailbox、Scheduler、Timer、生命周期和 Cluster 数据面；不引入 Actor、PTYPE 或业务领域类型。
- 业务入口只有 Command。Service 可公开实现 `CommandDeclarer`，Runtime 在创建时复制并冻结私有命令集；不公开可变 Registry。
- Command 必须经过 Mailbox；同一 Service 的 `Handle`、`Stop`、`Close` 不并发。Send/After 与 Stop 共享明确的消息接受边界。
- 业务状态变化只在 Mailbox handler 中发生；跨 Service 只用 ServiceRef + Send/Call，不持有对象指针或用多把锁协调状态。
- Service 不创建 goroutine。Runtime 创建的 Init、dispatch、Stop、Close 等任务必须有 owner、类型、完成句柄，并追踪到函数真实返回。
- 超时只能标记任务、尝试取消并释放 Runtime 自有结构；Go 不能强杀仍在运行的业务函数，也不能因此丢失任务句柄。
- Call 必须校验 Reply 来源、处理超时和迟到 Reply、拒绝同步调用环；Service 等待 Call 时要归还并恢复 Scheduler 执行许可。
- Timer 只生成 Command；取消和目标关闭必须清理绑定，投递失败需要可观测。
- 生命周期结束时收敛 Mailbox、PendingCall、Timer、Registry 和 Name；重复或并发 Stop/Close 的语义必须明确且有测试。

## 评审清单

- 同时对照 RFC 和代码，不把未来 Cluster、Tooling 或业务能力误判为当前 Core 缺陷。
- 重点检查接受边界、串行保证、超时后残留任务、PendingCall 唤醒、资源清理和错误可观测性。
- 覆盖正常、重复、迟到、取消、超时、panic、关闭竞争和 Runtime 并发创建场景。
- 性能优化先建立完整 Send/Call/Timer 基准；没有数据时不引入 Ring Buffer、对象池或 Timer Wheel。
