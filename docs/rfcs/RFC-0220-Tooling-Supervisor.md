# RFC-0220：Supervisor 与故障隔离

> 状态：草案  
> 范围：Runtime Tooling  
> 依据：`docs/learn/007-Game-Service-Runtime详细设计与实现.md`

## 目的

本文定义 Service 失败后的处理策略。

## 核心原则

Service 是故障隔离边界。

Service panic 不等于进程崩溃。

## 策略

```go
type RestartStrategy int

const (
    RestartNever RestartStrategy = iota
    RestartOnFailure
    RestartAlways
    DestroyOnFailure
)
```

## Panic 流程

```text
recover panic
  ↓
Mark ServiceFailed
  ↓
Report Supervisor
  ↓
Apply policy
```

## 服务建议

| 服务 | 策略 |
|-|-|
| BattleService | DestroyOnFailure |
| PlayerService | RestartOnFailure + Snapshot |
| WalletService | Protect state + fail fast |
| ConfigService | RestartOnFailure |
| DiscoveryService | RestartAlways 或进程保护 |

## 重启流程

```text
Freeze Service
  ↓
Reject new messages
  ↓
Snapshot if possible
  ↓
Stop old instance
  ↓
Create new instance
  ↓
Restore
  ↓
Resume
```

## 规则

1. Supervisor 不能吞掉错误不记录。
2. 必须限制重启频率。
3. Pending Call 必须得到明确错误。
4. Battle 崩溃默认不要自动重启。
5. Wallet 崩溃必须优先保护一致性。
