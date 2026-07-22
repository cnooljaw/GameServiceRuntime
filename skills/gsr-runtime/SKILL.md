---
name: gsr-runtime
description: 当实现或评审 GSR 的 Service、Command、Mailbox、Scheduler、Call/Reply、Timer、生命周期、Cluster、Runtime Inspection、Discovery、Monitor 及相关 RFC 时使用，尤其适用于并发、超时、任务所有权和 Core/Tooling 边界裁决。
---

# GSR Runtime

## 工作流

1. 先读 `docs/SUMMARY.md`、本次修改对应的 RFC；处理已实现能力时再读 `docs/TODO.md`。
2. 裁决顺序是：RFC -> Skynet 设计规则 -> Skynet 源码实现。源码只用于补足未说明的语义，不照搬历史实现；结论写回 RFC。
3. 先确认修改属于 Core、Tooling 还是 Business；外层不得把领域类型或管理 API 压入 Core。
4. 做跨文件评审或影响分析时，先运行 `codegraph sync` 和 `codegraph status`，再用 `query`、`callers`、`callees`、`impact` 定位；图结果只用于导航，最终以源码和测试为准。
5. 先写失败测试，再做最小实现。新增导出 API 时同步更新 RFC，私有实现不因测试方便而公开。
6. 完成后运行 `go test ./...`、`go vet ./...`、`go test -race ./...`；并发敏感测试增加重复次数。

## Core 不变量

- Core 只理解 Service、ServiceRef、Command、Envelope、Mailbox、Scheduler、Timer、生命周期和 Cluster 数据面；不引入 Actor、PTYPE 或业务领域类型。
- 业务入口只有 Command。Service 可公开实现 `CommandDeclarer`，Runtime 在创建时复制并冻结私有命令集；不公开可变 Registry。
- Command 必须经过 Mailbox；同一 Service 的 `Handle`、`Stop`、`Close` 不并发。Send/After 与 Stop 共享明确的消息接受边界。
- 业务状态变化只在 Mailbox handler 中发生；跨 Service 只用 ServiceRef + Send/Call，不持有对象指针或用多把锁协调状态。
- Service 不创建 goroutine。AST 门禁检查显式 Service 类型所有方法中的直接 `go` 语句；自由函数间接启动仍需评审。Runtime 创建的 Init、dispatch、Stop、Close 等任务必须有 owner、类型、完成句柄，并追踪到函数真实返回。
- 超时只能标记任务、尝试取消并释放 Runtime 自有结构；Go 不能强杀仍在运行的业务函数，也不能因此丢失任务句柄。
- Call 必须校验 Reply 来源、处理超时和迟到 Reply、拒绝同步调用环；Service 等待 Call 时要归还并恢复 Scheduler 执行许可。
- Timer 只生成 Command；取消和目标关闭必须清理绑定，投递失败需要可观测。
- 生命周期结束时收敛 Mailbox、PendingCall、Timer、Registry 和 Name；重复或并发 Stop/Close 的语义必须明确且有测试。

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
- 对返回稳定参数错误的 interface API，明确并测试 literal nil 与 typed nil 的语义，不能让校验成功后在内部延迟 panic。
- 检查 Tooling 是否只消费公开副本，以及 Core 是否出现为单一 adapter 增加的 getter、网络协议或领域类型。
- 性能优化先建立完整 Send/Call/Timer 基准；没有数据时不引入 Ring Buffer、对象池或 Timer Wheel。
