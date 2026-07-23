# Phase 10C2A Node Stop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在已有 `ReadyToStop` Drain Operation 后，以 Gateway 授权、NodeAgent receipt 和组合根 Runner 安全执行本地 `Runtime.Stop`。

**Architecture:** Coordinator 继续是全局 Operation owner，Gateway 只以 Principal 调用它；NodeAgent 只接受精确 Coordinator 的本地 Stop 指令并保存 receipt。组合根持有固定上限 `NodeStopRunner`，它在每次 Runtime.Stop 前重读 Directory，并以私有 Command 回写 NodeAgent；Gateway 通过 Resolve 拉取事实，不存在后台收敛。

**Tech Stack:** Go 1.23 标准库、GSR Runtime、`tooling/control`、`tooling/servicegroup`、现有 TCP ClusterCodec。

---

## 文件结构

- 新建 `tooling/control/stop_types.go`：StopOperation、NodeStop receipt、Runner 的公开值类型和窄接口。
- 新建 `tooling/control/stop_runner.go`：组合根持有的有界 worker pool、Directory 再确认、真实 Runtime.Stop 和 Close。
- 新建 `tooling/control/stop_runner_test.go`：Runner 提交、Directory 竞争、Stop 返回与关闭测试。
- 修改 `tooling/control/types.go`、`node_agent.go`：可选 StopCoordinator/Executor，NodeAgent 的 Begin、receipt 和私有结果 Command。
- 修改 `tooling/control/drain_service.go`、`drain_types.go`、`drain_validation.go`、`drain_copy.go`：Coordinator StopOperation 状态机和 Client。
- 修改 `tooling/control/commands.go`、`codec.go`、`errors.go`、`service.go`：Command、wire payload、稳定错误和 Codec。
- 新建 `tooling/control/stop_test.go`、`stop_remote_test.go`：本地完整切片、未知结果和双节点 TCP。
- 修改 `docs/rfcs/RFC-0273-Tooling-Node-Stop-Execution.md`、README、CHANGELOG、TODO、Roadmap、GSR Book：验收状态与边界。

### Task 1: 冻结并提交 Stop 执行契约

**Files:**
- Create: `docs/rfcs/RFC-0273-Tooling-Node-Stop-Execution.md`
- Create: `docs/superpowers/plans/2026-07-23-gsr-phase10c2a-node-stop.md`
- Modify: `docs/SUMMARY.md`, `docs/DECISIONS.md`, `docs/rfcs/RFC-0500-Roadmap.md`

- [ ] **Step 1: 写入 Coordinator、NodeAgent、Runner 的状态 owner、Source 授权和部署顺序。**

契约必须固定以下边界：Coordinator 不持有 Runtime；NodeAgent 不在 Handler 直接 Stop；Runner 仅由组合根 Close；Stop 前先在 Coordinator 和 Runner 两处读取同一 Published ServiceSet；Runner 不能从 worker 使用保存的 ServiceContext。

- [ ] **Step 2: 写入 RequestID、receipt、未知提交和失败终态。**

```go
type StopTargetState string

const (
    StopTargetPending StopTargetState = "pending"
    StopTargetQueued StopTargetState = "queued"
    StopTargetStopped StopTargetState = "stopped"
    StopTargetSuperseded StopTargetState = "superseded"
    StopTargetFailed StopTargetState = "failed"
)
```

要求同 RequestID+Target 的 NodeAgent Begin 幂等；Directory 读取失败回到 Pending；Directory 内容不同为 Superseded 且不调用 Runtime.Stop；任何 Runtime.Stop 错误为终态 Failed。

- [ ] **Step 3: 运行文档一致性测试并提交契约。**

Run: `go test ./...`

Expected: PASS；RFC 元数据状态为 `待实现`，代码尚未使用新类型。

```bash
git add docs
git commit -m 'docs(control): 冻结 Phase 10C2A Node Stop 契约'
```

### Task 2: 先写 NodeStopRunner 失败测试

**Files:**
- Create: `tooling/control/stop_runner_test.go`
- Create: `tooling/control/stop_types.go`

- [ ] **Step 1: 写 Runner 配置、队列满、Close 和真实返回测试。**

```go
func TestNodeStopRunnerCloseWaitsForStartedRuntimeStop(t *testing.T) {
    runtime := &blockingStopRuntime{entered: make(chan struct{}), release: make(chan struct{})}
    runner, err := NewNodeStopRunner(runtime, validRunnerConfig())
    if err != nil { t.Fatal(err) }
    if err := runner.Submit(validNodeStopTask()); err != nil { t.Fatal(err) }
    <-runtime.entered
    closed := make(chan error, 1)
    go func() { closed <- runner.Close(context.Background()) }()
    select { case <-closed: t.Fatal("Close returned before Stop") ; default: }
    close(runtime.release)
    if err := <-closed; err != nil { t.Fatal(err) }
}
```

