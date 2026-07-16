# GSR Phase 7A Runtime Inspection 实施计划

> 状态：待执行

**目标：** 在不引入管理面、网络入口或业务概念的前提下，为 Runtime Tooling 提供一个只读、独立副本、并发安全的观测边界；完成 Core 文档同步、API 专项 Review 和首个 `v0.1.0` 标签。

**范围：** `RFC-0230`、`CF-009` 至 `CF-011`。本计划是 Phase 7 的第一个子阶段，不实现完整 Monitor、Discovery、Supervisor、Login 或 Gateway。

## 设计裁决

### 唯一入口

Core 只新增一个观测入口：

```go
func (r *Runtime) Inspect() RuntimeInspection
```

`Inspect` 返回独立副本，不返回 Service、Mailbox、PendingCall、Timer、Task、Registry 或 Transport 指针。Go 调用方可以修改自己持有的切片和字段，但这种修改不能影响 Runtime 或后续 Inspection。

### 数据模型

```go
type RuntimeStatus int

const (
    RuntimeRunning RuntimeStatus = iota
    RuntimeClosing
    RuntimeClosed
)

type RuntimeTaskKind string

const (
    RuntimeTaskInit     RuntimeTaskKind = "init"
    RuntimeTaskDispatch RuntimeTaskKind = "dispatch"
    RuntimeTaskStop     RuntimeTaskKind = "stop"
    RuntimeTaskClose    RuntimeTaskKind = "close"
)

type RuntimeInspection struct {
    CapturedAt   time.Time
    Node         NodeID
    Status       RuntimeStatus
    Services     []ServiceInspection
    Tasks        []RuntimeTaskInspection
    PendingCalls int
    Timers       int
    Metrics      MetricsSnapshot
}

type ServiceInspection struct {
    Ref          ServiceRef
    Name         ServiceName
    Status       ServiceStatus
    MailboxDepth int
}

type RuntimeTaskInspection struct {
    ID        uint64
    Owner     ServiceRef
    Kind      RuntimeTaskKind
    StartedAt time.Time
    TimedOut  bool
}
```

上述导出类型和字段名作为本阶段 API 评审基线。`RuntimeTaskKind` 的稳定值只允许 `init`、`dispatch`、`stop`、`close`。

### 一致性

`Inspect` 是并发安全的只读快照，但不是跨 Registry、PendingCall、Timer、Task 和 Metrics 的停机事务快照。各子系统在自己的锁内复制数据，调用方必须把结果理解为同一采集过程中的最终一致视图。

`Services` 按 `ServiceRef` 排序，`Tasks` 按任务 ID 排序。确定顺序属于公开行为，不能依赖 Go map 遍历顺序。

### 非目标

- 不增加 HTTP、CLI、Web Console 或 Prometheus exporter。
- 不实现 `MonitorService`、`NodeAgentService` 或远程管理 Command。
- 不暴露 Cluster 连接对象、Transport 内部状态或管理命令。
- 不记录每个 Service 的 Last Command；这会增加热路径写入，留待真实诊断需求证明后再做。
- 不实现 `RFC-0210` 的业务状态 Snapshot；`RuntimeInspection` 只用于观测，不能用于恢复。
- 不实现 Discovery、ServiceGroup、Supervisor、LoginService 或 Gateway。

## Task 1：冻结观测契约

**文件：**

- 修改 `docs/rfcs/RFC-0230-Tooling-Monitor.md`
- 修改 `docs/TODO.md`
- 修改 `docs/rfcs/RFC-0500-Roadmap.md`
- 修改本计划

**步骤：**

1. 在 `RFC-0230` 增加 `RuntimeInspection`、`ServiceInspection` 和 `RuntimeTaskInspection` 的正式定义。
2. 明确 `Inspect` 可在 Running、Closing 和 Closed 状态调用，不返回 error，也不延长 Runtime 生命周期。
3. 明确快照是最终一致视图、结果为副本、顺序稳定、禁止暴露可变对象。
4. 把 Cluster 连接状态、NodeAgent 和远程查询移到 Phase 8，不作为 Phase 7A 验收条件。
5. 在 `TODO.md` 把 `CF-009` 标记为“执行中”；`CF-010`、`CF-011` 保持未完成。
6. 在 `RFC-0500` 将 Phase 7 拆为 7A 至 7E，并把当前阶段设为 7A。
7. 提交：`docs(tooling): 裁决 Runtime 只读观测边界`。

## Task 2：实现基础 Inspection

**文件：**

- 新增 `runtime/inspection.go`
- 新增 `runtime/inspection_test.go`
- 修改 `runtime/registry.go`
- 修改 `runtime/mailbox.go`
- 修改 `runtime/runtime.go`

**先写失败测试：**

