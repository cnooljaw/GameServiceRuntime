# GSR Core Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现可测试、可执行的单节点 Go Service Runtime，并建立 GSR 的工程约束和项目 Skill。

**Architecture:** 先冻结工程规则，再实现一条完整本地消息链路：`CreateService -> Send -> Mailbox -> Scheduler -> Handle`。`Call/Reply`、Timer 和生命周期只在这条链路稳定后接入。公开 API 位于 `runtime` 目录、Go package 名为 `gsr`；Registry、Mailbox、ReadyQueue、Pending Call 必须保持私有。

**Tech Stack:** Go 1.23.3、标准库、`go test`、`go vet`。

**Module:** `github.com/lijiawang/GameServiceRuntime`。若正式远程仓库地址不同，只在执行 Task 2 前修改 `go.mod` 的这一行。

---

## 范围

本计划实现 RFC-0100、0110、0120、0130、0140、0150、0160、0170、0180。Cluster、Tooling、Login 和 Business Layer 留给后续独立计划，避免外层概念污染 Core Runtime。

目标结构：

```text
AGENTS.md
go.mod
skills/gsr-runtime/SKILL.md
runtime/{errors,types,service,runtime,registry,mailbox,scheduler,call,timer,lifecycle}.go
runtime/{types,runtime,call,timer,lifecycle}_test.go
examples/local-runtime/main.go
```

## Task 1: 工程规则与项目 Skill

**Files:** Create `AGENTS.md`; create `skills/gsr-runtime/SKILL.md`; delete `skills/gsr-runtime-skill.md`.

- [ ] **Step 1: 写 `AGENTS.md`**

它必须包含下列可执行规则：

```text
设计和公开 API 以 docs/rfcs 为准，索引为 docs/SUMMARY.md。
Core Runtime 不得引用 game、examples 或业务领域类型。
使用 Go 1.23.3 和标准库；新增第三方依赖须先说明必要性。
每个可见行为先写失败测试；提交前执行 go test ./...、go vet ./...、go test -race ./...。
只使用 Service、ServiceRef、Command、Send、Call 命名；禁止 Actor、Spawn、Ask、Tell、PTYPE。
Service 状态只能在 Mailbox 消费的 handler 内修改；Service 只能通过 ServiceRef 和 Command 通信。
Timer 只能投递 Command；Session 只关联 Call/Reply；业务幂等使用 RequestID。
实现或评审 Runtime 前读取 skills/gsr-runtime/SKILL.md。
RFC/README 使用 technical-writing；模块边界使用 deep-module-design；计划使用 create-plan；项目 Skill 使用 skill-creator；评审使用 clean-coder-review 或 two-axis-code-review。
每个通过测试的垂直切片单独中文提交；不提交 .DS_Store、构建产物和密钥。
```

- [ ] **Step 2: 写 `skills/gsr-runtime/SKILL.md`**

```md
---
name: gsr-runtime
description: 当实现、评审或修改 GSR Go Service Runtime、Service、Command、Mailbox、Scheduler、Timer、Cluster 边界或相关 RFC 时使用。确保实现遵守 GSR 的 Service 模型和 RFC 约束。
---

# GSR Runtime

1. 先读 `docs/SUMMARY.md`，再读本次修改对应 RFC。
2. Core Runtime 只理解 Service、ServiceRef、Command、Envelope、Mailbox、Scheduler、Timer、生命周期和 Cluster 数据面。
3. 不引入 Actor、PTYPE、跨 Service 指针、业务领域类型或 Timer 回调改状态。
4. Service 内状态由 Mailbox 串行 handler 写入；跨 Service 使用 Send 或 Call。
5. 先写失败测试；完成后执行 `go test ./...`、`go vet ./...` 和 `go test -race ./...`。
6. 新增导出 API 时，同时更新对应 RFC 或说明 RFC 无需变化的原因。
```

- [ ] **Step 3: 验证并提交**

Run: `test -f AGENTS.md && test -f skills/gsr-runtime/SKILL.md && test ! -f skills/gsr-runtime-skill.md && rg -n "ServiceRef|Mailbox|Timer|technical-writing" AGENTS.md skills/gsr-runtime/SKILL.md`

