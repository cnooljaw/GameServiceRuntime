# GSR Phase 10C2B 人工恢复与补偿实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` task-by-task. 每个任务先写失败测试，再用最小实现通过测试；不要合并任务的提交。

**Goal:** 实现 RFC-0274 的人工、可审计恢复：替代实例只能由组合根 RecoveryRunner 创建，只有操作者 Confirm 后才以 Directory CAS 发布新 ServiceSet，旧 Guard/Stopped Ref 永不恢复。

**Architecture:** `DrainCoordinatorService` 拥有 RecoveryOperation 和审计；每个目标 NodeAgent 拥有 `(CoordinatorRef, RequestID, RemovedRef)` RecoveryReceipt；组合根拥有固定 worker 的 RecoveryRunner 和只读 BlueprintRegistry。Runner 只 CreateService 并以私有 Command 回报；Coordinator 读取 Directory、接受人工 Confirm、执行 CAS 和 Resolve，不能持有 Runtime 或 Blueprint closure。

**Tech Stack:** Go 1.23.3、标准库 `context`/`sync`/`time`/`encoding/json`、现有 `tooling/control`、`tooling/servicegroup`、TDD、Race Detector。

## Task 1：补齐公开类型、错误、Command 和 wire 形状

**Files:**

- Modify: `tooling/control/commands.go`
- Modify: `tooling/control/types.go`
- Modify: `tooling/control/errors.go`
- Modify: `tooling/control/validation.go`
- Modify: `tooling/control/codec.go`
- Test: `tooling/control/recovery_types_test.go`
- Test: `tooling/control/codec_test.go`

- [ ] 先写表驱动失败测试：空/空白 Blueprint、零 Ref、重复 Removed、Agent.Node 不匹配、RequestID 冲突、无效 Phase/Failure/Target 以及所有 slice/map/ServiceSet 的深复制必须失败或不泄漏内部状态。
- [ ] 定义 RFC-0274 的 `BlueprintID`、RecoveryTarget/Phase/Failure/Operation/Request/Receipt/Task、`RecoveryExecutor`、`BlueprintRegistry`、Runner config 与导出错误；每个导出符号有 Go doc。
- [ ] 将 `0x02500104/05/fc` 和 `0x02500308..0c` 加入 Commands、request/response 结构、`controlPayload`、`validWireResponse` 与 `validResponseCode`。私有 result Command 的 Codec 必须拒绝。
- [ ] 运行 `go test ./tooling/control -run 'Test(RecoveryTypes|Codec.*Recovery)' -count=100`，再运行同一选择器的 `-race -count=20`。
- [ ] Commit: `feat(control): 定义人工恢复操作协议`。

## Task 2：实现组合根 BlueprintRegistry 与 RecoveryRunner

**Files:**

- Create: `tooling/control/recovery_runner.go`
- Create: `tooling/control/recovery_registry.go`
- Test: `tooling/control/recovery_runner_test.go`

- [ ] 先写失败测试，覆盖：Config nil/typed-nil Registry、非正 Workers/QueueSize、无效 task、队列满、重复 Submit、未知 Blueprint、CreateService 成功/失败、result Send 失败、Close 前后 Submit、Close 超时和 Close 等待 worker 真正返回。
- [ ] 提供 `MapBlueprintRegistry`：构造时复制 map，`Build` 每次调用 factory 并验证得到的 `ServiceSpec` 为新实例、Name 为空、Service 非 nil；它不导出/序列化 factory。
- [ ] 实现固定 worker RecoveryRunner。`Submit` 非阻塞并有界；worker 对每 task 只 Build/Create 一次，随后向目标 NodeAgent 发送私有 `RecordRecoveryCreate`；Runner 不 Publish/Stop/重试。Close 先拒绝新任务，取消未开始队列，等待已开始 CreateService 返回；调用方 context 超时只结束等待，不假装 worker 已退出。
- [ ] 运行 `go test ./tooling/control -run '^TestRecoveryRunner' -count=100` 与 `go test -race ./tooling/control -run '^TestRecoveryRunner' -count=30`。
- [ ] Commit: `feat(control): 增加有界恢复创建执行器`。

## Task 3：扩展 NodeAgent 的 RecoveryReceipt

**Files:**

- Modify: `tooling/control/node_agent.go`
- Modify: `tooling/control/types.go`
- Modify: `tooling/control/validation.go`
- Test: `tooling/control/node_agent_recovery_test.go`

