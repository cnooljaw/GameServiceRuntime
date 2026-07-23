# Phase 10C1：受控 Drain 操作实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 Control Plane 中交付带授权、RequestID、审计和人工 Resolve 的 Drain 操作，并安全推进到 `ReadyToStop`。

**Architecture:** `DrainCoordinatorService` 是 Operation/Audit 的单一 Mailbox owner，并通过现有 typed Client 消费 Directory、Guard 和 Visitor facts。它不执行 Stop 或后台 Reconcile；每次外部不确定性都保留为可查询的状态，只有 Gateway Service 可代表已认证 Principal 发起或读取操作。

**Tech Stack:** Go 1.23 标准库、GSR Runtime、`tooling/control`、`tooling/drain`、`tooling/servicegroup`、TCP Cluster tests。

---

## 文件结构

- 新建 `tooling/control/drain_types.go`：Principal、RequestID、Operation、审计和 Config 的公开值类型。
- 新建 `tooling/control/drain_service.go`：Coordinator 的 Mailbox 状态机和下游 Call 编排。
- 新建 `tooling/control/drain_client.go`：Gateway 使用的类型化 Client。
- 修改 `tooling/control/commands.go`、`codec.go`、`errors.go`、`validation.go`：公开 Command、wire payload、错误码、深复制和 Codec 分派。
- 新建 `tooling/control/drain_test.go` 与 `tooling/control/drain_remote_test.go`：本地、失败、Codec 与 TCP 验收。
- 修改 `docs/rfcs/RFC-0272-Tooling-Controlled-Drain-Operation.md`、README、Roadmap、TODO、GSR Book、CHANGELOG：回填已实现状态与边界。

### Task 1: 冻结并提交可实现契约

**Files:**
- Create: `docs/rfcs/RFC-0272-Tooling-Controlled-Drain-Operation.md`
- Create: `docs/superpowers/plans/2026-07-23-gsr-phase10c1-controlled-drain.md`
- Modify: `docs/SUMMARY.md`, `docs/DECISIONS.md`, `docs/rfcs/RFC-0000-Foundation-Glossary.md`, `docs/rfcs/RFC-0260-Tooling-ServiceGroup-Routing.md`, `docs/rfcs/RFC-0270-Tooling-Drain-Hot-Reload.md`, `docs/rfcs/RFC-0500-Roadmap.md`

- [x] **Step 1: 写入 RFC 的 Phase、Operation owner、Gateway+Principal 授权、RequestID 幂等、未知发布、不可逆 Guard、Visitor 和 ReadyToStop 语义。**

- [x] **Step 2: 运行文档一致性测试。**

Run: `go test ./...`

Expected: PASS；代码尚未使用 RFC-0272 的公开类型。

- [x] **Step 3: 提交契约。**

```bash
git add docs
git commit -m 'docs(control): 冻结 Phase 10C1 受控 Drain 操作契约'
```

### Task 2: 写 Coordinator 的失败测试

**Files:**
- Create: `tooling/control/drain_test.go`
- Modify: `tooling/control/codec_test.go`

- [x] **Step 1: 写配置、Gateway source、Principal、RequestID 和所有权失败测试。**

```go
func TestDrainCoordinatorRejectsUnauthorizedRequestWithoutDownstreamCall(t *testing.T) {
    operation, err := client.Start(ctx, StartDrainRequest{RequestID: "op-1", Principal: "ops", /* valid group/version */})
    if !errors.Is(err, ErrUnauthorized) { t.Fatalf("Start() error = %v", err) }
    if operation != (DrainOperation{}) { t.Fatalf("unexpected operation %#v", operation) }
}
```

- [x] **Step 2: 写成功 Publish、幂等重复 Start、PublishUnknown、Guard 重试、强/弱 Visitor 与 Superseded 测试。**

```go
func TestDrainCoordinatorResolvePublishesGuardsAndWaitsForStrongVisitors(t *testing.T) {
    op, err := gateway.Start(ctx, request)
    if err != nil || op.Phase != DrainWaitingVisitors { t.Fatalf("Start() = %#v, %v", op, err) }
    op, err = gateway.Resolve(ctx, request.RequestID, request.Principal)
    if err != nil || op.Phase != DrainReadyToStop { t.Fatalf("Resolve() = %#v, %v", op, err) }
}
```