Expected: 退出码 `0`。随后执行 `git add AGENTS.md skills && git commit -m "chore: 建立 GSR 工程约束与项目技能"`。

## Task 2: 核心类型、错误和模块

**Files:** Create `go.mod`, `runtime/types.go`, `runtime/errors.go`, `runtime/types_test.go`.

- [ ] **Step 1: 写失败测试**

```go
func TestServiceRefIsComparable(t *testing.T) {
    ref := gsr.ServiceRef{Node: "local", ID: 7}
    if got := map[gsr.ServiceRef]string{ref: "service"}[ref]; got != "service" {
        t.Fatalf("lookup = %q", got)
    }
}

func TestCommandPreservesPayload(t *testing.T) {
    cmd := gsr.Command{ID: 1001, Payload: struct{ Value int }{42}}
    if got := cmd.Payload.(struct{ Value int }).Value; got != 42 {
        t.Fatalf("payload = %d", got)
    }
}
```

- [ ] **Step 2: 运行失败测试**

Run: `go test ./runtime -run 'TestServiceRef|TestCommand' -count=1`

Expected: FAIL，原因是模块或类型尚不存在。

- [ ] **Step 3: 实现最小公共类型**

```go
package gsr

type NodeID string
type ServiceID uint64
type ServiceName string
type CommandID uint32
type SessionID uint64
type TimerID uint64

type ServiceRef struct { Node NodeID; ID ServiceID }
type Command struct { ID CommandID; Payload any }
type Envelope struct { Source, Target ServiceRef; Session SessionID; Command CommandID; Payload any }
```

`errors.go` 只定义 `ErrTimeout`、`ErrReplyTwice`、`ErrServiceNotFound`、`ErrServiceClosed`、`ErrMailboxFull`、`ErrInvalidServiceSpec`。`go.mod` 为 `module github.com/lijiawang/GameServiceRuntime` 和 `go 1.23.3`。

- [ ] **Step 4: 验证并提交**

Run: `gofmt -w runtime && go test ./runtime -run 'TestServiceRef|TestCommand' -count=1 && go vet ./runtime`

Expected: PASS 且没有 vet 输出。提交：`git add go.mod runtime && git commit -m "feat(runtime): 定义核心地址与消息类型"`。

## Task 3: Service、Registry、Mailbox、Scheduler 与 Send

**Files:** Create `runtime/service.go`, `runtime/registry.go`, `runtime/mailbox.go`, `runtime/scheduler.go`, `runtime/runtime.go`, `runtime/runtime_test.go`.

- [ ] **Step 1: 写 Send 行为测试**

```go
func TestSendDeliversCommandThroughRuntime(t *testing.T) {
    rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 1, MailboxSize: 8})
    receiver := &recordingService{}
    ref, err := rt.CreateService(gsr.ServiceSpec{Service: receiver})
    if err != nil { t.Fatal(err) }
    if err := rt.Send(ref, 1001, "hello"); err != nil { t.Fatal(err) }
    eventually(t, func() bool { return receiver.last() == "hello" })
}

func TestSendToUnknownServiceFails(t *testing.T) {
    rt := gsr.NewRuntime(gsr.Config{NodeID: "local", Workers: 1, MailboxSize: 8})
    if err := rt.Send(gsr.ServiceRef{Node: "local", ID: 99}, 1001, nil); !errors.Is(err, gsr.ErrServiceNotFound) {
        t.Fatalf("err = %v", err)
    }
}
```

`recordingService` 的 mutex 只能用于测试读取，不能成为生产状态模型。

- [ ] **Step 2: 运行失败测试**

Run: `go test ./runtime -run 'TestCreateService|TestSend' -count=1`

Expected: FAIL，`Runtime`、`ServiceSpec` 或 `CreateService` 未定义。

- [ ] **Step 3: 实现 Runtime 管道**

```go
type Service interface {
    Init(ServiceContext) error
    Handle(CommandContext, Command) error
    Stop(context.Context) error
    Close() error
}

type ServiceSpec struct { Service Service; Policy ServicePolicy }

func (r *Runtime) Send(target ServiceRef, id CommandID, payload any) error {
    return r.route(Envelope{Target: target, Command: id, Payload: payload})
}
```

