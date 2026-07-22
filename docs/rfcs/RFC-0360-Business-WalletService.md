# RFC-0360：WalletService 设计

> 状态：草案
> 目标阶段：Phase 12
> 范围：Business Layer、Persistence
> 依赖：[RFC-0300](RFC-0300-Business-Layering.md)、[RFC-0210](RFC-0210-Tooling-Snapshot.md)
> 依据：`docs/learn/007-Game-Service-Runtime详细设计与实现.md`

## 目的

本文定义 WalletService 的一致性边界。

## 核心原则

Wallet 是强一致服务。

Battle 不得直接修改钱包余额。

## Command

建议：

```text
CmdFreezeAmount
CmdCommitSettlement
CmdRollbackSettlement
CmdGetBalance
```

## 幂等

所有结算命令必须带幂等键。

```go
type SettlementRequest struct {
    SettlementID string
    PlayerID     PlayerID
    Amount       int64
}
```

重复提交同一 `SettlementID` 必须返回相同结果。

## 持久化

Wallet 不应只依赖内存 Snapshot。

必须有：

- 持久化记录。
- 审计日志。
- 幂等表。
- 失败恢复策略。

## 规则

1. Battle 只计算结果。
2. Wallet 执行资金变更。
3. PlayerService 接收最终可见结果。
4. Wallet 崩溃时优先保护一致性。
5. Wallet 错误必须明确返回给结算流程。
6. Snapshot 不是余额、流水或幂等结果的权威来源。
7. 持久化成功与业务 Reply 的先后顺序、超时后的查询接口和重复请求结果必须在进入“待实现”前写成稳定契约。
