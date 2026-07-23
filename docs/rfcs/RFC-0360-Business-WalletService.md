# RFC-0360：WalletService 设计

> 状态：待实现
> 目标阶段：Phase 12
> 范围：Business Layer、Persistence
> 依赖：[RFC-0300](RFC-0300-Business-Layering.md)、[RFC-0310](RFC-0310-Business-Battle.md)、[RFC-0210](RFC-0210-Tooling-Snapshot.md)
> 依据：资金变更先持久化为幂等账本事实，再以 Command 推进业务可见状态

## 目的

WalletService 是余额、结算请求和幂等结果的业务 owner。它为 Battle 等领域提供异步、可查询的结算接口：账本 writer 成功提交不可变流水后，Wallet 才将结果标记 committed 并向发起 Battle 发送结果 Command。它不把余额备份降级为 Player Snapshot，也不预设某个数据库。

## 目标

- 定义以 RequestID 为键的增减余额结算、GetSettlement 与 GetBalance Command。
- 由组合根拥有固定上限 LedgerRunner 和 `LedgerStore`；Wallet Handler 只提交任务、记录 pending、接收私有结果 Command。
- 要求 Store 的 Commit 原子地持久化“幂等键 + ledger entries + 结果”，并支持崩溃后的 Lookup。
- 提供内存 Store 供测试/示例；生产部署必须注入持久 Store，并自行负责加密、权限、事务和合规保留。

## 非目标

- 不提供信用额度、货币换算、支付通道、退款策略、双录记账法规、数据库迁移、跨 Wallet 分布式事务或自动人工补偿。
- 不用 Snapshot、Record、PlayerModule 定期 Backup 作为余额/幂等结果的权威来源。

## 分层与依赖

```text
BattleService --CommitSettlement(RequestID)--> WalletService owns request state
WalletService -> LedgerRunner.Submit (bounded, non-blocking)
LedgerRunner -> LedgerStore.Commit / Lookup (external I/O)
LedgerRunner -> private ApplyLedgerResult Command -> WalletService
WalletService -> ApplySettlementResult Command -> requesting Battle
```

WalletService 从不持有数据库连接或启动 I/O goroutine。LedgerRunner 是组合根的外部 lifecycle owner；Store 不 import Runtime。Battle 不直接修改余额，只根据 SettlementResult 推进自身状态。

## 公开契约

包路径为 `game`：

```go
type Currency string
type Amount int64
type SettlementState string
const (
    SettlementPending   SettlementState = "pending"
    SettlementCommitted SettlementState = "committed"
    SettlementRejected  SettlementState = "rejected"
)
type Balance struct { Player PlayerID; Currency Currency; Amount Amount }
type SettlementEntry struct { Player PlayerID; Delta Amount }
type SettlementRequest struct {
    RequestID RequestID
    Source    gsr.ServiceRef
    Currency  Currency
    Entries   []SettlementEntry
}
type SettlementResult struct {
    RequestID RequestID
    State     SettlementState
    Currency  Currency
    Balances  []Balance
    Reason    string
}
type LedgerRecord struct { Request SettlementRequest; Result SettlementResult }
type LedgerStore interface {
    Commit(context.Context, LedgerRecord) (SettlementResult, error)
    Lookup(context.Context, RequestID) (SettlementResult, bool, error)
}
type LedgerTask struct { Wallet, Source gsr.ServiceRef; Request SettlementRequest }
type LedgerExecutor interface { Submit(LedgerTask) error }
type LedgerRuntime interface {
    Send(gsr.ServiceRef, gsr.CommandID, any) error
}
type LedgerRunnerConfig struct {
    Store     LedgerStore
    Workers   int
    QueueSize int
    Timeout   time.Duration
}
func NewLedgerRunner(LedgerRuntime, LedgerRunnerConfig) (*LedgerRunner, error)
func (*LedgerRunner) Submit(LedgerTask) error
func (*LedgerRunner) Close(context.Context) error
type WalletConfig struct { Executor LedgerExecutor; MaxPending int }
func NewWalletService(WalletConfig) (*WalletService, error)
func NewMemoryLedgerStore() *MemoryLedgerStore
```

