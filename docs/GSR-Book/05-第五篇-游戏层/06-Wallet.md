# Wallet 与 LedgerRunner：资金流程不能相信超时

“Battle Call Wallet，成功就加金币，超时就重试。”

老周问：“第一次其实已经写入数据库，第二次怎么办？”

这就是 Wallet 必须围绕 RequestID 设计的原因。

## 三个 owner

```text
WalletService
  -> 拥有请求阶段和余额投影
LedgerRunner
  -> 拥有有界 worker 与 I/O 生命周期
LedgerStore
  -> 拥有持久、幂等资金事实
```

任何一个角色都不能吞并另外两个。

## 创建 Runner

```go
store := game.NewMemoryLedgerStore()

runner, err := game.NewLedgerRunner(runtime, game.LedgerRunnerConfig{
    Store:     store,
    Workers:   2,
    QueueSize: 128,
    Timeout:   time.Second,
})
defer runner.Close(context.Background())
```

Runner 属于组合根，是允许使用固定 goroutine 的外部 worker pool。它有有界队列、取消和 Close 等待。

## 创建 Wallet

```go
wallet, err := game.NewWalletService(game.WalletConfig{
    Executor:   runner,
    MaxPending: 1024,
    RunnerNode: "local",
})
```

Wallet Handler 不直接调用 LedgerStore。

## 提交结算

Battle Send：

```go
game.SettlementRequest{
    RequestID: "settle-42",
    Source:    battleRef,
    Currency:  "coin",
    Entries: []game.SettlementEntry{{
        Player: "alice",
        Delta:  10,
    }},
}
```

Wallet 验证 `request.Source == commandContext.Source()`，防止调用者代替其他 Battle 声明结果接收方。

第一次请求：

```text
保存 request
保存 pending result
Submit LedgerTask
Reply pending
```

同 RequestID 同内容返回已有结果，不重复提交；不同内容返回 `ErrRequestConflict`。

## Runner 流程

```text
LedgerTask
  -> Store.Lookup(RequestID)
  -> 已存在：返回原结果
  -> 不存在：Store.Commit
  -> Runtime.Send(private ApplyLedgerResult)
```

Store 超时或未知时，Runner 返回非 terminal result；Wallet 保持 pending，后续可以 Recover/Query，不能擅自标记 rejected。

## 来源校验

私有 result Command 只接受：

```go
gsr.ServiceRef{Node: RunnerNode, ID: 0}
```

这表示组合根 Runner 通过 Runtime 节点来源回投。普通 Service 不能伪造 committed。

## Terminal result

Committed：

- Wallet 更新余额投影；
- Send `ApplySettlementResultCommand` 给原 Battle；
- Battle 等所有 settlement committed 后 Finished。

Rejected：

- Wallet 保存稳定原因；
- 通知 Battle；
- Battle 进入 Failed。

通知 Send 失败会记录指标。Wallet 的事实不会因此回滚；上层通过 RequestID 查询和补发。

## 为什么 Snapshot 不是账本

Snapshot 可能晚、可能丢，不能证明一笔资金是否提交。资金真相来自 LedgerStore 的幂等记录和审计。

MemoryLedgerStore 只用于测试与示例。生产 adapter 需要明确：

- 原子 RequestID 唯一约束；
- 同请求同结果；
- 冲突检测；
- 超时后可 Lookup；
- 审计和备份。

## Close

关闭顺序通常是：

```text
停止入口
  -> 等业务收敛
  -> Stop Wallet
  -> Close LedgerRunner
  -> 等 worker 真实返回
  -> Close Runtime
```

实际组合根可按依赖倒序调整，但不能让 Runner 在 Wallet 仍接收任务时先消失。

## 对照源码

- `game/wallet.go`
- `game/ledger_runner.go`
- `game/memory_ledger.go`
- `game/wallet_test.go`
- `docs/rfcs/RFC-0360-Business-WalletService.md`

## 本章小结

Wallet 的核心不是余额 map，而是对未知结果保持正确：pending 就是 pending，超时不能被翻译成失败，重试必须回到同一个 RequestID。
