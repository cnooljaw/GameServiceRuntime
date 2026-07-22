# GSR Phase 7D Snapshot Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在不修改 Core Service 接口的前提下，实现通过 Command 采集、由外部 Store 保存、在组合根加载并构造新 Service 的版本化 Snapshot Tooling。

**Architecture:** 目标 Service 在 `CaptureCommand` Handler 中串行生成有版本和修订号的内存 State；`Manager` 校验并复制响应，再在 Handler 外调用 `Store.Save`。`MemoryStore` 用每个稳定业务 Key 的 Revision 保证旧快照不会覆盖新快照；恢复只返回独立 Snapshot，由组合根构造新 Service 和新 `ServiceRef`。

**Tech Stack:** Go 1.23.3、标准库 `context`/`encoding/json`/`sync`/`time`、GSR `Command`/`Call`/`ClusterCodec`、TDD、Race Detector。

---

## 文件结构

- `tooling/snapshot/errors.go`：稳定 Snapshot 错误。
- `tooling/snapshot/types.go`：公开协议、Store/Caller 接口和 Config。
- `tooling/snapshot/validation.go`：Key、State、Snapshot、typed nil 和深复制规则。
- `tooling/snapshot/memory_store.go`：并发安全的内存 Store 与 Revision 冲突规则。
- `tooling/snapshot/manager.go`：Capture/Load 编排；Store IO 位于 Call 返回之后。
- `tooling/snapshot/codec.go`：Capture Command 的稳定 JSON Codec 和 fallback。
- `tooling/snapshot/*_test.go`：Store、Manager、Codec、本地/远程与并发契约。
- `examples/snapshot-runtime/main.go`：Capture、Stop、Load、构造新实例和新 `ServiceRef` 的端到端示例。
- `README.md`、`CHANGELOG.md`、`docs/TODO.md`、`docs/rfcs/RFC-0210-Tooling-Snapshot.md`、`docs/rfcs/RFC-0500-Roadmap.md`：验收后同步状态。

### Task 1：冻结 RFC 与阶段边界

**Files:**

- Create: `docs/rfcs/RFC-0003-Foundation-RFC-Lifecycle.md`
- Modify: `docs/SUMMARY.md`
- Modify: `docs/rfcs/RFC-0000-Foundation-Glossary.md`
- Modify: `docs/rfcs/RFC-0001-Foundation-Design-Principles.md`
- Modify: `docs/rfcs/RFC-0002-Foundation-Conflict-Resolution.md`
- Modify: `docs/rfcs/RFC-0100-Core-Service.md`
- Modify: `docs/rfcs/RFC-0180-Core-Lifecycle.md`
- Modify: `docs/rfcs/RFC-0190-Core-Cluster-Data-Plane.md`
- Modify: `docs/rfcs/RFC-0210-Tooling-Snapshot.md`
- Modify: `docs/rfcs/RFC-0220-Tooling-Supervisor.md`
- Modify: `docs/rfcs/RFC-0240-Tooling-Performance.md`
- Modify: `docs/rfcs/RFC-0250-Tooling-Cluster-Control-Plane.md`
- Modify: `docs/rfcs/RFC-0260-Tooling-ServiceGroup-Routing.md`
- Modify: `docs/rfcs/RFC-0270-Tooling-Drain-Hot-Reload.md`
- Modify: `docs/rfcs/RFC-0280-Tooling-Command-Record-Replay.md`
- Modify: `docs/rfcs/RFC-0290-Tooling-LoginService-Gateway.md`
- Modify: `docs/rfcs/RFC-0300-Business-Layering.md`
- Modify: `docs/rfcs/RFC-0310-Business-Battle.md`
- Modify: `docs/rfcs/RFC-0320-Business-Timeline.md`
- Modify: `docs/rfcs/RFC-0330-Business-Room.md`
- Modify: `docs/rfcs/RFC-0340-Business-PlayerService.md`
- Modify: `docs/rfcs/RFC-0350-Business-PlayerModule.md`
- Modify: `docs/rfcs/RFC-0360-Business-WalletService.md`
- Modify: `docs/rfcs/RFC-0370-Business-Templates.md`
- Modify: `docs/rfcs/RFC-0400-Example-Whack-Mole.md`
- Modify: `docs/rfcs/RFC-0500-Roadmap.md`
- Modify: `docs/TODO.md`

