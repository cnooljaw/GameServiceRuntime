# GSR Core API 收口实施计划

> 状态：执行中

**目标：** 完成 `docs/TODO.md` 中 `CF-001` 至 `CF-004`，在进入 Cluster 前冻结单节点 Core Runtime 的生命周期语义，并建立性能与资源基线。

**范围：** 只修改 `RFC-0170`、`RFC-0180`、`RFC-0240`、`runtime/` 测试与实现，以及状态文档。不引入 Cluster、Tooling、业务类型、第三方依赖、Ring Buffer、对象池或 Timer Wheel。

## 已裁决语义

1. `Runtime.Close` 只执行一次。首个调用者启动关闭，其他调用者等待同一个结果；等待者自己的 context 可以提前结束等待，但不启动第二次关闭。
2. `Runtime.Stop` 遇到正在停止或正在失败清理的实例时等待已有结果；实例已从 Registry 删除后稳定返回 `ErrServiceClosed`。
3. 保留 `Init(ServiceContext) error`。它是同步、短时的内存和本地资源初始化钩子，不承担无界 IO 或长期后台工作；长初始化通过 Command 和显式业务状态完成。
4. Runtime 追踪 Init 到真实返回。关闭超时只能释放 Runtime 自有结构并报告残留任务，不能强杀 Init，也不能提前删除任务句柄。
5. Timer 投递失败按原因计数。目标关闭和 Runtime 关闭属于预期丢弃，不记录错误日志；未知错误才记录结构化日志。
6. 优化由完整链路 benchmark 驱动。Go timer 不等同于“每个 Timer 常驻一个 goroutine”，未测得瓶颈前不实现 Timer Wheel。

## Task 1：收敛 Close 与 Stop

**文件：** 修改 `docs/rfcs/RFC-0180-Core-Lifecycle.md`、`runtime/runtime.go`、`runtime/lifecycle.go`；新增 `runtime/lifecycle_join_test.go`。

1. 在 RFC 中写明首个 `Runtime.Close` 决定关闭结果、并发调用加入等待、等待者 context 只取消自身等待；重复调用返回已保存结果。
2. 先增加以下测试：
   - `TestConcurrentRuntimeCloseJoinsSameResult`
   - `TestRepeatedRuntimeCloseReturnsSavedTimeout`
   - `TestRuntimeCloseJoinerCanCancelWait`
   - `TestConcurrentServiceStopJoinsExistingResult`
3. 运行：

   ```bash
   go test ./runtime -run 'TestConcurrentRuntimeClose|TestRepeatedRuntimeClose|TestRuntimeCloseJoiner|TestConcurrentServiceStop' -count=1
   ```

   预期：当前实现至少因第二个 Close 返回 `ErrRuntimeClosed`、重复超时结果丢失而失败。
4. 在 `Runtime` 内增加私有 `closeDone`、`closeResult` 和结果保护锁。把实际关闭流程拆为私有方法；关闭完成后一次性发布结果。
5. `Stop` 的状态 CAS 失败时，如果实例仍在停止或失败清理中，则通过 `serviceInstance.wait(ctx)` 加入现有流程，不创建第二个 Stop 请求。
6. 运行目标测试 30 次和 race：

   ```bash
   go test ./runtime -run 'TestConcurrentRuntimeClose|TestRepeatedRuntimeClose|TestRuntimeCloseJoiner|TestConcurrentServiceStop' -count=30
   go test -race ./runtime -run 'TestConcurrentRuntimeClose|TestRepeatedRuntimeClose|TestRuntimeCloseJoiner|TestConcurrentServiceStop' -count=1
   ```

7. 提交：`fix(runtime): 收敛重复关闭与停止语义`。

## Task 2：固定 Init 边界

**文件：** 修改 `docs/rfcs/RFC-0180-Core-Lifecycle.md`；新增 `runtime/lifecycle_init_test.go`。

1. 在 RFC 中明确 Init 是同步短初始化、没有取消 context；禁止在 Init 中创建后台 goroutine 或执行无界阻塞工作。
2. 说明 Runtime 关闭等待 Init 受 `ShutdownTimeout` 约束；超时后 Init 任务保持可观测，若稍后返回则回收任务记录并只执行一次 Service Close。
3. 增加 `TestRuntimeCloseTimesOutWithBlockedInitAndTracksUntilReturn`，验证：
   - Runtime 在 `ShutdownTimeout` 后返回 `ErrCloseTimeout`。
   - 阻塞期间 `runtime_tasks_active == 1` 且残留任务被报告。
   - 释放 Init 后 `CreateService` 返回 `ErrRuntimeClosed`、Service Close 恰好一次、任务数最终为 `0`。