- [ ] 写失败测试：只设置 Coordinator/Executor 之一拒绝；非精确 Coordinator source、跨节点目标、重复 Begin、冲突 Blueprint、迟到/伪造 result、未知 Get、Runner queue full、Runner Close 和 receipt 返回副本。
- [ ] 在 NodeAgentConfig 增加成对 `RecoveryCoordinator/RecoveryExecutor`；以 `(CoordinatorRef, RequestID, Removed)` 保存 receipt。Begin 只调用 Executor.Submit 并转为 creating/failed；private result 只接受本地 Runner 的允许 source（组合根 Runtime caller），且 immutable request 必须匹配。
- [ ] 实现 BeginRecoveryCreate/GetRecoveryReceipt/RecordRecoveryCreate command 分派与 JSON response；保留既有 Node Stop 行为和只读 NodeAgent 配置兼容性。
- [ ] 运行 `go test ./tooling/control -run '^TestNodeAgentRecovery' -count=100` 和 race 测试。
- [ ] Commit: `feat(control): 记录本地恢复创建回执`。

## Task 4：实现 Coordinator RecoveryOperation 和 typed Client

**Files:**

- Modify: `tooling/control/drain_service.go`
- Modify: `tooling/control/drain_client.go`（或现有 DrainClient 定义文件）
- Modify: `tooling/control/validation.go`
- Test: `tooling/control/drain_recovery_test.go`

- [ ] 先写测试：Gateway/Principal 拒绝、同 RequestID 幂等/冲突、没有匹配 StopOperation、Expected 与 Directory 不同、每个 Target 的 NodeAgent receipt、创建失败、所有 Created 后 AwaitingConfirmation、Confirm 前不可 Publish、CAS 冲突/网络未知需 Resolve、Abandon 不能 Stop、Completed/Failed/Abandoned 不可变与审计上限。
- [ ] 引入 RecoveryOperation 私有 record 与 audit；Begin 规范化目标、核对被移除 Ref 都来自该 Principal 的终态 StopOperation、确认 Directory 等于 Expected，然后向每 NodeAgent Call BeginRecoveryCreate。任何未知 Call 仅保持 creating，Resolve 用 Get receipt/Directory 收敛。
- [ ] Confirm 只在全 Target Created 时构造“Expected 中 Removed 替为 Created”的候选 ServiceSet，调用 Directory CAS。成功后记录 Published；失败/未知绝不猜测结果。Abandon 只记录终态。Get/Resolve/Confirm 全部返回深拷贝。
- [ ] 在 DrainClient 增加 Begin/Confirm/Resolve/Get/AbandonRecovery，响应类型和错误码必须严格校验。
- [ ] 运行 `go test ./tooling/control -run '^TestDrainRecovery' -count=100` 与 `go test -race ./tooling/control -run '^TestDrainRecovery' -count=30`。
- [ ] Commit: `feat(control): 编排人工恢复并确认发布`。

## Task 5：端到端、关闭和文档验收

**Files:**

- Test: `tooling/control/recovery_integration_test.go`
- Test: `tests/integration/recovery_cluster_test.go`
- Modify: `README.md`
- Modify: `CHANGELOG.md`
- Modify: `docs/TODO.md`
- Modify: `docs/rfcs/RFC-0274-Tooling-Manual-Recovery-Compensation.md`
- Modify: `docs/rfcs/RFC-0500-Roadmap.md`

- [ ] 本地集成：Drain/Stop 后创建新实例；Confirm 前 Router 仍不指向新 Ref；Confirm 后 ServiceSet version 增加且只含新 Ref；旧 Ref 仍被 Guard/Closed；外层显式 Stop abandoned 新实例。
- [ ] 双节点 TCP：Runner 在目标 Node 创建替代 Service，Coordinator 跨节点读取 receipt 并发布 Directory；断线未知后 Resolve 收敛。
- [ ] 增加 AST/泄漏断言，确保 control Service 无直接 `go`，RecoveryRunner Close 等待 worker；用 `-count=20` 跑双节点场景。
- [ ] 通过后将 RFC-0274 标为已接受并写接受/实现日期，更新 README/CHANGELOG/路线图/TODO；明确它不实现自动 Reconcile 或生产 HA 审计。
- [ ] 运行全量门禁：`go test ./...`、`go vet ./...`、`go test -race ./...`。
- [ ] Commit: `docs(control): 完成 C2B 人工恢复验收`。