- [x] **Step 1: 定义 RFC 状态和必备章节**

新增 `RFC-0003`，固定 `草案 / 待实现 / 已接受 / 已废弃`，并要求目标、非目标、分层、公开契约、生命周期、错误、并发、可观测性和验收。

- [x] **Step 2: 修复与已实现 Core 冲突的 Snapshot 契约**

删除 Core `Stateful.Snapshot/Restore` 方向，改为：

```text
Call CaptureCommand -> serial Handle -> State
Manager -> Store.Save
Manager.Load -> composition root -> CreateService(new instance)
```

- [x] **Step 3: 拆分 Snapshot 与 Supervisor**

路线图改为 7D Snapshot、7E Supervisor、7F 客户端入口。Supervisor 保持草案，并明确失败通知、launcher、退避任务 owner 和名字发布失败是实施前阻塞项。

- [x] **Step 4: 审核后续 RFC 的现有 Runtime 可实现性**

修正 Timeline 取消、Room 创建 Service、Login socket owner、ServiceGroup/Discovery、VisitorTracker、Record decorator 和 Battle 结算/停止流程。

- [x] **Step 5: 验证并提交文档裁决**

Run:

```bash
git diff --check
rg -n 'GateService|Call WalletService|Snapshot 与 Supervisor|Phase 7E：客户端' docs/rfcs docs/TODO.md
rg -n 'type Stateful' docs/rfcs/RFC-0100-Core-Service.md
```

Expected: `git diff --check` 无输出；`rg` 无命中。

Commit:

```bash
git add docs
git commit -m "docs(rfc): 审核后续阶段并冻结 Snapshot 边界"
```

### Task 2：实现 Snapshot 模型与 MemoryStore

**Files:**

- Create: `tooling/snapshot/errors.go`
- Create: `tooling/snapshot/types.go`
- Create: `tooling/snapshot/validation.go`
- Create: `tooling/snapshot/memory_store.go`
- Test: `tooling/snapshot/memory_store_test.go`

- [x] **Step 1: 写 MemoryStore 失败测试**

测试至少包含以下完整行为：

```go
func TestMemoryStoreReturnsIndependentCopies(t *testing.T) {
    store := NewMemoryStore()
    original := validSnapshot(2, []byte("state"))
    if err := store.Save(context.Background(), original); err != nil { t.Fatal(err) }
    original.State.Payload[0] = 'X'
    loaded, err := store.Load(context.Background(), original.Key)
    if err != nil { t.Fatal(err) }
    if string(loaded.State.Payload) != "state" { t.Fatalf("payload = %q", loaded.State.Payload) }
    loaded.State.Payload[0] = 'Y'
    again, err := store.Load(context.Background(), original.Key)
    if err != nil { t.Fatal(err) }
    if string(again.State.Payload) != "state" { t.Fatalf("payload = %q", again.State.Payload) }
}

func TestMemoryStoreOrdersRevisionsAndDetectsConflict(t *testing.T) {
    store := NewMemoryStore()
    current := validSnapshot(2, []byte("two"))
    if err := store.Save(context.Background(), current); err != nil { t.Fatal(err) }
    if err := store.Save(context.Background(), validSnapshot(1, []byte("one"))); !errors.Is(err, ErrStaleSnapshot) {
        t.Fatalf("Save stale error = %v", err)
    }
    if err := store.Save(context.Background(), validSnapshot(2, []byte("other"))); !errors.Is(err, ErrSnapshotConflict) {
        t.Fatalf("Save conflict error = %v", err)
    }
    if err := store.Save(context.Background(), current); err != nil { t.Fatalf("idempotent Save = %v", err) }
}
```

增加表驱动用例覆盖 invalid context、Key、Source、Schema、Version、Revision、nil Payload、zero CapturedAt、not found，以及并发 Save/Load。

- [x] **Step 2: 运行测试并确认失败**

Run: `go test ./tooling/snapshot -run '^TestMemoryStore' -count=1`

Expected: FAIL，包或符号尚不存在。

- [x] **Step 3: 写最小公开模型和错误**

