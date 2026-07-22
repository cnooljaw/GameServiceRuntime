# GSR Phase 7E Supervisor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现一个不侵入 Core、不会在 Service 中阻塞或创建 goroutine、能从已提交 Snapshot 有界恢复新实例的本地 Supervisor。

**Architecture:** Decorator 在 Handler panic 时发送不可变 FailureNotice 并重新 panic；SupervisorService 只在 Mailbox 中串行维护注册、代际、策略和恢复状态；组合根持有的 Runner 用固定 worker 执行退避、Snapshot/Store I/O 和两阶段 Launcher。Launcher 先 Prepare 未发布实例，Supervisor 登记新代际后再 Commit 长期名字；失败或迟到结果统一 Abort。

**Tech Stack:** Go 1.23.3、标准库 `context`/`errors`/`log/slog`/`sync`/`time`、GSR `Service`/`Command`/`Call`/`Send`、Snapshot Manager、TDD、Race Detector、Markdown RFC。

---

## 文件结构

- `docs/rfcs/RFC-0220-Tooling-Supervisor.md`：冻结 Phase 7E owner、策略、两阶段发布、错误和验收契约。
- `tooling/supervisor/types.go`：公开 Key、通知、注册、策略、状态、恢复任务和 seam 类型。
- `tooling/supervisor/errors.go`：稳定 sentinel error。
- `tooling/supervisor/validation.go`：Key、Ref、策略、配置和 nil interface 校验。
- `tooling/supervisor/decorator.go`：保持原 Service 语义并在 Handler panic 时发送通知、重新 panic。
- `tooling/supervisor/commands.go`：包内 CommandID、请求、响应和稳定失败分类。
- `tooling/supervisor/client.go`：注册与状态查询 typed Client。
- `tooling/supervisor/service.go`：Mailbox 内的注册、通知校验、预算、退避和恢复状态机。
- `tooling/supervisor/runner.go`：有界队列、固定 worker、退避等待、结果重试和真实关闭等待。
- `tooling/supervisor/launcher.go`：基于 RuntimeControl、ServiceFactory、BindingPublisher 的两阶段 launcher。
- `tooling/supervisor/*_test.go`：每个公开边界、故障路径、重复与并发测试。
- `examples/supervisor-runtime/main.go`：从已提交 Snapshot 恢复到新 `ServiceRef` 的组合根示例。
- `docs/GSR-Book/04-第四篇-基础设施/02-Supervisor.md`：可读实现说明和时序。
- `README.md`、`CHANGELOG.md`、`docs/TODO.md`、`docs/rfcs/RFC-0500-Roadmap.md`：同步 Phase 7E 状态。

### Task 1：冻结 RFC 与计划

**Files:**

- Modify: `docs/rfcs/RFC-0220-Tooling-Supervisor.md`
- Create: `docs/superpowers/plans/2026-07-22-gsr-phase7e-supervisor.md`

- [x] **Step 1: 裁决五个开放问题**

明确：生命周期错误 owner；Runner 非 Service owner；两阶段 Launcher；失败通知 fail-closed；发布失败 Abort 后重试，Abort 失败终止。

- [x] **Step 2: 冻结额外缺失契约**

补充 `MaxAttempts` 与 `MaxRestarts` 的不同含义、prepared/commit 次序、Runner 结果来源、队列上限、Client 状态查询和精确指标名。

- [x] **Step 3: 执行文档门禁**

Run:

```bash
go test ./runtime -run '^TestRFCMetadataPolicy$' -count=10
git diff --check -- docs/rfcs/RFC-0220-Tooling-Supervisor.md docs/superpowers/plans/2026-07-22-gsr-phase7e-supervisor.md
```

Expected: PASS；差异检查无输出。

- [x] **Step 4: 提交契约**

```bash
git add docs/rfcs/RFC-0220-Tooling-Supervisor.md docs/superpowers/plans/2026-07-22-gsr-phase7e-supervisor.md
git commit -m "docs(supervisor): 冻结故障恢复契约"
```

### Task 2：实现模型、校验与 panic Decorator

**Files:**

- Create: `tooling/supervisor/types.go`
- Create: `tooling/supervisor/errors.go`
- Create: `tooling/supervisor/validation.go`
- Create: `tooling/supervisor/decorator.go`
- Create: `tooling/supervisor/decorator_test.go`
- Create: `tooling/supervisor/validation_test.go`

- [x] **Step 1: 写 Decorator 失败测试**

至少覆盖：

```go
func TestDecoratorReportsHandlerPanicAndRepanics(t *testing.T)
func TestDecoratorPreservesCommandsAndNormalLifecycle(t *testing.T)
func TestDecoratorRecordsDeliveryFailureAndRepanics(t *testing.T)
func TestDecoratorRejectsInvalidConfigAndSupervisorSelfReference(t *testing.T)
```