1. `TestRuntimeInspectReportsIdentityAndStatus`：新 Runtime 返回 Node、Running 和固定 `CapturedAt`。
2. `TestRuntimeInspectReturnsServicesInStableOrder`：创建多个命名 Service，结果按 `ServiceRef` 排序，并包含名称、状态和 Mailbox 深度。
3. `TestRuntimeInspectReturnsIndependentCopies`：修改第一次返回的 `Services` 切片和字段，不影响第二次结果。
4. `TestRuntimeInspectWorksAfterClose`：关闭后仍可读取 Closed 状态，且不 panic。

运行并确认测试先失败：

```bash
go test ./runtime -run '^TestRuntimeInspect' -count=1
```

**最小实现：**

1. 在 `runtime/inspection.go` 定义导出只读模型和完整 Go doc。
2. 增加内部 Runtime 状态到公开 `RuntimeStatus` 的单向转换，不把公开状态常量用于调度分支。
3. `localRegistry` 在锁内复制实例列表；`mailbox` 增加只读深度方法。
4. `Runtime.Inspect` 读取副本并排序，不持有多个子系统锁。
5. 使用 `Runtime.now` 生成 `CapturedAt`，保证测试和生产时间源一致。
6. 运行：

   ```bash
   go test ./runtime -run '^TestRuntimeInspect' -count=30
   go test -race ./runtime -run '^TestRuntimeInspect' -count=10
   ```

7. 提交：`feat(runtime): 增加只读运行状态观测`。

## Task 3：补齐资源和任务观测

**文件：**

- 修改 `runtime/inspection.go`
- 修改 `runtime/inspection_test.go`
- 修改 `runtime/call.go`
- 修改 `runtime/timer.go`
- 修改 `runtime/task.go`

**先写失败测试：**

1. `TestRuntimeInspectReportsPendingCallsAndTimers`：创建一个等待 Reply 的 Call 和一个长 Timer，观测数量为 1；释放后最终回到 0。
2. `TestRuntimeInspectReportsActiveTask`：使用阻塞 Init 建立活动任务，检查 owner、`init`、开始时间和任务 ID。
3. `TestRuntimeInspectReportsTimedOutTask`：让 Init 超过 Runtime 关闭期限，检查残留任务的 `TimedOut=true`；Init 退出后任务消失。
4. `TestRuntimeInspectIsSafeDuringConcurrentLifecycleChanges`：并发 Send、After、Cancel、Stop 和 Inspect；断言不 panic，并由 Race Detector 验证无数据竞争。

运行并确认测试先失败：

```bash
go test ./runtime -run '^TestRuntimeInspect(ReportsPendingCallsAndTimers|ReportsActiveTask|ReportsTimedOutTask|IsSafeDuringConcurrentLifecycleChanges)$' -count=1
```

**最小实现：**

1. 给 `pendingCalls`、`timerManager` 增加包内只读计数方法，各自在自己的锁内读取。
2. 复用 `taskTracker.active()` 的复制语义，将私有任务快照转换为导出 `RuntimeTaskInspection`。
3. 不公开取消函数、done channel 或内部任务句柄。
4. `Inspect` 依次采集子系统，不为追求原子性增加 Runtime 全局锁。
5. 运行：

   ```bash
   go test ./runtime -run '^TestRuntimeInspect' -count=50
   go test -race ./runtime -run '^TestRuntimeInspect' -count=20
   ```

6. 提交：`feat(runtime): 补齐资源与任务观测`。

## Task 4：同步 Core 文档

**文件：**

- 修改 `README.md`
- 修改 `docs/rfcs/RFC-0100-Core-Service.md`
- 修改 `docs/rfcs/RFC-0110-Core-ServiceRef.md`
- 修改 `docs/rfcs/RFC-0120-Core-Command.md`
- 修改 `docs/rfcs/RFC-0130-Core-Send-Call-Reply.md`
- 修改 `docs/rfcs/RFC-0140-Core-Session-PendingCall.md`
- 修改 `docs/rfcs/RFC-0150-Core-Mailbox.md`
- 修改 `docs/rfcs/RFC-0160-Core-Scheduler.md`
- 修改 `docs/rfcs/RFC-0170-Core-Timer.md`
- 修改 `docs/rfcs/RFC-0180-Core-Lifecycle.md`
- 修改 `docs/rfcs/RFC-0190-Core-Cluster-Data-Plane.md`
- 修改 `docs/rfcs/RFC-0191-Core-Cluster-Transport.md`
- 修改 `docs/GSR-Book/02-第二篇-Runtime内核/01-Service.md`
- 修改 `docs/GSR-Book/02-第二篇-Runtime内核/02-ServiceRef.md`
- 修改 `docs/GSR-Book/02-第二篇-Runtime内核/03-Command.md`
- 修改 `docs/GSR-Book/02-第二篇-Runtime内核/04-Mailbox.md`
- 修改 `docs/GSR-Book/02-第二篇-Runtime内核/05-Scheduler.md`
- 修改 `docs/GSR-Book/02-第二篇-Runtime内核/06-Timer.md`
- 修改 `docs/GSR-Book/02-第二篇-Runtime内核/07-Lifecycle.md`
- 修改 `docs/GSR-Book/03-第三篇-Cluster/01-Cluster.md`
- 修改 `docs/GSR-Book/03-第三篇-Cluster/02-Transport.md`
- 修改 `docs/GSR-Book/03-第三篇-Cluster/04-Session.md`
- 修改 `docs/TODO.md`