实现以下契约，并为全部导出符号写 Go doc：

```go
const CaptureCommand gsr.CommandID = 0x02000201

type Key struct { Namespace, ID string }
type State struct { Schema string; Version uint32; Revision uint64; Payload []byte }
type Snapshot struct { Key Key; Source gsr.ServiceRef; CapturedAt time.Time; State State }
type CaptureRequest struct{}
type CaptureResponse struct{ State State }
type Store interface { Save(context.Context, Snapshot) error; Load(context.Context, Key) (Snapshot, error) }
type CommandCaller interface { Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error) }
type Config struct { MaxPayloadBytes int; Now func() time.Time }
```

错误完整定义为：`ErrInvalidConfig`、`ErrInvalidContext`、`ErrInvalidKey`、`ErrInvalidTarget`、`ErrInvalidState`、`ErrPayloadTooLarge`、`ErrSnapshotNotFound`、`ErrStaleSnapshot`、`ErrSnapshotConflict`、`ErrInvalidResponse`、`ErrUnsupportedCommand`。

- [x] **Step 4: 实现验证、深复制和 MemoryStore**

核心替换逻辑必须等价于：

```go
func (s *MemoryStore) Save(ctx context.Context, candidate Snapshot) error {
    if isNil(ctx) { return ErrInvalidContext }
    if err := validateSnapshot(candidate, 0); err != nil { return err }
    candidate = cloneSnapshot(candidate)
    s.mu.Lock()
    defer s.mu.Unlock()
    current, exists := s.snapshots[candidate.Key]
    switch {
    case !exists || candidate.State.Revision > current.State.Revision:
        s.snapshots[candidate.Key] = candidate
        return nil
    case candidate.State.Revision < current.State.Revision:
        return ErrStaleSnapshot
    case equalState(candidate.State, current.State):
        return nil
    default:
        return ErrSnapshotConflict
    }
}
```

`Load` 在锁内取值，锁外返回深复制；构造函数初始化非 nil map，不创建 goroutine。

- [x] **Step 5: 运行 Store 测试和 Race Detector**

Run:

```bash
go test ./tooling/snapshot -run '^TestMemoryStore' -count=100
go test -race ./tooling/snapshot -run '^TestMemoryStore' -count=20
```

Expected: PASS，Race Detector 无报告。

- [x] **Step 6: 提交 Store 切片**

```bash
git add tooling/snapshot/errors.go tooling/snapshot/types.go tooling/snapshot/validation.go tooling/snapshot/memory_store.go tooling/snapshot/memory_store_test.go
git commit -m "feat(snapshot): 增加版本化内存快照存储"
```

### Task 3：实现 Manager Capture 与 Load

**Files:**

- Create: `tooling/snapshot/manager.go`
- Test: `tooling/snapshot/manager_test.go`

- [x] **Step 1: 写 Capture/Load 失败测试**

测试 fake Caller 收到稳定 Command 和请求，并让 fake Store 在 `Call` 返回前被调用时失败：

```go
func TestManagerCaptureCallsServiceBeforeStore(t *testing.T) {
    now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
    target := gsr.ServiceRef{Node: "node-a", ID: 7}
    key := Key{Namespace: "player", ID: "42"}
    caller := &fakeCaller{value: CaptureResponse{State: State{Schema: "player", Version: 1, Revision: 3, Payload: []byte("ok")}}}
    store := &recordingStore{caller: caller}
    manager, err := NewManager(caller, store, Config{Now: func() time.Time { return now }})
    if err != nil { t.Fatal(err) }
    got, err := manager.Capture(context.Background(), target, key)
    if err != nil { t.Fatal(err) }
    if caller.command != CaptureCommand || caller.target != target { t.Fatalf("call = %#v", caller) }
    if _, ok := caller.payload.(CaptureRequest); !ok { t.Fatalf("payload = %T", caller.payload) }
    if got.Key != key || got.Source != target || !got.CapturedAt.Equal(now) { t.Fatalf("snapshot = %#v", got) }
    if store.saved.State.Revision != 3 { t.Fatalf("saved = %#v", store.saved) }
}
```

增加用例：typed nil Caller/Store、nil context、invalid target/key、Call error 原样返回、错误响应类型、invalid state、payload 超限、Store error 返回零值、Load 校验 Store 返回值和独立副本。