使用真实 Runtime 验证通知 Envelope Source 等于失败 Service 自身；使用捕获 `ServiceContext` 或小型 fake 验证发送失败指标与日志。正常 `Handle` error 和 Reply 不得转成 FailureNotice。

- [x] **Step 2: 写模型与策略失败测试**

验证空白/非法 UTF-8 Key、零 Ref、未知策略、非恢复策略携带限制、`MaxBackoff < MinBackoff`、nil/dynamic-nil Service。

- [x] **Step 3: 运行并确认失败**

Run: `go test ./tooling/supervisor -run '^Test(Decorator|Validate)' -count=1`

Expected: package 或符号尚不存在，测试失败。

- [x] **Step 4: 写最小实现**

Decorator 的核心 defer：

```go
defer func() {
    recovered := recover()
    if recovered == nil {
        return
    }
    notice := FailureNotice{
        Key: d.config.Key, FailedRef: d.context.Self(),
        Generation: d.config.Generation,
        OccurredAt: d.context.Now(), Kind: FailureHandlerPanic,
    }
    if err := d.context.Send(d.config.Supervisor, failureCommand, notice); err != nil {
        d.context.Metrics().Inc(metricFailureNoticeDeliveryErrors)
        d.context.Logger().Error("supervisor failure notice delivery failed", "error", err)
    }
    panic(recovered)
}()
```

`Commands()` 返回独立副本；`Init` 拒绝 `Self()==Supervisor` 后再委托；`Stop` 和 `Close` 只委托，不产生通知。

- [x] **Step 5: 重复测试并提交**

Run:

```bash
go test ./tooling/supervisor -run '^Test(Decorator|Validate)' -count=100
go test -race ./tooling/supervisor -run '^Test(Decorator|Validate)' -count=20
```

Expected: PASS，Race Detector 无报告。

```bash
git add tooling/supervisor/types.go tooling/supervisor/errors.go tooling/supervisor/validation.go tooling/supervisor/decorator.go tooling/supervisor/decorator_test.go tooling/supervisor/validation_test.go
git commit -m "feat(supervisor): 增加故障通知装饰器"
```

### Task 3：实现 Supervisor Service、Client 与策略状态机

**Files:**

- Create: `tooling/supervisor/commands.go`
- Create: `tooling/supervisor/client.go`
- Create: `tooling/supervisor/service.go`
- Create: `tooling/supervisor/client_test.go`
- Create: `tooling/supervisor/service_test.go`
- Create: `tooling/supervisor/policy_test.go`

- [ ] **Step 1: 写注册和查询失败测试**

覆盖有效注册/查询、重复 Key、错误节点、Supervisor 自身、未知 Key、错误响应类型、nil/dynamic-nil caller，以及返回 `Record` 不泄露包内可变结构。

- [ ] **Step 2: 写通知与策略失败测试**

使用 fake `RecoveryExecutor` 精确断言：

```go
func TestFailureNoticeRequiresExactSourceRefAndGeneration(t *testing.T)
func TestDuplicateAndStaleNoticeDoNotScheduleRecovery(t *testing.T)
func TestRestartNeverAndDestroyHaveDistinctTerminalStates(t *testing.T)
func TestRestartOnFailureSchedulesExponentialBackoff(t *testing.T)
func TestRestartWindowSuppressesRapidRepeatedFailures(t *testing.T)
func TestRecoveryAttemptBudgetStopsContinuousFailures(t *testing.T)
func TestSupervisorCannotRegisterItself(t *testing.T)
```

通过真实 Runtime Send 产生不可伪造 Source；需要精确内部错误时在同包测试直接调用状态转换 helper，但最终行为仍由公开 Client/Runtime 测试复核。

- [ ] **Step 3: 运行并确认失败**

Run: `go test ./tooling/supervisor -run '^Test(Client|Failure|Duplicate|Restart|Recovery|Supervisor)' -count=1`

Expected: Client、Service 和状态机尚不存在，测试失败。

- [ ] **Step 4: 实现 typed 协议和最小状态机**

Command 只在包内暴露，至少包括 register、get、failure notice、prepared、committed、failed。业务错误编码到 response；`CommandContext.Reply` 的基础设施错误直接从 `Handle` 返回。

状态机记录：已提交 Registration、当前状态、active Attempt、fault attempts、窗口内 commit 时间、prepared Ref/Generation。`Submit` 成功才进入 Backoff；队列满进入 `ServiceRecoveryFailed`。退避使用饱和乘法，不能 duration 溢出。

- [ ] **Step 5: 验证指标和重复行为**

