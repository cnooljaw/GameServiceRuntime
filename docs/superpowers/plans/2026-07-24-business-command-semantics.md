# 业务层 Command 语义统一 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 统一 Battle、Player、Room 与 Wallet 对 `Send`、`Call`、`Reply` 和 Context 生命周期的调用语义，并在打地鼠示例中展示非阻塞启动、同步查询与跨 Battle 性能基准。

**Architecture:** GSR 保持 Skynet 式显式消息原语，不新增隐藏 `Send`、`Call`、`Reply` 的领域 RPC 层。`BattleContext` 与 `PlayerContext` 继续包含当前 Command 语义，并添加领域能力；其实现仅在当前 Handler 内可写。Room 和 Wallet 没有可插拔 Logic/Module 边界，直接使用同一 `gsr.CommandContext` 语义与共享 `reply` 归一化函数。

**Tech Stack:** Go 1.23.3、标准库、GSR Runtime、`go test`、`go vet`、race detector。

---

## 文件结构

- `game/errors.go`：新增 Context 过期的稳定业务错误。
- `game/types.go`、`game/module.go`：记录 Battle/Player 扩展 Context 的统一语义，并使 Broadcast 能报告 Context 生命周期失败。
- `game/battle.go`、`game/timeline.go`、`game/player.go`、`game/wallet.go`：重命名私有 Runtime 能力字段，统一 Reply 和可写 Context 的有效期检查。
- `game/battle_test.go`、`game/room_player_test.go`：通过生产的 Battle/Player 扩展 seam 验证 Send/Reply 和 Context 过期行为。
- `examples/whackmole/main.go`、`examples/whackmole/service.go`、`examples/whackmole/service_test.go`、`examples/whackmole/benchmark_test.go`：展示 Send 启动、Call 查询，验证顺序，并建立单局与多局基准。
- `docs/rfcs/RFC-0300-Business-Layering.md`、`RFC-0310`、`RFC-0330`、`RFC-0340`、`RFC-0350`、`RFC-0360`、`RFC-0370`、`RFC-0400`：冻结业务层调用与热点性能契约。
- `docs/DECISIONS.md`、`docs/TODO.md`、`docs/rfcs/RFC-0500-Roadmap.md`：记录可复用裁决、后续性能测量与路线图状态。

### Task 1: 写入业务调用契约

**Files:**
- Modify: `docs/rfcs/RFC-0300-Business-Layering.md`
- Modify: `docs/rfcs/RFC-0310-Business-Battle.md`
- Modify: `docs/rfcs/RFC-0330-Business-Room.md`
- Modify: `docs/rfcs/RFC-0340-Business-PlayerService.md`
- Modify: `docs/rfcs/RFC-0350-Business-PlayerModule.md`
- Modify: `docs/rfcs/RFC-0360-Business-WalletService.md`
- Modify: `docs/rfcs/RFC-0370-Business-Templates.md`
- Modify: `docs/rfcs/RFC-0400-Example-Whack-Mole.md`
- Modify: `docs/DECISIONS.md`
- Modify: `docs/TODO.md`
- Modify: `docs/rfcs/RFC-0500-Roadmap.md`

- [x] **Step 1: 写入权威语义**

在 RFC-0300 定义：`Send` 是非阻塞投递；`Call` 等待当前 Command 的 Reply；业务 Handler 可以尝试 Reply，同一 Command 经 Send 到达时 Reply 必须成为成功无副作用。Context 只在当前 Handler 有效，Context 过期后的 Send、Reply、Finish、Broadcast 和 Timeline 调度返回 `ErrContextExpired` 或不产生效果。

- [x] **Step 2: 写入模板边界与性能规则**

在 Battle/Player RFC 说明扩展 Logic/Module 接收的是“当前 Command + 领域能力”，不是 Runtime 控制面；Room/Wallet 不增加无收益的 Context 包装。记录单局 Mailbox 是串行热点，优化顺序为缩短 Handler、异步外部 I/O、按 Battle 分片、再以真实 p95/p99/队列指标决定是否拆 owner；不以业务 goroutine 绕过边界。

- [x] **Step 3: 写入决策、待办和路线图索引**

新增决策索引项，链接 RFC-0300/0310/0350/0370；在 TODO 增加业务热点压测、allocation 与广播策略测量项；在路线图 Phase 12 状态中加入本次契约收敛。

### Task 2: Battle Context 先写失败测试

**Files:**
- Modify: `game/battle_test.go`
- Modify: `game/types.go`
- Modify: `game/errors.go`
- Modify: `game/battle.go`
- Modify: `game/timeline.go`

- [x] **Step 1: 写入失败测试**

增加一个 `BattleLogic`，在由 Send 到达的自定义 Command 内调用 `ctx.Reply("accepted")`；使用 Reply 返回 `gsr.ErrReplyUnavailable` 的真实测试 Context，断言 `BattleService.Handle` 返回 nil。再增加一个保存 `BattleContext` 的 Logic，在 Handler 返回后断言：

```go
if err := saved.Reply("late"); !errors.Is(err, ErrContextExpired) { t.Fatal(err) }
if err := saved.Send(target, commandBattleTestSchedule, nil); !errors.Is(err, ErrContextExpired) { t.Fatal(err) }
if _, err := saved.Broadcast(commandBattleTestSchedule, nil); !errors.Is(err, ErrContextExpired) { t.Fatal(err) }
if _, err := saved.Timeline().After(time.Second, commandBattleTestSchedule, nil); !errors.Is(err, ErrContextExpired) { t.Fatal(err) }
```

- [x] **Step 2: 验证测试失败**

Run: `go test ./game -run 'TestBattleContext(ReplyAllowsSend|RejectsEffectsAfterHandler)$' -count=1`