- [x] **Step 2: 运行测试并确认失败**

Run: `go test ./tooling/snapshot -run '^TestManager' -count=1`

Expected: FAIL，`NewManager` 或方法尚不存在。

- [x] **Step 3: 实现 Manager**

构造和 Capture 流程必须等价于：

```go
func NewManager(caller CommandCaller, store Store, config Config) (*Manager, error) {
    if isNil(caller) || isNil(store) || config.MaxPayloadBytes < 0 { return nil, ErrInvalidConfig }
    if config.MaxPayloadBytes == 0 { config.MaxPayloadBytes = defaultMaxPayloadBytes }
    if config.Now == nil { config.Now = time.Now }
    return &Manager{caller: caller, store: store, maxPayloadBytes: config.MaxPayloadBytes, now: config.Now}, nil
}

func (m *Manager) Capture(ctx context.Context, target gsr.ServiceRef, key Key) (Snapshot, error) {
    if isNil(ctx) { return Snapshot{}, ErrInvalidContext }
    if err := validateTarget(target); err != nil { return Snapshot{}, err }
    if err := validateKey(key); err != nil { return Snapshot{}, err }
    value, err := m.caller.Call(ctx, target, CaptureCommand, CaptureRequest{})
    if err != nil { return Snapshot{}, err }
    response, ok := value.(CaptureResponse)
    if !ok { return Snapshot{}, ErrInvalidResponse }
    result := Snapshot{Key: key, Source: target, CapturedAt: m.now(), State: cloneState(response.State)}
    if err := validateSnapshot(result, m.maxPayloadBytes); err != nil { return Snapshot{}, err }
    if err := m.store.Save(ctx, result); err != nil { return Snapshot{}, err }
    return cloneSnapshot(result), nil
}
```

`Load` 调用 Store 后校验 Key 相等、结构合法和大小上限，再返回深复制。

- [x] **Step 4: 运行 Manager 测试**

Run:

```bash
go test ./tooling/snapshot -run '^TestManager' -count=100
go test -race ./tooling/snapshot -run '^TestManager' -count=20
```

Expected: PASS。

- [x] **Step 5: 提交 Manager 切片**

```bash
git add tooling/snapshot/manager.go tooling/snapshot/manager_test.go
git commit -m "feat(snapshot): 通过 Command 采集并保存快照"
```

### Task 4：实现 Cluster Codec 与远程验收

**Files:**

- Create: `tooling/snapshot/codec.go`
- Test: `tooling/snapshot/codec_test.go`
- Test: `tooling/snapshot/remote_test.go`

- [x] **Step 1: 写 Codec 失败测试**

覆盖精确线格式：

```go
func TestCodecUsesStableWireFormat(t *testing.T) {
    codec := NewCodec(nil)
    request, err := codec.Encode(CaptureCommand, false, CaptureRequest{})
    if err != nil { t.Fatal(err) }
    if string(request) != `{}` { t.Fatalf("request = %s", request) }
    response, err := codec.Encode(CaptureCommand, true, CaptureResponse{State: State{Schema: "player", Version: 1, Revision: 2, Payload: []byte("ok")}})
    if err != nil { t.Fatal(err) }
    const want = `{"state":{"schema":"player","version":1,"revision":2,"payload":"b2s="}}`
    if string(response) != want { t.Fatalf("response = %s", response) }
}
```

增加未知字段兼容、尾随 JSON、错误 Go 类型、fallback 委托、无 fallback 的 `ErrUnsupportedCommand` 和 response round-trip。

- [x] **Step 2: 写双节点远程 Capture 失败测试**

使用 `transport/tcp` 的 `127.0.0.1:0` listener：node-b 创建实现 `CaptureCommand` 的 Service；node-a 使用 `NewCodec(nil)` 和 `Manager` 远程 Capture，断言 Store 中 Source 为 node-b ServiceRef、State 完整。

- [x] **Step 3: 运行测试并确认失败**

Run: `go test ./tooling/snapshot -run '^Test(Codec|Remote)' -count=1`

Expected: FAIL，Codec 尚不存在。

- [x] **Step 4: 实现可组合 Codec**