同时覆盖 `Workers<=0`、`QueueSize<=0`、无效 Directory、负 timeout、Submit 无效任务、队列满和 Close 后 `ErrNodeStopRunnerClosed`。

- [ ] **Step 2: 写 Directory 再确认与结果投递失败测试。**

```go
func TestNodeStopRunnerDoesNotStopWhenDirectoryChanged(t *testing.T) {
    runner := newRunnerWithDirectory(t, publishedSet)
    if err := runner.Submit(NodeStopTask{Agent: agent, RequestID: "stop-1", Target: old, Group: group, Published: publishedSet}); err != nil { t.Fatal(err) }
    receipt := waitNodeStopResult(t, runtime, agent)
    if receipt.State != StopTargetSuperseded { t.Fatalf("receipt = %#v", receipt) }
    if runtime.stopCalls.Load() != 0 { t.Fatal("Runtime.Stop was called") }
}
```

再覆盖 Directory Call timeout 产生 Pending+`StopFailureDirectoryUnavailable`，`ErrServiceClosed`/`ErrServiceNotFound` 产生 Stopped，普通 Stop error 产生 Failed+`StopFailureRuntimeStop`。

- [ ] **Step 3: 运行并确认编译失败。**

Run: `go test ./tooling/control -run NodeStopRunner -count=1`

Expected: FAIL，缺少 `NodeStopRunner`、`NodeStopTask` 与稳定 Stop 类型。

### Task 3: 实现有界 NodeStopRunner

**Files:**
- Create: `tooling/control/stop_types.go`, `tooling/control/stop_runner.go`
- Modify: `tooling/control/commands.go`, `tooling/control/errors.go`

- [ ] **Step 1: 定义窄 Runtime 与执行接口。**

```go
type NodeStopRuntime interface {
    Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error)
    Send(gsr.ServiceRef, gsr.CommandID, any) error
    Stop(context.Context, gsr.ServiceRef) error
}

type NodeStopExecutor interface {
    Submit(NodeStopTask) error
}
```

`NodeStopRunner` 私有地创建 `servicegroup.Client`；它只接受 Target.Node==Agent.Node、非零 RequestID、合法 Group 和完整 Published snapshot 的任务。

- [ ] **Step 2: 实现固定 worker、强读取和结果 Command。**

```go
func (r *NodeStopRunner) execute(task NodeStopTask) {
    current, err := r.directory.Get(callContext, task.Group)
    switch {
    case err != nil:
        r.sendResult(task, StopTargetPending, StopFailureDirectoryUnavailable)
    case !sameServiceSet(current, task.Published):
        r.sendResult(task, StopTargetSuperseded, StopFailureNone)
    default:
        r.stopAndSendResult(task)
    }
}
```

`stopAndSendResult` 用 Runner context 的 `StopTimeout` 调用 Runtime.Stop；`ErrServiceClosed` 与 `ErrServiceNotFound` 归一为 Stopped。worker 只处理 Runner 自己的 queue；`Close` 取消未开始任务、等待已开始 Stop 的 worker 返回，并拒绝新的 Submit。

- [ ] **Step 3: 运行 Runner 测试。**

Run: `go test ./tooling/control -run NodeStopRunner -count=1`

Expected: PASS；无 ServiceContext 从 worker 使用。

- [ ] **Step 4: 提交 Runner。**

```bash
git add tooling/control/stop_types.go tooling/control/stop_runner.go tooling/control/stop_runner_test.go tooling/control/commands.go tooling/control/errors.go
git commit -m 'feat(control): 增加有界 Node Stop Runner'
```

### Task 4: 写 NodeAgent Stop receipt 的失败测试

**Files:**
- Modify: `tooling/control/node_agent_test.go`, `tooling/control/codec_test.go`

- [ ] **Step 1: 写配置和精确 Source 拒绝测试。**

```go
func TestNodeAgentRejectsStopWithoutExactCoordinator(t *testing.T) {
    agent := newStopEnabledNodeAgent(t, coordinator, executor)
    if _, err := nodeCall(agent, otherService, beginNodeStopRequest{Task: validNodeStopTask()}); !errors.Is(err, ErrUnauthorized) {
        t.Fatalf("Begin(other source) error = %v", err)
    }
    if executor.submits != 0 { t.Fatal("unauthorized request reached executor") }
}
```

覆盖仅设置 StopCoordinator 或 StopExecutor 的无效 Config、未启用 Agent 的 `ErrStopDisabled`、跨节点 Target、自身 Target、错误 Published、重复 RequestID 不同内容和私有结果 Command 的非本地 Runtime source。

