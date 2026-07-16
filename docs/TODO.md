# GSR 待办列表

> 更新时间：2026-07-17
>
> 作用：记录已实现里程碑的工程欠账和收口项；尚未开始的新能力仍以 `RFC-0500` 为准。

## 当前结论

**Core Runtime** 已经完成 `RFC-0100` 至 `RFC-0192` 的功能实现，覆盖：

- Service、ServiceRef、ServiceName、Registry 和私有只读 Command 集。
- Mailbox、ReadyQueue、固定执行许可池和串行 Handler。
- Send、Call、Reply、Session、PendingCall 和同步调用环检测。
- Timer 到 Command 的统一投递链路。
- Stop、Close、超时、panic 隔离、任务追踪和基础指标。
- 本地端到端示例及并发、生命周期一致性测试。
- Cluster Router、WireEnvelope、远程 Send/Call/Reply 和跨节点调用环检测。
- TCP 握手、受限长度帧、连接复用、断线通知及双节点端到端示例。

当前状态定义为：**Core Runtime 功能闭环，公开 API 尚未发布稳定版本**。Runtime Tooling 和业务模板属于后续里程碑，不计入 Core 未完成项。

Core API 冻结前的 P1 收口已于 2026-07-17 完成。

Cluster 前的 P2 工程门禁、Phase 5 Cluster Data Plane 和 Phase 6 Core Runtime 验证已于 2026-07-17 完成。当前执行 Phase 7A Runtime Inspection 与 Core 首版发布；P3 Tooling 项按下表跟踪。

## P1：Core API 冻结前完成

| 编号 | 状态 | 事项 | 完成标准 |
|---|---|---|---|
| CF-001 | 已完成 | 收敛 Runtime 重复关闭语义 | 并发或重复调用 `Runtime.Close` 时复用同一次关闭流程，并返回一致结果；补充成功、超时和调用方取消测试。同步裁决重复 `Runtime.Stop` 是等待已有结果还是稳定返回已关闭错误。 |
| CF-002 | 已完成 | 明确 Init 的取消和超时边界 | 先更新 `RFC-0180`，明确保留无 context 的短初始化约束，或在 API 冻结前引入可取消 Init；覆盖 Init 永久阻塞时 Runtime 按期返回、任务保持可观测，以及 Init 后续退出时任务记录被回收。 |
| CF-003 | 已完成 | 补齐 Timer 投递失败可观测性 | Timer 到期后因目标关闭、Mailbox 满或 Runtime 关闭而投递失败时，按原因记录指标；预期关闭不得产生无意义错误日志。 |
| CF-004 | 已完成 | 建立 Core 性能与泄漏基线 | 增加完整 Send、Call/Reply、批量 Service、批量 Timer benchmark，记录 `allocs/op` 和吞吐；增加 Runtime 反复创建关闭后的 goroutine、任务、Timer、PendingCall 泄漏测试。优化只依据基准结果，不预先引入 Ring Buffer、对象池或 Timer Wheel。 |

## P2：进入 Cluster 前完成

| 编号 | 状态 | 事项 | 完成标准 |
|---|---|---|---|
| CF-005 | 已完成 | 保护 SessionID 回绕和冲突 | `PendingCall` 分配 Session 时跳过 `0`，不得覆盖仍在等待的 Session；在远程 Reply 接入前补充回绕测试。 |
| CF-006 | 已完成 | 建立持续集成质量门禁 | CI 固定执行 `go test ./...`、`go vet ./...` 和 `go test -race ./...`；示例程序至少执行一次。 |
| CF-007 | 已完成 | 拆分超大一致性测试文件 | 按 Scheduler、Lifecycle、Call、Observability、Command Registry 拆分 `runtime/conformance_test.go`，只移动测试和共享 fixture，不改变行为。 |
| CF-008 | 已完成 | 固化“Service 不创建裸 goroutine”规则 | 在工程检查中检测项目自有 Service 实现中的直接 `go` 语句，或提供等价静态分析；Runtime 内部 goroutine 必须继续经过任务追踪或具有明确 Runtime 所有权。 |

## P3：Tooling 阶段承接

| 编号 | 状态 | 事项 | 完成标准 |
|---|---|---|---|
| CF-009 | 已完成 | 向 Monitor 提供只读运行任务快照 | 通过 `Runtime.Inspect` 暴露任务类型、owner、开始时间和超时标记；不公开可变 task registry，不把 Monitor API 放入 Core Service 接口。 |
| CF-010 | 执行中 | 同步教程和 RFC 状态 | 以实现为准回填 `docs/GSR-Book` 的 Core Runtime 章节；完成 API 冻结评审后，把 `RFC-0100` 至 `RFC-0192` 从“草案”更新为明确的已接受状态。 |
| CF-011 | 待处理 | 发布首个 Core 版本 | P1、P2 清零后整理变更说明，执行全量质量命令并建立首个版本标签；标签前不承诺公开 API 稳定性。 |

## 验收命令

每次关闭待办前至少执行：

```bash
go test ./...
go vet ./...
go test -race ./...
go run ./examples/local-runtime
go run ./examples/cluster-runtime
```
