# GSR Phase 7D Snapshot Contract Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 Phase 7D 审查发现的 Snapshot 数据不变量、稳定 Key 所有权、canonical Save 返回、UTF-8 校验和 RFC 元数据治理问题。

**Architecture:** Business Service 拥有稳定 `Key`；Capture 请求携带期望 Key，响应返回 owner Key，`Manager` 在保存前校验二者一致。`Store.Save` 返回原子操作后真正保留的 canonical Snapshot；结构验证发生在复制前，复制函数保留 nil 语义。RFC 元数据通过仓库级 Go 测试约束。

**Tech Stack:** Go 1.23.3、标准库 `context`/`encoding/json`/`unicode/utf8`/`sync`、GSR `Command`/`Call`/`ClusterCodec`、Markdown RFC、TDD、Race Detector。

---

## 文件结构

- `docs/rfcs/RFC-0210-Tooling-Snapshot.md`：冻结 Key owner、canonical Save、nil/UTF-8 和错误语义。
- `docs/rfcs/RFC-0003-Foundation-RFC-Lifecycle.md`：明确接受日期、依赖字段和元数据门禁。
- `docs/rfcs/*.md`：迁移缺失依赖、非规范状态和尾随空白。
- `tooling/snapshot/types.go`：给 Capture 协议加入 Key，并让 `Store.Save` 返回 canonical Snapshot。
- `tooling/snapshot/validation.go`：保持 nil 语义并验证合法 UTF-8。
- `tooling/snapshot/memory_store.go`：原子返回真正保留的 Snapshot。
- `tooling/snapshot/manager.go`：验证 owner Key、原始 State 和 Store canonical 结果。
- `tooling/snapshot/codec.go`：稳定编码带 Key 的请求和响应。
- `tooling/snapshot/*_test.go`：通过公开 seam 覆盖全部修复行为。
- `examples/snapshot-runtime/main.go`：示例 Service 显式拥有并验证 Key。
- `runtime/rfc_document_policy_test.go`：自动验证 RFC 元数据和尾随空白。
- `docs/GSR-Book/04-第四篇-基础设施/01-Snapshot.md`、`README.md`、`CHANGELOG.md`：同步最终公开行为。

### Task 1：冻结修复契约

**Files:**

- Modify: `docs/rfcs/RFC-0210-Tooling-Snapshot.md`
- Modify: `docs/rfcs/RFC-0003-Foundation-RFC-Lifecycle.md`
- Modify: `docs/superpowers/plans/2026-07-22-gsr-phase7d-snapshot.md`
- Create: `docs/superpowers/plans/2026-07-22-gsr-phase7d-snapshot-contract-fixes.md`

- [x] **Step 1: 将 RFC-0210 暂时改为待实现并冻结新接口**

公开契约改为：

```go
type CaptureRequest struct {
    Key Key
}

type CaptureResponse struct {
    Key   Key
    State State
}

type Store interface {
    Save(context.Context, Snapshot) (Snapshot, error)
    Load(context.Context, Key) (Snapshot, error)
}
```

规则必须写明：Service 返回自己拥有的 Key；Manager 拒绝无效或不匹配的响应 Key；Store 成功时返回原子操作后真正保留的独立 Snapshot；同 Revision/State 的幂等保存返回旧 canonical 值。

- [x] **Step 2: 修订 RFC-0003 元数据规则**

头部字段顺序定义为 `状态`、可选 `目标阶段`、已接受 RFC 可选 `接受日期`、`范围`、`依赖`、可选 `依据`。状态值保持精确枚举；`依赖` 必须存在，没有依赖时写 `无`。

- [x] **Step 3: 记录上一轮 Review 结论已重新打开**

旧计划保留历史结果，同时说明后续独立 Review 发现本计划覆盖的问题，避免继续声称“无遗留发现”。

- [x] **Step 4: 检查并提交文档裁决**

Run: `git diff --check -- docs/rfcs docs/superpowers/plans`

Expected: 无输出。

Commit:

```bash
git add docs/rfcs/RFC-0003-Foundation-RFC-Lifecycle.md docs/rfcs/RFC-0210-Tooling-Snapshot.md docs/superpowers/plans/2026-07-22-gsr-phase7d-snapshot.md docs/superpowers/plans/2026-07-22-gsr-phase7d-snapshot-contract-fixes.md
git commit -m "docs(snapshot): 收紧快照身份与存储契约"
```

### Task 2：修复 State 与 Store 不变量

**Files:**

- Modify: `tooling/snapshot/types.go`
- Modify: `tooling/snapshot/validation.go`
- Modify: `tooling/snapshot/memory_store.go`
- Modify: `tooling/snapshot/memory_store_test.go`
- Modify: `tooling/snapshot/manager.go`
- Modify: `tooling/snapshot/manager_test.go`

- [x] **Step 1: 写失败测试**

至少增加以下行为：

