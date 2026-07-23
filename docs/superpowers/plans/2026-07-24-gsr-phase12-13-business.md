# GSR Phase 12–13 Business 模板与打地鼠示例实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` task-by-task. 不可跳过失败测试、Runner Close 或跨 Service 迟到结果测试。

**Goal:** 实现 `game` 最小 Business Layer（Battle、Timeline、Room、Player/Module、Wallet）及 `examples/whackmole`，证明完整 Command/TImer/结算/外层 Stop/Replay 流程，不污染 Core 或 Tooling。

**Architecture:** `game` 只依赖 `runtime` 和标准库。Battle Mailbox 拥有一局和 Timeline，Room Mailbox 拥有索引，Player Mailbox 拥有单玩家与 Module，Wallet Mailbox 拥有 RequestID 状态。外部 LedgerRunner 是组合根拥有的固定 worker；示例调用 CreateBattle/Stop 只在组合根或显式 Factory 边界。

**Tech Stack:** Go 1.23.3、标准库、现有 Runtime/Record Tooling、TDD、AST goroutine guard、Race Detector。

## Task 1：建立 game 基础模型与错误边界

**Files:**

- Create: `game/errors.go`
- Create: `game/types.go`
- Create: `game/validation.go`
- Test: `game/types_test.go`

- [ ] 写失败测试：所有 ID/RequestID、Ref、Currency、Participant、deep clone、Request canonical ordering 和 typed nil validation。
- [ ] 实现 RFC-0300 的 ID、RequestID、Creator/Runtime 接口、导出稳定错误、保留 CommandID 分段以及 clone/validate helper；每个导出符号写 Go doc。
- [ ] 运行 `go test ./game -run '^Test(Type|Validation)' -count=100` 与 race；Commit: `feat(game): 定义业务层基础类型`。

## Task 2：实现 Timeline 与 Battle 容器

**Files:**

- Create: `game/timeline.go`
- Create: `game/battle.go`
- Test: `game/timeline_test.go`
- Test: `game/battle_test.go`

- [ ] 先写 Timeline tests：After/At、Cancel、Replace、Core After failure、旧 Revision/Epoch、重复 fire、finished cleanup、排序 Snapshot 和 payload clone。
- [ ] 先写 Battle tests：config/Logic command conflict、Start、participant connection、Broadcast partial failure、custom logic dispatch、GetSnapshot、Finish RequestID、Wallet Send failure、delayed/repeated settlement result 与终态拒绝。
- [ ] 实现 BattleContext 仅在 Handle 有效；private TimelineFire 验证后再把原 Command 给 Logic。CreateBattle 使用 ServiceCreator 但不在 Handler 访问它。Finish 只 Send Wallet，绝不 Call/Stop。
- [ ] 运行 battle/timeline tests `-count=100` 和 `-race -count=30`；Commit: `feat(game): 增加 Battle 与 Timeline 模板`。

## Task 3：实现 Room 与 Player/Module

**Files:**

- Create: `game/room.go`
- Create: `game/player.go`
- Create: `game/module.go`
- Test: `game/room_test.go`
- Test: `game/player_test.go`
- Test: `game/module_test.go`

- [ ] Room 失败测试覆盖 join/leave/capacity、RequestID idempotency/conflict、factory failure、created/finished source/ref fencing、snapshot clone 与 closed 行为。
- [ ] Player/Module 失败测试覆盖 identity/generation fencing、online/offline event order、module name/command conflict、module failure、room/battle binding idempotency、backup/snapshot clone、reconnect result 迟到与无 goroutine。
- [ ] 实现 Room 只索引 Battle、Factory result Command 才写入；Player 只编排 modules/identity，Module Handle/Event/Snapshot 均在 Player Mailbox。
- [ ] 运行 `go test ./game -run '^Test(Room|Player|Module)' -count=100` 及 race；Commit: `feat(game): 增加房间和玩家业务模板`。

## Task 4：实现 Wallet、MemoryLedgerStore 与 LedgerRunner

**Files:**

- Create: `game/wallet.go`
- Create: `game/ledger_runner.go`
- Create: `game/memory_ledger.go`
- Test: `game/wallet_test.go`
- Test: `game/ledger_runner_test.go`
- Test: `game/memory_ledger_test.go`

- [ ] 先写 Store conformance tests：相同 RequestID 同结果、冲突拒绝、原子 balance/entries、Lookup restart fake、copy isolation。
- [ ] 写 Runner/Wallet tests：queue full、Close 等待、Store reject/timeout/unknown、Lookup recovery、伪造 private result、迟到结果、Source notification、Battle 不在 Call timeout 下重复结算。
- [ ] 实现 Wallet pending/terminal map，Runner 先 Lookup 后 Commit 并用 private Command 回报；MemoryLedger 仅测试/示例可配置。生产构造路径拒绝 MemoryLedger；没有 Handler 内 Store I/O。
- [ ] 运行 Wallet 选择器 `-count=100`、race `-count=30`；Commit: `feat(game): 增加异步幂等账本结算`。

## Task 5：实现打地鼠示例和 Record/Replay 场景

**Files:**

- Create: `examples/whackmole/main.go`
- Create: `examples/whackmole/service.go`
- Test: `examples/whackmole/service_test.go`
- Test: `tests/scenarios/whackmole_flow_test.go`
- Test: `tests/scenarios/whackmole_replay_test.go`

- [ ] 先写失败场景：Room create → Start → controlled Timeline spawn/expire → concurrent kick only once → Finish → Wallet result → Room finish → composition-root Stop；旧 Epoch、重复 timer/finish、Wallet reject/unknown 和 reconnect snapshot。
- [ ] 实现固定 seed WhackMoleLogic、Command mapper fake、Snapshot projection；示例不使用 sleep，以 test clock/显式 TimelineFire 进行时间推进。
- [ ] 记录一段 Battle input、在全新 Runtime/Service 回放，断言 Score/Shrew 终态相同；验证 Core/Tooling 不 import examples。
- [ ] 运行 scenario `-count=50`、race `-count=20` 和 `go run ./examples/whackmole`；Commit: `feat(example): 增加打地鼠端到端示例`。

## Task 6：文档、架构与全量验收

**Files:**

- Modify: `README.md`
- Modify: `CHANGELOG.md`
- Modify: `docs/TODO.md`
- Modify: `docs/GSR-Book/05-第五篇-游戏层/*.md`
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

- [ ] 运行 `go list -deps ./runtime ./tooling/... | rg '/game|/examples'`，预期无匹配；运行 AST 规则确保 Service 与 Module 无直接 go。
- [ ] 将已完成的 RFC-0300 至 0400 标为已接受、更新书籍/README/TODO/CHANGELOG，并明确生产 LedgerStore、认证协议、自动 Controller/Reconcile 仍不属于示例。
- [ ] 全量门禁：`go test ./...`、`go vet ./...`、`go test -race ./...`，对 scenarios 执行 `-count=20`；`git diff --check` 必须为空。
- [ ] Commit: `docs(game): 完成业务模板和示例验收`。
