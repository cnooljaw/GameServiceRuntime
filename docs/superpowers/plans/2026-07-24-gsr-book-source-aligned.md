# 源码对齐版 GSR Book Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 依据当前 `runtime/`、`transport/`、`tooling/`、`game/`、`examples/` 源码与已接受 RFC，一次性完成一套可从零阅读、可对照源码、包含完整示例的 GSR Book。

**Architecture:** 全书按“设计动机 → Core Runtime → Cluster → Tooling → Business Layer → 完整实践”展开。每章采用“开发场景—错误方案—设计对话—源码契约—可运行例子—边界小结”的叙事节奏；RFC 负责解释为什么，源码和测试负责展示现在怎么做。

**Tech Stack:** Markdown、Go 1.23.3、GSR Runtime、`go test`、仓库内 RFC 和 examples。

---

## 文件结构

- `docs/GSR-Book/00-前言.md`：读者对象、阅读路线、运行方式和全书故事背景。
- `docs/GSR-Book/01-第一篇-设计理念/*.md`：解释 Service 模型、三层架构、owner 与并发边界。
- `docs/GSR-Book/02-第二篇-Runtime内核/*.md`：逐章对应 `runtime/service.go`、`types.go`、`runtime.go`、`call.go`、`mailbox.go`、`scheduler.go`、`timer.go`、`lifecycle.go`、`inspection.go`。
- `docs/GSR-Book/03-第三篇-Cluster/*.md`：对应 `runtime/cluster.go`、`transport/tcp/`、`tooling/discovery/`、Pending Call 和 `tooling/servicegroup/`。
- `docs/GSR-Book/04-第四篇-基础设施/*.md`：对应 Snapshot、Supervisor、Monitor、入口、Control、Drain、Recovery、Record/Replay 和性能模型。
- `docs/GSR-Book/05-第五篇-游戏层/*.md`：对应 `game/` 的 Battle、Timeline、Room、PlayerModule、Wallet 与 LedgerRunner。
- `docs/GSR-Book/06-第六篇-实践/*.md`：对照 `examples/`、`tests/scenarios/` 和 RFC 工作流完成纵向实战。
- `docs/GSR-Book/SUMMARY.md`：唯一书籍目录和推荐阅读路径。

### Task 1：重写前言与设计理念

**Files:**
- Modify: `docs/GSR-Book/00-前言.md`
- Modify: `docs/GSR-Book/01-第一篇-设计理念/01-为什么不是Actor框架.md`
- Modify: `docs/GSR-Book/01-第一篇-设计理念/02-为什么选择Service.md`
- Modify: `docs/GSR-Book/01-第一篇-设计理念/03-设计原则.md`

- [x] **Step 1: 建立贯穿全书的游戏服务器场景**

用“玩家 Alice 登录、进入房间、开始 Battle、命中目标、结算金币、运维热更”的故事说明共享对象、多把锁、任意 goroutine 和隐式 RPC 为什么难以维护。

- [x] **Step 2: 固定 Service、Command、ServiceRef、owner、Core/Tooling/Business 术语**

术语与 RFC-0000、RFC-0001 和 `docs/DECISIONS.md` 一致，明确 GSR 学习 Skynet 的消息与职责划分，但保留 Go 生命周期和并发硬约束。

### Task 2：重写 Core Runtime

**Files:**
- Modify: `docs/GSR-Book/02-第二篇-Runtime内核/01-Service.md`
- Modify: `docs/GSR-Book/02-第二篇-Runtime内核/02-ServiceRef.md`
- Modify: `docs/GSR-Book/02-第二篇-Runtime内核/03-Command.md`
- Modify: `docs/GSR-Book/02-第二篇-Runtime内核/04-Mailbox.md`
- Modify: `docs/GSR-Book/02-第二篇-Runtime内核/05-Scheduler.md`
- Modify: `docs/GSR-Book/02-第二篇-Runtime内核/06-Timer.md`
- Modify: `docs/GSR-Book/02-第二篇-Runtime内核/07-Lifecycle.md`
- Modify: `docs/GSR-Book/02-第二篇-Runtime内核/08-Inspection.md`

- [x] **Step 1: 用最小 CounterService 贯穿创建、Send、Call、Reply 和 After**

示例直接使用当前公开接口，解释 `CommandDeclarer`、`ServiceContext`、`CommandContext`、`ServiceSpec` 和 `Runtime`。

- [x] **Step 2: 解释 Mailbox、Scheduler、Call 许可、Timer Command 和任务追踪**

对照源码描述串行不变量、同步调用环拒绝、超时不等于取消、Timer 不执行回调、Stop/Close 不并发。

- [x] **Step 3: 用 Inspect 和 benchmark 说明如何观测及何时优化**

只通过 `Runtime.Inspect()` 获取快照；说明当前微秒级基准的适用范围，不承诺生产容量。

### Task 3：重写 Cluster 与寻址

**Files:**
- Modify: `docs/GSR-Book/03-第三篇-Cluster/01-Cluster.md`
- Modify: `docs/GSR-Book/03-第三篇-Cluster/02-Transport.md`
- Modify: `docs/GSR-Book/03-第三篇-Cluster/03-Discovery.md`
- Modify: `docs/GSR-Book/03-第三篇-Cluster/04-Session.md`
- Modify: `docs/GSR-Book/03-第三篇-Cluster/05-ServiceGroup.md`