`CreateService` 创建 Mailbox 和上下文、注册实例、调用 `Init`、最后设置 `ServiceRunning`。`route` 只能查 Registry、写 Mailbox、通知 Scheduler，不能直接调用 `Handle`。Scheduler 使用固定 worker 和每实例原子 ready 标记；每批最多 `Config.MaxBatch` 条，保证同一个 Service handler 最大并发为 `1`。

- [ ] **Step 4: 验证与提交**

Run: `gofmt -w runtime && go test ./runtime -run 'TestCreateService|TestSend|TestServiceHandlerIsSerial' -count=20 && go test -race ./runtime -run 'TestCreateService|TestSend|TestServiceHandlerIsSerial' -count=1`

Expected: PASS；未知 Ref 返回 `ErrServiceNotFound`；同一 Service 最大并发为 `1`。提交：`git add runtime && git commit -m "feat(runtime): 实现本地 Service 与 Send 调度"`。

## Task 4: CommandContext、Session、Call 与 Reply

**Files:** Create `runtime/call.go`, `runtime/call_test.go`; modify `runtime/service.go`, `runtime/runtime.go`.

- [ ] **Step 1: 写失败测试**

```go
func TestCallReturnsReply(t *testing.T) {
    rt := newTestRuntime(t)
    ref, _ := rt.CreateService(gsr.ServiceSpec{Service: replyService{}})
    got, err := rt.Call(context.Background(), ref, 10, "ping")
    if err != nil || got != "pong" { t.Fatalf("got %v, err %v", got, err) }
}

func TestCallTimesOut(t *testing.T) {
    rt := newTestRuntime(t)
    ref, _ := rt.CreateService(gsr.ServiceSpec{Service: noReplyService{}})
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
    defer cancel()
    _, err := rt.Call(ctx, ref, 10, nil)
    if !errors.Is(err, gsr.ErrTimeout) { t.Fatalf("err = %v", err) }
}
```

另写重复 `ctx.Reply` 和 Send 场景 Reply 测试，分别断言 `ErrReplyTwice`。

- [ ] **Step 2: 实现 Pending Call**

```go
func (r *Runtime) Call(ctx context.Context, target ServiceRef, id CommandID, payload any) (any, error) {
    session, pending := r.pending.create()
    if err := r.route(Envelope{Target: target, Session: session, Command: id, Payload: payload}); err != nil {
        r.pending.remove(session)
        return nil, err
    }
    return pending.wait(ctx)
}
```

`Reply` 仅在 `Session != 0` 时有效且只能完成一次；调用 context 截止时返回 `ErrTimeout` 并删除 session；迟到 Reply 丢弃且不阻塞 worker。

- [ ] **Step 3: 验证与提交**

Run: `go test ./runtime -run 'TestCall|TestReply' -count=20 && go test -race ./runtime -run 'TestCall|TestReply' -count=1`

Expected: 成功 Call 得到结果，超时为 `ErrTimeout`，重复 Reply 和 Send Reply 失败。提交：`git add runtime && git commit -m "feat(runtime): 实现 Call Reply 与 PendingCall"`。

## Task 5: Timer Command 管道

**Files:** Create `runtime/timer.go`, `runtime/timer_test.go`; modify `runtime/runtime.go`, `runtime/service.go`.

- [ ] **Step 1: 写失败测试**

```go
func TestAfterDeliversCommand(t *testing.T) {
    rt := newTestRuntime(t)
    svc := &recordingService{}
    ref, _ := rt.CreateService(gsr.ServiceSpec{Service: svc})
    if _, err := rt.After(ref, 5*time.Millisecond, 20, "expired"); err != nil { t.Fatal(err) }
    eventually(t, func() bool { return svc.last() == "expired" })
}

func TestCancelTimerPreventsDelivery(t *testing.T) {
    rt := newTestRuntime(t)
    svc := &recordingService{}
    ref, _ := rt.CreateService(gsr.ServiceSpec{Service: svc})
    id, _ := rt.After(ref, 30*time.Millisecond, 20, "expired")
    if err := rt.Cancel(id); err != nil { t.Fatal(err) }
    time.Sleep(50 * time.Millisecond)
    if got := svc.count(); got != 0 { t.Fatalf("count = %d", got) }
}
```