Expected: FAIL，因为现有 `Reply` 直接返回 `ErrReplyUnavailable`，且 `Broadcast` 尚未返回 error，过期 Context 仍可 Send。

- [x] **Step 3: 最小实现**

新增：

```go
// ErrContextExpired indicates a BattleContext or PlayerContext escaped its Handler.
ErrContextExpired = errors.New("game: context expired")
```

将私有 `battleCommandContext` 改为 `battleContext`，将 `BattleService.context` 改为 `service`。`Reply` 调用共享 `reply`；`Send`、`Finish`、`Broadcast` 和 Timeline 的可写方法先检查 active。公开 API 调整为：

```go
Broadcast(gsr.CommandID, any) (BroadcastResult, error)
```

Timeline 在过期时返回 `ErrContextExpired`，`Cancel` 返回 false，`Snapshot` 返回零快照。

- [x] **Step 4: 验证测试通过**

Run: `go test ./game -run 'TestBattleContext(ReplyAllowsSend|RejectsEffectsAfterHandler)$' -count=1`

Expected: PASS。

### Task 3: Player Module Context 先写失败测试

**Files:**
- Modify: `game/room_player_test.go`
- Modify: `game/module.go`
- Modify: `game/player.go`

- [x] **Step 1: 写入失败测试**

增加一个模块，其 `Handle` 只执行 `ctx.Reply("accepted")`。用 Reply 返回 `gsr.ErrReplyUnavailable` 的 CommandContext 将模块 Command 投递到 PlayerService，断言 `Handle` 成功。增加保存 Context 的模块，Handler 返回后调用 `saved.Send`，断言得到 `ErrContextExpired` 且测试 ServiceContext 没有记录投递。

- [x] **Step 2: 验证测试失败**

Run: `go test ./game -run 'TestPlayerContext(ReplyAllowsSend|RejectsSendAfterHandler)$' -count=1`

Expected: FAIL，因为现有 `playerCommandContext.Reply` 直接透传 Runtime 错误，过期 Context 仍可 Send。

- [x] **Step 3: 最小实现**

将 `playerCommandContext` 改为 `playerContext`，`initPlayerCommandContext` 改为 `initPlayerContext`，`PlayerService.context` 改为 `service`。`playerContext.Reply` 使用共享 `reply`；`playerContext.Send` 在 active 为 false 时返回 `ErrContextExpired`。保持 `Self`、`Source`、`Now` 的只读值语义不变。

- [x] **Step 4: 验证测试通过**

Run: `go test ./game -run 'TestPlayerContext(ReplyAllowsSend|RejectsSendAfterHandler)$' -count=1`

Expected: PASS。

### Task 4: Room/Wallet 和示例统一调用方式

**Files:**
- Modify: `game/wallet.go`
- Modify: `examples/whackmole/main.go`
- Modify: `examples/whackmole/service.go`
- Modify: `examples/whackmole/service_test.go`

- [x] **Step 1: 写入失败测试**

为 WhackMole 增加测试：先 `Runtime.Send` 通用 Battle Start 和玩法 Start，再对 Kick 使用 `Runtime.Call`，断言结果是 `KickResult{Hit:true, Score:1}`。这证明同一 Mailbox 中 Send 启动和随后 Call 查询的顺序可见，且玩法 Logic 不需要本地 `whackReply`。

- [x] **Step 2: 验证测试失败**

Run: `go test ./examples/whackmole -run TestWhackMoleSendStartThenCallKick -count=1`

Expected: FAIL，因为示例尚未提供该测试路径，且 Logic 仍有独立 Reply 归一化 helper。

- [x] **Step 3: 最小实现**

将 WalletService 的私有 Runtime 字段 `context` 改为 `service`。示例主程序改为 Send 两个启动 Command、Call Kick；删除 `whackReply`，由 `BattleContext.Reply` 统一处理 Send/Call 的 Reply 差异。测试使用生产 Runtime 的 Send/Call，而不是直接调用 Logic。

- [x] **Step 4: 验证测试通过**

Run: `go test ./examples/whackmole -run TestWhackMoleSendStartThenCallKick -count=1`

Expected: PASS。

### Task 5: 建立 Battle 热点测量基线

**Files:**
- Create: `examples/whackmole/benchmark_test.go`

- [x] **Step 1: 编写基准**

创建两个 benchmark：`BenchmarkKickSingleBattle` 对一个已启动 Battle 连续 Call Kick；`BenchmarkKickAcrossBattles` 在四个 Runtime Workers 和 64 个已启动 Battle 上用 `b.RunParallel` 轮转 Call Kick。两者都使用已过期/已命中的 Shrew，避免 timer 和结算 I/O 干扰，只测 Mailbox、Call/Reply 和 Logic 状态读取路径。

- [x] **Step 2: 运行基准并记录命令**

Run: `go test ./examples/whackmole -run '^$' -bench '^BenchmarkKick' -benchmem -count=1`

Expected: 两个 benchmark 均通过；结果仅作为本机基线，不把一次运行的数字写入 RFC 结论。

### Task 6: 格式化、完整验证并提交

**Files:**
- Modify: 上述全部文件

- [x] **Step 1: 格式化与针对性测试**

Run: `gofmt -w game/*.go examples/whackmole/*.go && go test ./game ./examples/whackmole`

Expected: PASS。

- [x] **Step 2: 全量质量门禁**

Run:

```bash
go test ./...
go vet ./...
go test -race ./...
go run ./examples/whackmole
```

Expected: 全部 PASS；示例输出一次成功 Kick。

- [x] **Step 3: 检查变更并提交**

Run: `git diff --check && git status --short && git add docs game examples/whackmole && git commit -m "feat(game): 统一业务 Command 调用语义"`

Expected: 无空白错误，只有本切片文件被暂存并生成中文提交。