- [x] **Step 1: 写清本地和远程 Envelope 的统一语义**

对照 `NewClusterRuntime`、`ClusterTransport`、`ClusterCodec`、TCP framing、remote Reply 和错误编码。

- [x] **Step 2: 区分 ResolveRemote、Discovery 和 ServiceGroup**

使用已知节点长期名、动态节点 lease、版本化 ServiceSet 三个例子，说明三类事实不能混在一个目录。

- [x] **Step 3: 解释 Session、超时、迟到 Reply 和路由策略**

说明 Session 只关联 Call/Reply；业务幂等使用 RequestID。给出 Hash、RoundRobin、Broadcast 示例。

### Task 4：补齐 Tooling 工程化能力

**Files:**
- Modify: `docs/GSR-Book/04-第四篇-基础设施/01-Snapshot.md`
- Modify: `docs/GSR-Book/04-第四篇-基础设施/02-Supervisor.md`
- Modify: `docs/GSR-Book/04-第四篇-基础设施/03-Monitor.md`
- Modify: `docs/GSR-Book/04-第四篇-基础设施/04-Performance.md`
- Modify: `docs/GSR-Book/04-第四篇-基础设施/05-客户端入口.md`
- Create: `docs/GSR-Book/04-第四篇-基础设施/06-ControlPlane.md`
- Create: `docs/GSR-Book/04-第四篇-基础设施/07-Drain与恢复.md`
- Create: `docs/GSR-Book/04-第四篇-基础设施/08-RecordReplay.md`

- [x] **Step 1: 完成 Snapshot、Supervisor、Monitor 和客户端入口教程**

每章给出源码角色、组合根示例、失败边界和不可解决的问题。

- [x] **Step 2: 完成 Observer、NodeAgent、Drain、Stop 和人工恢复流程**

用状态推进例子解释 observed state、desired state、入口 guard、visitor lease、版本发布、外部 runner 与 RequestID。

- [x] **Step 3: 完成 Command Record/Replay 和性能测量**

解释 decorator、有界 recorder、外部 archive、隔离 replay，以及 Send/Call/Timer/Battle 基准的正确读法。

### Task 5：重写 Business Layer

**Files:**
- Modify: `docs/GSR-Book/05-第五篇-游戏层/01-GameLayer.md`
- Modify: `docs/GSR-Book/05-第五篇-游戏层/02-Battle.md`
- Modify: `docs/GSR-Book/05-第五篇-游戏层/03-Timeline.md`
- Modify: `docs/GSR-Book/05-第五篇-游戏层/04-Room.md`
- Modify: `docs/GSR-Book/05-第五篇-游戏层/05-Player.md`
- Modify: `docs/GSR-Book/05-第五篇-游戏层/06-Wallet.md`

- [x] **Step 1: 解释业务模板不是另一套 Runtime**

直接保留 Send、Call、Reply；BattleContext 和 PlayerContext 只增加领域能力，Room 和 Wallet 不创建浅包装。

- [x] **Step 2: 给出 Battle、Timeline、Room、PlayerModule 完整例子**

覆盖 Context 有效期、Epoch/Revision fencing、RequestID 幂等、重连和 Snapshot 副本。

- [x] **Step 3: 给出 Wallet 与 LedgerRunner 异步收敛例子**

说明资金事实属于 LedgerStore，外部 I/O 由组合根 runner 持有，结果以 Command 返回 Wallet。

### Task 6：完成实战、目录和协作指南

**Files:**
- Modify: `docs/GSR-Book/06-第六篇-实践/01-打地鼠实战.md`
- Modify: `docs/GSR-Book/06-第六篇-实践/02-Codex开发指南.md`
- Modify: `docs/GSR-Book/06-第六篇-实践/03-开发路线图.md`
- Modify: `docs/GSR-Book/SUMMARY.md`

- [x] **Step 1: 从组合根到 Battle 完成打地鼠纵向切片**

按 `examples/whackmole` 展示 Send 启动、Call Kick、Timeline、Finish、Wallet、Record/Replay、场景测试和 benchmark。

- [x] **Step 2: 写出源码阅读、RFC 修改和测试归位流程**

明确单元测试留在包内，跨包流程进入 `tests/scenarios/`，RFC 先于代码，决策索引不替代契约。

- [x] **Step 3: 重建 SUMMARY**

目录顺序与源码依赖方向一致；每篇给出适合读者与可运行命令。

### Task 7：验证并提交

**Files:**
- Modify: 上述全部 GSR Book 与计划文件

- [x] **Step 1: 检查占位、旧术语和断链**

Run: `rg -n '固定结构|全书统一术语|TODO|TBD|ActorRef|Ask|Tell|Spawn' docs/GSR-Book`

Expected: 禁止术语只在“为什么不是 Actor 框架”中作为反例出现，不存在模板占位或未完成标记。

- [x] **Step 2: 运行文档和代码质量门禁**

Run: `go test ./...`

Expected: PASS，包含 RFC 文档策略测试和所有 examples/scenarios。

Run: `go vet ./...`

Expected: PASS。

Run: `go test -race ./...`

Expected: PASS。

- [x] **Step 3: 提交完整书稿**

Run: `git add docs/GSR-Book docs/superpowers/plans/2026-07-24-gsr-book-source-aligned.md`

Run: `git commit -m "docs(book): 重写源码对齐版 GSR Book"`

Expected: 创建一个中文文档提交，工作区干净。