```go
func TestManagerRejectsNilPayloadBeforeCopy(t *testing.T) {
    caller := &fakeCaller{value: CaptureResponse{Key: testKey(), State: State{
        Schema: "player", Version: 1, Revision: 1, Payload: nil,
    }}}
    store := &recordingStore{}
    manager := newTestManager(t, caller, store, Config{})
    if _, err := manager.Capture(context.Background(), testTarget(), testKey()); !errors.Is(err, ErrInvalidState) {
        t.Fatalf("Capture error = %v, want ErrInvalidState", err)
    }
    if store.saveCalled {
        t.Fatal("Store.Save called for nil payload")
    }
}

func TestMemoryStoreIdempotentSaveReturnsExistingCanonicalSnapshot(t *testing.T) {
    store := NewMemoryStore()
    original := validSnapshot(2, []byte("state"))
    if _, err := store.Save(context.Background(), original); err != nil {
        t.Fatal(err)
    }
    retry := cloneSnapshotForTest(original)
    retry.Source = gsr.ServiceRef{Node: "node-b", ID: 8}
    retry.CapturedAt = original.CapturedAt.Add(time.Minute)
    canonical, err := store.Save(context.Background(), retry)
    if err != nil {
        t.Fatal(err)
    }
    if canonical.Source != original.Source || !canonical.CapturedAt.Equal(original.CapturedAt) {
        t.Fatalf("canonical = %#v, want original metadata", canonical)
    }
}
```

同时覆盖响应 Key 无效或不匹配、非法 UTF-8 Key/Schema，以及 Store 返回错误 canonical Key/State。

- [x] **Step 2: 运行测试并确认失败**

Run: `go test ./tooling/snapshot -run '^Test(Manager|MemoryStore)' -count=1`

Expected: Capture 协议仍无 Key、Save 仍只返回 error、nil Payload 仍被归一化，因此测试失败。

- [x] **Step 3: 写最小实现**

复制函数保留 nil：

```go
func cloneState(state State) State {
    if state.Payload == nil {
        return state
    }
    state.Payload = append([]byte{}, state.Payload...)
    return state
}
```

文本验证增加 `utf8.ValidString(value)`。Manager 发送 `CaptureRequest{Key: key}`，先验证原始响应，再保存 candidate；Store 成功后验证 canonical Key、State 和结构，并返回其独立副本。

- [x] **Step 4: 运行重复测试与 Race Detector**

Run:

```bash
go test ./tooling/snapshot -run '^Test(Manager|MemoryStore)' -count=100
go test -race ./tooling/snapshot -run '^Test(Manager|MemoryStore)' -count=20
```

Expected: PASS，Race Detector 无报告。

- [x] **Step 5: 提交模型与存储修复**

```bash
git add tooling/snapshot/types.go tooling/snapshot/validation.go tooling/snapshot/memory_store.go tooling/snapshot/memory_store_test.go tooling/snapshot/manager.go tooling/snapshot/manager_test.go
git commit -m "fix(snapshot): 收紧状态与存储不变量"
```

### Task 3：同步 Codec 与 Service owner

**Files:**

- Modify: `tooling/snapshot/codec.go`
- Modify: `tooling/snapshot/codec_test.go`
- Modify: `tooling/snapshot/remote_test.go`
- Modify: `tooling/snapshot/integration_test.go`
- Modify: `examples/snapshot-runtime/main.go`

- [x] **Step 1: 写带 Key 线格式失败测试**

精确格式改为：

```json
{"key":{"namespace":"player","id":"42"}}
{"key":{"namespace":"player","id":"42"},"state":{"schema":"player","version":1,"revision":2,"payload":"b2s="}}
```

增加 `payload:null`、缺失 payload、非法 UTF-8 wire，以及 Service owner Key 与请求不一致的拒绝测试。

- [x] **Step 2: 运行测试并确认失败**

Run: `go test ./tooling/snapshot -run '^Test(Codec|Remote|Snapshot)' -count=1`

Expected: 旧 wire 类型没有 Key，Service 也未拥有 Key，因此测试失败。

- [x] **Step 3: 实现稳定 wire 类型与 owner 验证**

```go
type wireKey struct {
    Namespace string `json:"namespace"`
    ID        string `json:"id"`
}

type wireCaptureRequest struct {
    Key wireKey `json:"key"`
}

type wireCaptureResponse struct {
    Key   wireKey   `json:"key"`
    State wireState `json:"state"`
}
```

`decodeJSONObject` 在 JSON 解码前拒绝非法 UTF-8。示例、本地测试和远程测试中的 Service 保存稳定 Key，验证请求 Key，并在响应中返回 owner Key；恢复构造函数接收完整 Snapshot。

- [x] **Step 4: 运行测试、Race 与示例**

Run:

```bash
go test ./tooling/snapshot -run '^Test(Codec|Remote|Snapshot)' -count=50
go test -race ./tooling/snapshot -run '^Test(Codec|Remote|Snapshot)' -count=20
go run ./examples/snapshot-runtime
```

Expected: tests PASS；示例输出 `2`。