一个 SettlementRequest 必须有非空 RequestID、具体 Source、非空 Currency、`1..256` 个不重复 Player 的非零 Entry，并按 PlayerID 规范排序；同一 RequestID 的完全相同规范化请求返回相同状态/结果，任何字段不同为 `ErrRequestConflict`。负 Delta 是扣款、正 Delta 是入账；是否允许负余额由 Store 的领域策略决定，拒绝必须返回 `SettlementRejected` 和稳定 Reason。

保留 CommandID：

```text
0x03000501 CommitSettlement
0x03000502 GetSettlement
0x03000503 GetBalance
0x030005fe ApplyLedgerResult（私有，仅 LedgerRunner）
0x030005ff RecoverSettlement（私有启动/人工查询）
```

Commit/ GetSettlement 的 Call 返回 SettlementResult。Commit 首次接受时返回 pending，并将任务送给 Executor；提交失败（队列满/关闭）返回 rejected。GetBalance 只在 Store/内存投影已知时返回；它不是高吞吐读模型。LedgerRunner 通过私有 ApplyLedgerResult 将 Store 的 committed/rejected/lookup 结果送回 Wallet；Wallet 再向 Request.Source 发送约定的 `ApplySettlementResult` Command。Source 必须是可控业务 Service，不能是 Gateway 连接。

## 状态与生命周期

Wallet Mailbox 是 RequestID 到 pending/terminal result 的唯一 owner。Commit 首次写 pending 后只提交一次任务；重复相同请求不重复 Submit。Runner 调用 Store.Commit 前先 Lookup：已有 terminal record 直接回报，未找到才 Commit。Store.Commit 必须保证成功返回的 record 在重新打开 Store 后 Lookup 可见；它不得返回 committed 后丢失 entries。Wallet 只有收到 committed/rejected ApplyLedgerResult 后才进入终态并通知 Source。

Runner 的未知 I/O 超时不写 rejected：它通过 Lookup 收敛；若 Lookup 仍失败，Wallet 保持 pending，使用 GetSettlement/RecoverSettlement 触发新的 Lookup 任务。重复/迟到 Apply 只在与原 RequestID/Request 匹配时更新，terminal 结果不可被覆盖。MemoryLedgerStore 仅用于测试和示例，进程重启会丢失状态，生产 Config 必须拒绝它。

## 错误与失败语义

- 无效请求、Executor 关闭/队列满、未知 RequestID、request conflict、非授权私有 result source 和 Store 错误有稳定导出错误/Reason。
- Store 显式业务拒绝（例如余额不足）是 terminal rejected；网络错误、超时、进程中断是 unknown/pending，不能冒充拒绝或成功。
- Battle Call 超时后必须 GetSettlement 或等待 `ApplySettlementResult`，不得再次生成新 RequestID 以补偿同一结算。
- Store 对同 RequestID 必须返回字节等价/语义等价的结果；若检测到已存在但请求不同，返回 conflict 且不写账。

## 并发与所有权

WalletService 不创建 goroutine，也不在 Handler 做 Store I/O。LedgerRunner 是唯一允许的 worker pool：组合根拥有固定 Workers、有限 Queue、每任务 Timeout 和 Close；Close 停止接收、取消未开始任务、等待所有已开始 Commit/Lookup 返回，并经 Command 交还能交还的结果。Store 必须自行保证跨 runner/process 的原子 Commit；MemoryLedgerStore 通过锁实现仅进程内一致性。

Balances、Entries、Requests、Results 和 LedgerRecord 均深拷贝。Runner 不保存 ServiceContext，不改 Wallet map，不把 Store error 文本或敏感账目写到日志。

## 可观测性

Wallet GetSettlement/只读 Snapshot 显示 pending/terminal 数、RequestID、Currency 和错误类别；账本审计由 Store 持久化的 LedgerRecord 提供。日志/Record 不输出完整余额或玩家隐私；可按 RequestID 关联。Core Metrics 仍不由 Wallet 直接读取。

## 验收

- 同 RequestID 幂等、冲突请求、显式拒绝、队列满、Runner Close、Store timeout/unknown、迟到重复 result 和 Get/Recover 收敛均有测试。
- Store restart contract 通过一个持久 Store conformance fake 验证；MemoryLedgerStore 仅在 test/example build path 使用。
- Battle 仅收到结果 Command，Wallet Handler 无 I/O/goroutine；race 与 Runner 泄漏测试通过。