- [ ] **Step 2: 实现首版 TimerManager**

`After` 分配 `TimerID` 并用标准库 timer 到期调用 `Send(target, id, payload)`；timer 回调绝不访问业务 Service。`Cancel` 对已触发或已取消 timer 幂等返回 `nil`；目标关闭时取消其未触发 timer。

- [ ] **Step 3: 验证与提交**

Run: `go test ./runtime -run 'TestAfter|TestCancelTimer' -count=20 && go test -race ./runtime -run 'TestAfter|TestCancelTimer' -count=1`

Expected: Timer 只投递一个 Command，取消后零投递，无 race。提交：`git add runtime && git commit -m "feat(runtime): 增加 Timer Command 投递"`。

## Task 6: 生命周期与最终本地示例

**Files:** Create `runtime/lifecycle.go`, `runtime/lifecycle_test.go`, `examples/local-runtime/main.go`; modify `runtime/runtime.go`, `runtime/registry.go`, `README.md`.

- [ ] **Step 1: 写失败测试**

```go
func TestStopRemovesServiceAndRejectsNewSend(t *testing.T) {
    rt := newTestRuntime(t)
    ref, _ := rt.CreateService(gsr.ServiceSpec{Service: &stoppableService{}})
    if err := rt.Stop(context.Background(), ref); err != nil { t.Fatal(err) }
    if err := rt.Send(ref, 1, nil); !errors.Is(err, gsr.ErrServiceClosed) { t.Fatalf("err = %v", err) }
}
```

另写未回复 Call 在 `Stop` 后返回 `ErrServiceClosed` 的测试。

- [ ] **Step 2: 实现退出流程**

`Stop` 依次标记 `ServiceStopping`、拒绝新消息、用 `ServicePolicy.StopTimeout` 调用 `Service.Stop`、调用 `Close`、取消目标 Timer、以 `ErrServiceClosed` 唤醒目标 PendingCall、删除 Registry、标记 `ServiceClosed`。实现 RFC-0180 的完整 `ServiceStatus` 常量，不允许服务永远停在 `ServiceStopping`。

- [ ] **Step 3: 写示例和 README**

`examples/local-runtime/main.go` 创建一个 `echoService`，对 `cmdEcho` 调用 `ctx.Reply("hello")`，随后 `rt.Call` 并打印 `hello`。README 添加命令：`go test ./...`、`go run ./examples/local-runtime`，预期输出 `hello`。

- [ ] **Step 4: 最终验收和提交**

Run: `gofmt -w runtime examples && go test ./... && go vet ./... && go test -race ./... && go run ./examples/local-runtime && git status --short`

Expected: 三个质量命令成功，示例输出 `hello`，提交前没有意外文件。提交：`git add README.md examples runtime && git commit -m "feat(example): 增加本地 Service Runtime 示例"`。

## 后续阶段

| 阶段 | RFC | 可交付物 | 前提 |
|---|---|---|---|
| 2 | RFC-0190、0191 | 两节点进程的远程 Send/Call/Reply、Handshake、WireEnvelope | 本计划全量测试通过 |
| 3 | RFC-0200、0210、0220、0230、0240 | Discovery、Snapshot、Supervisor、Metrics、benchmark | Cluster 行为与本地一致 |
| 4 | RFC-0250、0260、0270 | Control Plane、ServiceGroup、Drain、版本切换 | Tooling 基础稳定 |
| 5 | RFC-0280、0290 | Record/Replay、LoginService、Gateway、SessionRegistry | Tooling API 已冻结 |
| 6 | RFC-0300 至 0370 | Player、Battle、Timeline、Room、Wallet、业务模板 | Core 不引用业务类型 |
| 7 | RFC-0400 | 打地鼠端到端示例 | Business Template 通过测试 |

## 自检

任务 1 覆盖项目 `AGENTS.md` 和 Skill；任务 2 至 6 覆盖 RFC-0100 至 RFC-0180；每个任务给出明确文件、失败测试、验证命令和独立提交边界。没有把 Cluster、认证、业务或 Timer Wheel 提前带入单节点核心实现。