私有 wire 类型固定字段：

```go
type wireState struct {
    Schema string `json:"schema"`
    Version uint32 `json:"version"`
    Revision uint64 `json:"revision"`
    Payload []byte `json:"payload"`
}
type wireCaptureResponse struct { State wireState `json:"state"` }
```

只有 `CaptureCommand` 由本 Codec 处理。Decoder 使用一次 `Decode` 后再确认 EOF；允许未知字段，拒绝第二个 JSON 值。非 Snapshot Command 委托非 nil fallback。

- [x] **Step 5: 运行 Codec、远程和 Race 测试**

Run:

```bash
go test ./tooling/snapshot -run '^Test(Codec|Remote)' -count=30
go test -race ./tooling/snapshot -run '^Test(Codec|Remote)' -count=10
```

Expected: PASS。

- [x] **Step 6: 提交 Codec 切片**

```bash
git add tooling/snapshot/codec.go tooling/snapshot/codec_test.go tooling/snapshot/remote_test.go
git commit -m "feat(snapshot): 增加跨节点快照编解码"
```

### Task 5：增加受限恢复示例

**Files:**

- Create: `examples/snapshot-runtime/main.go`
- Test: `tooling/snapshot/integration_test.go`

- [x] **Step 1: 写本地恢复集成测试**

测试流程必须是：创建 revision=1 的 Service；发送状态变更 Command；Capture revision=2；Stop 旧 Service；Load；在构造函数中解析 Payload；CreateService 新实例；断言新 `ServiceRef` 不等于旧 Ref，查询状态等于快照；向旧 Ref Call 返回 `ErrServiceClosed`。

- [x] **Step 2: 在增加示例前运行集成测试**

Run: `go test ./tooling/snapshot -run '^TestSnapshotRestoreCreatesNewServiceInstance$' -count=1`

Expected: PASS。该任务不新增通用 Restore API；测试先证明现有 Manager/Store 已形成组合根恢复缝，再增加同一流程的可执行示例。

- [x] **Step 3: 实现示例**

示例 Service 只在 Handler 中修改状态；Capture Handler 使用标准库 JSON 生成 `State`。恢复代码位于 `main` 组合根：

```go
loaded, err := manager.Load(ctx, snapshot.Key)
if err != nil { log.Fatal(err) }
restored, err := newCounterServiceFromSnapshot(loaded.State)
if err != nil { log.Fatal(err) }
newRef, err := runtime.CreateService(gsr.ServiceSpec{Service: restored})
if err != nil { log.Fatal(err) }
value, err := runtime.Call(ctx, newRef, commandGet, struct{}{})
if err != nil { log.Fatal(err) }
fmt.Println(value)
```

预期输出：`2`。

- [x] **Step 4: 运行集成测试和示例**

Run:

```bash
go test ./tooling/snapshot -run '^TestSnapshotRestoreCreatesNewServiceInstance$' -count=50
go test -race ./tooling/snapshot -run '^TestSnapshotRestoreCreatesNewServiceInstance$' -count=20
go run ./examples/snapshot-runtime
```

Expected: tests PASS；示例输出 `2`。

- [x] **Step 5: 提交恢复示例**

```bash
git add examples/snapshot-runtime/main.go tooling/snapshot/integration_test.go
git commit -m "feat(snapshot): 增加组合根受限恢复示例"
```

### Task 6：文档同步、Review 与完整门禁

**Files:**

- Modify: `README.md`
- Modify: `CHANGELOG.md`
- Modify: `docs/TODO.md`
- Modify: `docs/rfcs/RFC-0210-Tooling-Snapshot.md`
- Modify: `docs/rfcs/RFC-0500-Roadmap.md`
- Modify: `docs/GSR-Book/04-第四篇-基础设施/01-Snapshot.md`
- Modify: `docs/superpowers/plans/2026-07-22-gsr-phase7d-snapshot.md`
- Modify: `.github/workflows/ci.yml`

- [x] **Step 1: 同步实现状态**

`RFC-0210` 在 Review 清零后改为“已接受（2026-07-22）”；路线图把 7D 标记完成、下一阶段指向 7E Supervisor 的 RFC 裁决。README 增加 Snapshot 示例和“不会原地 Restore”的限制；CHANGELOG 增加 `[Unreleased]` Snapshot 条目。