- [ ] **Step 2: 写 receipt 的幂等与 Runner 回写测试。**

```go
func TestNodeAgentReturnsSameQueuedReceiptAndAppliesRunnerResult(t *testing.T) {
    first := beginNodeStop(t, agent, task)
    second := beginNodeStop(t, agent, task)
    if first != second || executor.submits != 1 { t.Fatalf("duplicate = %#v, submits=%d", second, executor.submits) }
    sendPrivateResult(t, agent, task, StopTargetStopped, StopFailureNone)
    receipt := getNodeStopReceipt(t, agent, task)
    if receipt.State != StopTargetStopped { t.Fatalf("receipt = %#v", receipt) }
}
```

- [ ] **Step 3: 运行并确认编译失败。**

Run: `go test ./tooling/control -run NodeAgent.*Stop -count=1`

Expected: FAIL，缺少 BeginNodeStop、receipt 和 StopExecutor。

### Task 5: 实现 NodeAgent Stop receipt 和 Codec

**Files:**
- Modify: `tooling/control/types.go`, `node_agent.go`, `commands.go`, `codec.go`, `copy.go`, `validation.go`, `errors.go`, `service.go`

- [ ] **Step 1: 扩展 NodeAgentConfig 和私有状态。**

```go
type nodeAgent struct {
    // existing lease fields
    stopExecutor NodeStopExecutor
    stopRecords map[nodeStopKey]NodeStopReceipt
}
```

在 Init 初始化 receipt map；Begin 在保存 Pending/Queued receipt 后才 Reply；`RecordNodeStopResult` 必须只修改现有且相同 Task 的 receipt，迟到或冲突结果不得覆盖。

- [ ] **Step 2: 扩展 Codec。**

`BeginNodeStop`、`GetNodeStopReceipt` 及 Coordinator Stop Command 有精确 request/response prototype；`RecordNodeStopResult` 返回 `ErrUnsupportedCommand`。成功 receipt 必须校验 Target/Agent Node、状态/失败组合和非零时间。

- [ ] **Step 3: 运行 NodeAgent 与 Codec 测试。**

Run: `go test ./tooling/control -run 'NodeAgent.*Stop|Codec' -count=1`

Expected: PASS。

### Task 6: 写 Coordinator Stop Operation 的失败测试

**Files:**
- Create: `tooling/control/stop_test.go`
- Modify: `tooling/control/drain_test.go`

- [ ] **Step 1: 写 Ready、owner、Target 对和 Directory 拒绝测试。**

```go
func TestDrainCoordinatorBeginStopRequiresReadyPublishedAndExactTargets(t *testing.T) {
    fixture := newStopFixture(t)
    if _, err := fixture.client.BeginStop(ctx, BeginStopRequest{RequestID: fixture.requestID, Principal: "ops", Targets: fixture.targets}); !errors.Is(err, ErrStopNotReady) {
        t.Fatalf("BeginStop(before Ready) error = %v", err)
    }
    fixture.makeDrainReady(t)
    wrong := fixture.targets[:1]
    if _, err := fixture.client.BeginStop(ctx, BeginStopRequest{RequestID: fixture.requestID, Principal: "ops", Targets: wrong}); !errors.Is(err, ErrStopTargetMismatch) {
        t.Fatalf("BeginStop(missing target) error = %v", err)
    }
}
```

覆盖 Gateway/Principal 拒绝、其他 Principal owner 拒绝、同 RequestID 的不同 Agent `ErrStopRequestConflict`、Directory 已改变时 Superseded 且 executor 不 Submit。

- [ ] **Step 2: 写 Pending/Queued/Stopped 收敛和部分 Stop Superseded 测试。**

```go
func TestDrainCoordinatorResolveStopPullsReceiptsWithoutBackgroundRetry(t *testing.T) {
    fixture := newStopFixture(t)
    fixture.makeDrainReady(t)
    operation := fixture.beginStop(t)
    if operation.Phase != StopWaiting { t.Fatalf("BeginStop() = %#v", operation) }
    fixture.completeFirstTarget(t)
    completed := fixture.resolveStop(t)
    if completed.Phase != StopCompleted { t.Fatalf("ResolveStop() = %#v", completed) }
}
```

再覆盖 Agent Begin Reply 丢失、Get receipt Reply 丢失、QueueFull 后再次 Resolve、Runtime Stop 失败为 StopFailed，以及已停止第一 Target 后 Directory 改变时不提交第二 Target。

- [ ] **Step 3: 运行并确认编译失败。**

Run: `go test ./tooling/control -run 'DrainCoordinator.*Stop' -count=1`