- [x] **Step 5: 提交协议与示例修复**

```bash
git add tooling/snapshot/codec.go tooling/snapshot/codec_test.go tooling/snapshot/remote_test.go tooling/snapshot/integration_test.go examples/snapshot-runtime/main.go
git commit -m "fix(snapshot): 绑定快照 Key 与状态 owner"
```

### Task 4：建立 RFC 元数据门禁

**Files:**

- Create: `runtime/rfc_document_policy_test.go`
- Modify: `docs/rfcs/*.md`

- [x] **Step 1: 写 RFC policy 失败测试**

测试从仓库根目录读取 `docs/rfcs/*.md`，逐份校验：状态属于精确枚举；`状态`、`范围`、`依赖` 存在且不重复；草案和待实现包含 `目标阶段`；`接受日期` 只用于已接受状态且符合 `YYYY-MM-DD`；RFC 不包含尾随空白。

- [x] **Step 2: 运行测试并确认失败**

Run: `go test ./runtime -run '^TestRFCMetadataPolicy$' -count=1`

Expected: FAIL，报告带日期的状态、缺失依赖字段和尾随空白。

- [x] **Step 3: 迁移全部 RFC 头部**

基础依赖从 `RFC-0000` 向后单向排列；Core RFC 按 ServiceRef、Command、Call、Mailbox、Scheduler、Timer、Lifecycle、Cluster、Inspection 的真实依赖填写；无依赖写 `无`。把带日期的状态改为精确状态，日期移到 `接受日期`。

- [x] **Step 4: 运行 policy 和文档检查**

Run:

```bash
go test ./runtime -run '^TestRFCMetadataPolicy$' -count=10
git diff --check -- docs/rfcs runtime/rfc_document_policy_test.go
```

Expected: PASS；差异检查无输出。

- [x] **Step 5: 提交治理门禁**

```bash
git add runtime/rfc_document_policy_test.go docs/rfcs
git commit -m "test(rfc): 增加元数据一致性门禁"
```

### Task 5：同步文档并完成验收

**Files:**

- Modify: `README.md`
- Modify: `CHANGELOG.md`
- Modify: `docs/GSR-Book/04-第四篇-基础设施/01-Snapshot.md`
- Modify: `docs/rfcs/RFC-0210-Tooling-Snapshot.md`
- Modify: `docs/superpowers/plans/2026-07-22-gsr-phase7d-snapshot-contract-fixes.md`

- [ ] **Step 1: 同步最终语义**

README、CHANGELOG 和 GSR Book 说明：Business Service 拥有 Key；Manager 验证 owner Key；`Store.Save` 返回 canonical Snapshot；nil 和非法 UTF-8 被拒绝。Review 清零后把 RFC-0210 恢复为 `已接受` 并记录独立接受日期。

- [ ] **Step 2: 执行双轴 Review**

Standards 轴检查模块深度、Core 依赖、owner、复制、并发和错误；Spec 轴逐条对照 RFC-0210 的 Key、State、Store、Codec、恢复和验收行为。发现问题先修复，再记录结果。

- [ ] **Step 3: 执行完整质量门禁**

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
git diff --check 5d2a236..HEAD
```

Expected: 全部成功；示例依次包含 `hello`、`hello cluster`、`.config -> node-b/2`、一行 Monitor JSON、`2`；差异检查无输出。

- [ ] **Step 4: 检查占位符和公开类型一致性**

Run:

```bash
rg -n 'TB''D|implement lat''er|similar to ab''ove|类似 Ta''sk' docs/superpowers/plans/2026-07-22-gsr-phase7d-snapshot-contract-fixes.md
rg -n 'CaptureRequest|CaptureResponse|Save\(context.Context, Snapshot\)' docs/rfcs/RFC-0210-Tooling-Snapshot.md tooling/snapshot
```

Expected: 第一条无命中；第二条 RFC 与代码签名一致。

- [ ] **Step 5: 提交验收文档**

```bash
git add README.md CHANGELOG.md docs/GSR-Book/04-第四篇-基础设施/01-Snapshot.md docs/rfcs/RFC-0210-Tooling-Snapshot.md docs/superpowers/plans/2026-07-22-gsr-phase7d-snapshot-contract-fixes.md
git commit -m "docs(snapshot): 完成契约修复验收"
```

## 完成标准

- nil Payload 和非法 UTF-8 在进入 Store 前被稳定拒绝。
- Service 返回自己拥有的稳定 Key，Manager 不再依赖调用者手工保证 target/key 配对。
- `Store.Save` 成功时返回真正保留的独立 canonical Snapshot，Manager 返回值与后续 Load 一致。
- 本地、远程和恢复路径使用同一 Capture Key 契约。
- RFC 状态、接受日期、范围、依赖和尾随空白由自动测试约束。
- Core Runtime 不导入 Snapshot 类型，不新增 Service 接口或旁路。
- 全量测试、vet、Race Detector、重复测试、示例和文档检查全部通过。