- [x] **Step 3: 运行并确认编译失败。**

Run: `go test ./tooling/control -run DrainCoordinator -count=1`

Expected: FAIL，缺少 `DrainCoordinatorService`、`DrainClient` 和 Phase 10C1 类型。

### Task 3: 实现公开类型、Client、Codec 与最小状态机

**Files:**
- Create: `tooling/control/drain_types.go`, `tooling/control/drain_client.go`, `tooling/control/drain_service.go`
- Modify: `tooling/control/commands.go`, `tooling/control/errors.go`, `tooling/control/validation.go`, `tooling/control/codec.go`

- [x] **Step 1: 定义并验证 Principal、RequestID、Config、Operation、Audit 和 wire 副本。**

```go
type DrainCoordinatorConfig struct {
    Gateway gsr.ServiceRef
    AllowedPrincipals []Principal
    Directory gsr.ServiceRef
    VisitorRegistry gsr.ServiceRef
    CallTimeout time.Duration
    AuditLimit int
}
```

- [x] **Step 2: 实现 Start 和 Resolve 的 Mailbox 状态机。**

```go
switch operation.Phase {
case DrainPreparing:
    return s.prepareAndPublish(operation)
case DrainPublishUnknown:
    return s.confirmPublished(operation)
case DrainGuarding, DrainWaitingVisitors:
    return s.confirmPublishedGuardsAndVisitors(operation)
}
```

`prepareAndPublish` 只用 Expected CAS；`confirmPublished` 只 Get，不重发未知 Publish；Guard 通过 Status/幂等 Begin 确认；ReadyToStop 不调用 Runtime.Stop。

- [x] **Step 3: 扩展 Control Codec，使用 CommandID 分派 Start、Resolve、Get、ListAudit 的请求和响应。**

- [x] **Step 4: 运行本地测试。**

Run: `go test ./tooling/control -run DrainCoordinator -count=1`

Expected: PASS。

- [x] **Step 5: 提交最小操作闭环。**

```bash
git add tooling/control
git commit -m 'feat(control): 增加受控 Drain 操作记录'
```

### Task 4: 增加双节点 TCP、审查与文档收口

**Files:**
- Create: `tooling/control/drain_remote_test.go`
- Modify: `README.md`, `CHANGELOG.md`, `docs/TODO.md`, `docs/rfcs/RFC-0272-Tooling-Controlled-Drain-Operation.md`, `docs/rfcs/RFC-0500-Roadmap.md`, `docs/GSR-Book/03-第三篇-Cluster/05-ServiceGroup.md`

- [x] **Step 1: 写 TCP Gateway Service 通过 Control Codec 远程 Start/Resolve 的测试；节点级 caller 必须得到 ErrUnauthorized。**

- [x] **Step 2: 运行定向竞态测试。**

Run: `go test -race ./tooling/control -run DrainCoordinator -count=20`

Expected: PASS，无数据竞争、无 goroutine 泄漏。

- [x] **Step 3: 以实现结果更新 RFC 为“已接受”，说明 ReadyToStop 的业务价值、不会 Stop 的边界和下一阶段 NodeAgent 依赖。**

- [x] **Step 4: 运行全量门禁和示例。**

Run: `go test ./... && go vet ./... && go test -race ./... && go run ./examples/drain-runtime`

Expected: PASS；现有 Visitor 示例仍只演示 lease 事实。

- [x] **Step 5: 提交验收文档。**

```bash
git add README.md CHANGELOG.md docs
git commit -m 'docs(control): 完成 Phase 10C1 Drain 操作验收'
```

## 自检

- RFC 的每项 Phase 10C1 行为分别由 Task 2 的本地失败测试、Task 3 的实现和 Task 4 的 TCP/竞态验证覆盖。
- Plan 没有让 Coordinator 修改 Directory、Guard 或 Visitor 的内部状态；它只通过现有 Client 调用公开 Command。
- `Runtime.Stop`、NodeAgent 动作、Desired State 与后台 Reconcile 没有任何实现任务，仍属于下一份契约。