用 `Runtime.Inspect().Metrics` 断言八个 Supervisor 指标；同一 notice 重复 100 次只能 Submit 一次。

Run:

```bash
go test ./tooling/supervisor -run '^Test(Client|Failure|Duplicate|Restart|Recovery|Supervisor)' -count=100
go test -race ./tooling/supervisor -run '^Test(Client|Failure|Duplicate|Restart|Recovery|Supervisor)' -count=20
```

- [ ] **Step 6: 提交状态机**

```bash
git add tooling/supervisor/commands.go tooling/supervisor/client.go tooling/supervisor/service.go tooling/supervisor/client_test.go tooling/supervisor/service_test.go tooling/supervisor/policy_test.go
git commit -m "feat(supervisor): 实现有界恢复决策"
```

### Task 4：实现 Runner 和两阶段 Launcher

**Files:**

- Create: `tooling/supervisor/runner.go`
- Create: `tooling/supervisor/launcher.go`
- Create: `tooling/supervisor/runner_test.go`
- Create: `tooling/supervisor/launcher_test.go`

- [ ] **Step 1: 写 Runner 失败测试**

覆盖：固定 worker、非阻塞有界 Submit、队列满、退避取消、AttemptTimeout、prepared/commit 次序、Mailbox full 结果重试、迟到 prepared/commit 自动 Abort、Close 等待真实返回、重复 Close 和 dynamic-nil seam。

关键断言：

```go
func TestRunnerRecordsPreparedBeforeCommit(t *testing.T)
func TestRunnerAbortsWhenPreparedResultIsStale(t *testing.T)
func TestRunnerAbortsPublishFailureBeforeReportingFailure(t *testing.T)
func TestRunnerQueueIsBoundedAndSubmitDoesNotBlock(t *testing.T)
func TestRunnerCloseTimeoutKeepsWaitingForRealLauncherReturn(t *testing.T)
```

- [ ] **Step 2: 写 Runtime launcher 失败测试**

覆盖 Factory error、Snapshot-not-found 分类、Factory 返回命名 ServiceSpec、Decorator 装配、CreateService error、Publish/Withdraw/Stop 次序、Abort 幂等和错误汇总。

- [ ] **Step 3: 运行并确认失败**

Run: `go test ./tooling/supervisor -run '^Test(Runner|RuntimeLauncher)' -count=1`

Expected: Runner 和 launcher 尚不存在，测试失败。

- [ ] **Step 4: 实现 Runner**

`NewRunner` 启动固定 worker；`Submit` 使用带 default 的 channel send；worker 用可取消 Timer 等待 Delay。每次 launcher 调用使用 `AttemptTimeout`，prepared/committed/failed 通过 Call 获取业务确认。只有 `ErrMailboxFull` 可按配置重试；Target/Runtime 关闭立即停止。

`Close(ctx)` 第一次只触发 cancel，不关闭任务 channel，避免并发 Submit 的 send-on-closed；所有调用等待同一个 `done`。ctx 到期返回 cause，worker 真实退出后关闭 `done`，后续调用取得最终结果。

- [ ] **Step 5: 实现 Runtime launcher**

Prepare：Factory Build → 拒绝非空 Name → Decorate → `CreateService`。Commit：可选 Publisher.Publish。Abort：可选 Publisher.Withdraw 后 `Runtime.Stop`；`ErrServiceClosed` 和 `ErrServiceNotFound` 作为已停止收敛处理，其它错误汇总为 `ErrAbortFailed`。

- [ ] **Step 6: 重复、Race 和泄漏测试**

Run:

```bash
go test ./tooling/supervisor -run '^Test(Runner|RuntimeLauncher)' -count=100
go test -race ./tooling/supervisor -run '^Test(Runner|RuntimeLauncher)' -count=20
```

Expected: PASS；队列、worker、Timer 和阻塞 launcher 测试无 Race、无 goroutine 数持续增长。

- [ ] **Step 7: 提交执行层**

```bash
git add tooling/supervisor/runner.go tooling/supervisor/launcher.go tooling/supervisor/runner_test.go tooling/supervisor/launcher_test.go
git commit -m "feat(supervisor): 增加两阶段恢复执行器"
```

### Task 5：打通 Snapshot 恢复纵向切片

**Files:**

- Create: `tooling/supervisor/integration_test.go`
- Create: `examples/supervisor-runtime/main.go`

- [ ] **Step 1: 写端到端失败测试**

场景：创建稳定 Key 的计数 Service → Capture revision 2 → 注册并发布初始实例 → Call 触发 panic → Core 返回 `ErrServiceFailed` 并移除旧 Ref → Factory 从 `snapshot.Manager.Load` 构造新 Service → Runner Prepare/Commit → Client.Get 返回 Generation+1 和新 Ref → 新实例返回 revision 2 的状态。