**步骤：**

1. 修正 README 中 Cluster Transport 仍在规划的过期说明，补充本地和双节点示例入口。
2. 对照已实现代码逐份检查 `RFC-0100` 至 `RFC-0191`；只在内容与实现一致后将状态改为“已接受”。
3. 回填 GSR Book 已实现的 Core 与 Cluster 章节；Discovery 章节继续保持 Draft。
4. 文档只描述已实现能力。Discovery、Supervisor、Snapshot、NodeAgent、Login 和业务模板继续链接对应草案 RFC。
5. 把 `CF-009`、`CF-010` 标记为已完成，`CF-011` 保持待发布。
6. 检查 Markdown 链接和过期措辞：

   ```bash
   rg -n 'Cluster Transport.*规划|RFC-01(00|10|20|30|40|50|60|70|80|90|91).*草案|Core Runtime.*未完成' README.md docs
   ```

7. 提交：`docs(core): 同步已实现的 Runtime 与 Cluster`。

## Task 5：执行 API 冻结 Review

**文件：** 根据 Review 结论修改 RFC、代码和测试；不得为了通过 Review 扩大 Phase 7A 范围。

**Review 轴：**

1. RFC 轴：检查 `Runtime.Inspect` 是否完全符合 `RFC-0230` 的只读、副本、最终一致和稳定排序规则。
2. Core 轴：检查是否暴露可变 Registry、Service 指针、Mailbox、channel、取消句柄或 Transport 细节。
3. 并发轴：检查锁顺序、关闭竞争、残留 Init 任务和 Timer 回调竞争。
4. API 轴：检查导出命名、Go doc、零值含义和未来兼容性。
5. 性能轴：确认 Send、Call、Timer 热路径没有因为 Inspection 增加额外写入或全局锁。

**验证：**

```bash
go test ./... -count=1
go vet ./...
go test -race ./... -count=1
go test ./runtime -run '^TestRuntimeInspect' -count=100
go test ./runtime -run '^$' -bench 'Benchmark(Send|CallReply|ManyServices|TimerDelivery)$' -benchmem -benchtime=1000x -count=5
```

P1/P2 Review 结论必须清零后才能进入 Task 6。修复按垂直切片单独提交，提交信息使用中文。

## Task 6：发布 Core v0.1.0

**文件：**

- 新增 `CHANGELOG.md`
- 修改 `docs/TODO.md`
- 修改 `docs/rfcs/RFC-0500-Roadmap.md`
- 修改本计划

**步骤：**

1. 在 `CHANGELOG.md` 记录 Core Runtime、Cluster Data Plane、TCP Transport、限制条件和兼容性承诺；明确当前 TCP 只允许可信内网。
2. 把 `CF-011` 标记为已完成。
3. 将路线图当前阶段更新为 Phase 7B：最小 DiscoveryService。
4. 把本计划状态更新为“已完成”，记录测试、Benchmark 对比和 Review 结论。
5. 最终运行：

   ```bash
   go test ./... -count=1
   go vet ./...
   go test -race ./... -count=1
   go run ./examples/local-runtime
   go run ./examples/cluster-runtime
   git diff --check
   git status --short
   ```

6. 提交：`docs(release): 发布 Core v0.1.0`。
7. 确认工作区干净后创建附注标签：

   ```bash
   git tag -a v0.1.0 -m 'GSR Core Runtime v0.1.0'
   git show --stat --oneline v0.1.0
   ```

8. 本计划不执行 `git push`；远端发布由后续明确操作处理。

## 完成标准

- `Runtime.Inspect()` 是唯一新增 Core 观测入口。
- 所有返回数据都是副本，不含可变内部对象。
- Inspection 在创建、投递、计时、停止和关闭竞争中通过 Race Detector。
- `RFC-0100` 至 `RFC-0191` 与代码一致并标记为已接受。
- README 和 GSR Book 不再把已完成能力写成规划项。
- API 专项 Review 无未处理 P1/P2。
- 全量测试、vet、race、示例和基准完成。
- `v0.1.0` 指向干净、可复验的提交。