- [x] **Step 2: 把全部示例纳入 CI**

在现有 local/cluster 检查后补充 discovery、monitor 和 snapshot，分别校验稳定输出或成功退出。Monitor JSON 用 `go run ./examples/monitor-runtime >/dev/null`，Snapshot 校验输出 `2`。

- [x] **Step 3: 执行双轴 Review**

逐项确认：

- RFC 轴：公开类型、错误、Revision、大小、Codec 和恢复流程完全一致。
- 分层轴：`runtime` 零 Snapshot 依赖，Business Schema 不进入 Tooling。
- 状态轴：运行中读取只经 Capture Command，恢复只构造新实例。
- 并发轴：Store 副本、Revision 原子替换、typed nil、取消和 Race。
- 失败轴：Call、Codec、Store、过期 Revision、冲突和旧 Ref 都有稳定结果。
- 热路径轴：Core Send/Call/Timer 无修改；Store IO 不在目标 Handler。

Review 结果：0 个 P1；发现 3 个 P2 并全部修复。P2 分别是公开 `MemoryStore` 零值写入 nil map、默认 1 MiB 上限缺少边界测试、RFC 对 Key/Schema 首尾空白的表述不够精确。修复后 Standards 轴和 Spec 轴均无遗留发现。

- [x] **Step 4: 执行完整质量门禁**

Run:

```bash
go test ./... -count=1
go vet ./...
go test -race ./... -count=1
go test ./tooling/snapshot -count=100
go run ./examples/local-runtime
go run ./examples/cluster-runtime
go run ./examples/discovery-runtime
go run ./examples/monitor-runtime
go run ./examples/snapshot-runtime
git diff --check
```

Expected: 全部成功；示例依次包含 `hello`、`hello cluster`、`.config -> node-b/2`、一行 Monitor JSON、`2`；`git diff --check` 无输出。

- [x] **Step 5: 检查计划占位符和类型一致性**

Run:

```bash
rg -n 'TB''D|implement lat''er|similar to ab''ove|类似 Ta''sk' docs/superpowers/plans/2026-07-22-gsr-phase7d-snapshot.md
rg -n 'CaptureCommand|MaxPayloadBytes|ErrSnapshotConflict|NewMemoryStore' docs/rfcs/RFC-0210-Tooling-Snapshot.md tooling/snapshot
```

Expected: 第一条无命中；第二条 RFC 与代码命名一致。

- [x] **Step 6: 提交验收文档**

```bash
git add .github/workflows/ci.yml README.md CHANGELOG.md docs/TODO.md docs/rfcs/RFC-0210-Tooling-Snapshot.md docs/rfcs/RFC-0500-Roadmap.md docs/superpowers/plans/2026-07-22-gsr-phase7d-snapshot.md
git commit -m "docs(snapshot): 完成 Phase 7D 验收"
```

## 完成标准

- Snapshot 通过目标 Service 的 Mailbox Command 采集，不增加 Core `Stateful`。
- Store IO 在目标 Handler 返回后执行，MemoryStore 保证副本和 Revision 冲突语义。
- 本地与远程 Capture 使用同一 Manager 和稳定 Codec。
- Load 只返回数据；恢复在组合根构造新 Service 和新 `ServiceRef`。
- Supervisor、自动重启、持久化数据库和 Record/Replay 没有进入 Phase 7D。
- 全量测试、vet、race、100 次 Snapshot 重复测试、全部示例和文档检查通过。

## 实施结果

- 文档审核提交：`3ac05f0`。
- MemoryStore 提交：`7953625`。
- Manager 提交：`6bec9d3`。
- Cluster Codec 提交：`50b329b`。
- 组合根恢复示例提交：`0745a90`。
- Review 修正提交：`f203fe1`。
- `go test ./... -count=1`、`go vet ./...`、`go test -race ./... -count=1` 通过。
- `go test ./tooling/snapshot -count=100` 通过。
- local、cluster、discovery、monitor 和 snapshot 示例全部通过；Snapshot 输出 `2`。
- 双轴 Review 最终为 0 个 Standards 遗留、0 个 Spec 遗留。