Expected: FAIL，缺少 BeginStop、StopOperation 和 Coordinator 状态机。

### Task 7: 实现 Coordinator Stop Operation 与 Client

**Files:**
- Modify: `tooling/control/drain_types.go`, `drain_service.go`, `drain_validation.go`, `drain_copy.go`, `commands.go`, `codec.go`, `errors.go`, `service.go`

- [ ] **Step 1: 记录规范化 Target/Agent 对和 BeginStop。**

```go
func (s *drainCoordinatorService) beginStop(record *stopOperationRecord) {
    if !s.directoryStillPublishedForStop(record) {
        s.setStopPhase(record, StopSuperseded)
        return
    }
    for index := range record.operation.Targets {
        if !s.directoryStillPublishedForStop(record) { s.setStopPhase(record, StopSuperseded); return }
        s.beginNodeStop(record, index)
    }
}
```

创建前要求同 RequestID 的 DrainOperation 为 ReadyToStop；BeginNodeStop 只由 Coordinator 的 ServiceContext.Call 发送。Call 超时不标记成功，保持 Pending，Resolve 先 Get receipt 再安全重复 Begin。

- [ ] **Step 2: 实现 ResolveStop 和终态归约。**

```go
func stopPhaseFor(targets []StopTarget) StopPhase {
    if hasSuperseded(targets) { return StopSuperseded }
    if allStopped(targets) { return StopCompleted }
    if allTerminal(targets) && hasFailed(targets) { return StopFailed }
    return StopWaiting
}
```

Resolve 在每轮先确认 Directory；随后只读 receipt，Pending 才重发幂等 Begin。Completed、Failed 与 Superseded 不再 Call Agent 或 Runner。

- [ ] **Step 3: 运行本地 Stop 测试。**

Run: `go test ./tooling/control -run 'DrainCoordinator.*Stop|NodeAgent.*Stop|NodeStopRunner' -count=1`

Expected: PASS。

- [ ] **Step 4: 提交 Stop 闭环。**

```bash
git add tooling/control
git commit -m 'feat(control): 增加受控 Node Stop 执行'
```

### Task 8: 增加 TCP、竞态和文档验收

**Files:**
- Create: `tooling/control/stop_remote_test.go`
- Modify: `README.md`, `CHANGELOG.md`, `docs/TODO.md`, `docs/rfcs/RFC-0273-Tooling-Node-Stop-Execution.md`, `docs/rfcs/RFC-0500-Roadmap.md`, `docs/GSR-Book/03-第三篇-Cluster/05-ServiceGroup.md`

- [ ] **Step 1: 写双节点 TCP Gateway → Coordinator → remote NodeAgent → local Runner 测试。**

测试必须让 node-a 的 Gateway 远程 BeginStop node-b 的 Target；Runner 只在 node-b 调用 node-b Runtime.Stop。节点级 caller、其它 Service 和错误 Coordinator source 必须被拒绝；私有结果 Command 的 Codec 必须拒绝。

- [ ] **Step 2: 运行定向竞态与关闭测试。**

Run: `go test -race ./tooling/control -run 'NodeStopRunner|NodeAgent.*Stop|DrainCoordinator.*Stop' -count=20`

Expected: PASS；Runner Close 没有 worker 泄漏，Service 没有直接 goroutine。

- [ ] **Step 3: 回填已接受 RFC 和读者文档。**

将 RFC-0273 标为 `已接受` 并写入接受日期。README 与 CHANGELOG 必须说明实际业务作用是“安全停止已 Drain 的旧实例”，不等于 Node 进程退出或自动恢复；路线图下一阶段只剩 C2B 人工恢复与补偿。

- [ ] **Step 4: 运行全量门禁和示例。**

Run: `go test ./... && go vet ./... && go test -race ./... && go run ./examples/drain-runtime`

Expected: PASS；既有 Visitor 示例仍只演示 lease，不触发 Stop。

- [ ] **Step 5: 提交验收文档。**

```bash
git add README.md CHANGELOG.md docs
git commit -m 'docs(control): 完成 Phase 10C2A Node Stop 验收'
```

## 自检

- Runner 是唯一调用 Runtime.Stop 的对象，且由组合根 Close；NodeAgent、Coordinator、Gateway 和业务 Service 都没有 Runtime 指针。
- Coordinator 和 Runner 都以完整 Published ServiceSet 确认 Directory；任一不确定性均不执行新的 Stop。
- 每个下游未知结果都有 RequestID 幂等 Begin 或 receipt Get 的显式 Resolve 路径；没有 Timer、Service goroutine 或自动补偿。
- C2A 不创建新实例、不重新接流被 Guard 的 Ref；C2B 才处理人工恢复与补偿。