4. 运行：

   ```bash
   go test ./runtime -run 'TestRuntimeClose.*Init|TestRuntimeCloseWaitsForServiceInitialization' -count=30
   go test -race ./runtime -run 'TestRuntimeClose.*Init|TestRuntimeCloseWaitsForServiceInitialization' -count=1
   ```

5. 提交：`test(runtime): 固定 Init 超时与任务追踪边界`。

## Task 3：观测 Timer 投递结果

**文件：** 修改 `docs/rfcs/RFC-0170-Core-Timer.md`、`runtime/runtime.go`；新增 `runtime/timer_delivery_test.go`。

1. 在 RFC 中定义：
   - `timers_fired_total`
   - `timer_deliveries_total`
   - `timer_delivery_errors_total`
   - `timer_delivery_mailbox_full_total`
   - `timer_delivery_target_closed_total`
   - `timer_delivery_runtime_closed_total`
   - `timer_delivery_other_errors_total`
2. 先增加 Timer 正常投递和 Mailbox 满时的指标测试；增加包内表驱动测试覆盖错误分类。
3. 运行：

   ```bash
   go test ./runtime -run 'TestTimerDelivery' -count=1
   ```

   预期：当前实现忽略 `Send` 结果，指标断言失败。
4. 将 Timer callback 改为调用私有 `deliverTimer`。成功和失败都计数；仅未知错误写日志。
5. 运行目标测试 30 次和 race：

   ```bash
   go test ./runtime -run 'TestTimerDelivery' -count=30
   go test -race ./runtime -run 'TestTimerDelivery' -count=1
   ```

6. 提交：`feat(runtime): 增加 Timer 投递结果指标`。

## Task 4：建立性能与资源基线

**文件：** 新增 `runtime/benchmark_test.go`、`runtime/resource_leak_internal_test.go`、`docs/benchmarks/2026-07-17-core-runtime.md`；修改 `docs/rfcs/RFC-0170-Core-Timer.md`、`docs/rfcs/RFC-0240-Tooling-Performance.md`。

1. 增加完整链路 benchmark：
   - `BenchmarkSend`
   - `BenchmarkCallReply`
   - `BenchmarkManyServices`
   - `BenchmarkTimerDelivery`
2. 所有 benchmark 使用 `b.ReportAllocs()`，路径必须经过公开 API、Mailbox、Scheduler 和 Handler；不得直接调用私有处理函数伪造结果。
3. 增加 `TestRuntimeRepeatedCreateCloseReleasesOwnedResources`，循环创建和关闭 Runtime，检查 Registry、PendingCall、Timer、任务表最终为空，并以宽松上限检查 scheduler goroutine 已退出。
4. 将 `RFC-0170` 的 Timer Wheel 改为基准触发的候选优化；在 `RFC-0240` 记录可重复执行的基准命令，不设置尚无数据支撑的硬阈值。
5. 运行：

   ```bash
   go test ./runtime -run 'TestRuntimeRepeatedCreateCloseReleasesOwnedResources' -count=10
   go test -race ./runtime -run 'TestRuntimeRepeatedCreateCloseReleasesOwnedResources' -count=1
   go test ./runtime -run '^$' -bench 'Benchmark(Send|CallReply|ManyServices|TimerDelivery)$' -benchmem -benchtime=100x
   ```

6. 提交：`test(runtime): 建立核心性能与资源基线`。

## Task 5：关闭 P1 待办

**文件：** 修改 `docs/TODO.md`、`docs/rfcs/RFC-0500-Roadmap.md` 和本计划。

1. 将 `CF-001` 至 `CF-004` 标记为已完成，更新日期；路线图不再把 Core 性能基准列为缺口。
2. 把本计划状态改为“已完成”，记录最终验证命令。
3. 执行：

   ```bash
   go test ./...
   go vet ./...
   go test -race ./...
   go run ./examples/local-runtime
   git diff --check
   ```

   预期：全部通过，示例输出 `hello`。
4. 提交：`docs: 完成 Core API 冻结前收口`。