同时覆盖 Snapshot 不存在、连续 CreateService 失败、Publish 失败后 prepared 实例已停止，以及旧 Generation 通知不会覆盖新实例。

- [ ] **Step 2: 运行并确认失败**

Run: `go test ./tooling/supervisor -run '^TestSupervisorSnapshotRecovery' -count=1`

Expected: 纵向装配或边界行为尚不完整，测试失败。

- [ ] **Step 3: 完成最小装配与示例**

Factory 内显式把 `ServiceKey` 转成 `snapshot.Key`；`snapshot.ErrSnapshotNotFound` 用 `errors.Join(supervisor.ErrSnapshotNotFound, err)` 保留双方 `errors.Is`。示例组合根显式持有 Runtime、Runner、Snapshot Manager 和 Launcher，不让业务 Service 获取这些对象。

- [ ] **Step 4: 验证端到端重复与示例**

Run:

```bash
go test ./tooling/supervisor -run '^TestSupervisorSnapshotRecovery' -count=100
go test -race ./tooling/supervisor -run '^TestSupervisorSnapshotRecovery' -count=20
go run ./examples/supervisor-runtime
```

Expected: PASS；示例输出旧/新 Ref、Generation `1 -> 2` 和恢复后的 revision/value。

- [ ] **Step 5: 提交纵向切片**

```bash
git add tooling/supervisor/integration_test.go examples/supervisor-runtime/main.go
git commit -m "feat(supervisor): 打通快照故障恢复"
```

### Task 6：同步文档、执行双轴 Review 并验收阶段

**Files:**

- Modify: `docs/GSR-Book/04-第四篇-基础设施/02-Supervisor.md`
- Modify: `docs/rfcs/RFC-0220-Tooling-Supervisor.md`
- Modify: `docs/rfcs/RFC-0500-Roadmap.md`
- Modify: `docs/TODO.md`
- Modify: `README.md`
- Modify: `CHANGELOG.md`
- Modify: `docs/superpowers/plans/2026-07-22-gsr-phase7e-supervisor.md`

- [ ] **Step 1: 同步最终公开行为**

GSR Book 解释 owner、时序、策略和组合根；README/CHANGELOG 只宣称实际通过验收的能力；Roadmap/TODO 把 Phase 7E 标记完成并指出下一阶段。RFC 通过 Review 后改为 `已接受` 并增加 `接受日期：2026-07-22`。

- [ ] **Step 2: 执行双轴 Review**

Standards 轴：模块深度、依赖方向、Service 无 goroutine、Runner 生命周期、队列上限、错误不丢失、nil/dynamic-nil、复制、日志 secret、公共 Go doc。

Spec 轴：逐条核对 RFC-0220 的 12 条验收、两阶段发布、尝试/窗口、Source/Generation、Snapshot 和 fail-closed。发现问题先写失败测试再修复。

- [ ] **Step 3: 运行静态和文档门禁**

```bash
gofmt -w tooling/supervisor examples/supervisor-runtime
git diff --check
go test ./runtime -run '^Test(ProjectServicesDoNotStartGoroutines|RFCMetadataPolicy)$' -count=10
go vet ./...
```

- [ ] **Step 4: 运行完整、重复和 Race 验收**

```bash
go test ./... -count=1
go test ./tooling/supervisor -count=100
go test -race ./... -count=1
go run ./examples/supervisor-runtime
```

Expected: 全部 PASS；Race Detector 无报告；示例正常退出。

- [ ] **Step 5: CodeGraph 复核依赖**

```bash
codegraph sync
codegraph callers Decorate -l 50
codegraph callers NewRunner -l 50
codegraph impact ServiceContext -d 3
```

确认 Core 没有反向依赖 `tooling/supervisor`，普通业务 Service 不持有 Launcher/Runner/Runtime 指针。

- [ ] **Step 6: 自审计划完整性**

检查：无 TODO placeholder；文档符号与代码完全一致；所有新导出符号有 Go doc；所有 RFC 验收映射到测试；每个命令可从仓库根执行；计划 checkbox 和 Review 结论更新。

- [ ] **Step 7: 提交阶段验收**

```bash
git add docs/GSR-Book/04-第四篇-基础设施/02-Supervisor.md docs/rfcs/RFC-0220-Tooling-Supervisor.md docs/rfcs/RFC-0500-Roadmap.md docs/TODO.md README.md CHANGELOG.md docs/superpowers/plans/2026-07-22-gsr-phase7e-supervisor.md
git commit -m "docs(supervisor): 完成 Phase 7E 验收"
```
