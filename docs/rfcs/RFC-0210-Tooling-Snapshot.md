# RFC-0210：Snapshot 与恢复

> 状态：草案  
> 范围：Runtime Tooling、Business Layer  
> 依据：`docs/learn/007-Game-Service-Runtime详细设计与实现.md`

## 目的

本文定义 Service 状态快照的用途和边界。

## 用途

Snapshot 用于：

- Player 重启恢复。
- Battle 重连视图。
- Supervisor 恢复。
- Debug dump。
- 后续迁移。

Record/Replay 用于：

- 复现 Battle 复杂 Bug。
- 回放 Command 执行顺序。
- 验证热更新前后行为差异。
- 定位非确定性输入。

Snapshot 是状态，Record 是输入序列。两者不能互相替代。

## 接口草案

```go
type Stateful interface {
    Snapshot() ([]byte, error)
    Restore([]byte) error
}
```

Record 接口草案：

```go
type CommandRecorder interface {
    Record(envelope Envelope, at time.Time) error
}

type CommandReplayer interface {
    Replay(ctx context.Context, record CommandRecord) error
}
```

`CommandRecord` 至少包含：

```text
ServiceRef
CommandID
Payload
Session mode
Timestamp or logical tick
Record schema version
```

## 不同服务策略

### BattleService

Battle 通常是临时实例。

Snapshot 主要用于：

- 玩家重连。
- 观战。
- Debug。
- Record/Replay。

崩溃后可以 Destroy，不一定恢复。

Battle 的 Replay 必须控制随机数、时间和外部输入，否则只能作为近似排查工具。

### PlayerService

Player 是长期状态服务。

Snapshot 建议必做。

### WalletService

Wallet 是强一致边界。

不能只依赖内存 Snapshot，必须结合持久化和幂等结算。

## 规则

1. Snapshot 不应阻塞 Worker 太久。
2. Snapshot 内容必须有版本。
3. Restore 必须校验版本。
4. Snapshot 不替代数据库持久化。
5. Battle 重连快照必须包含 `BattleEpoch` 和 `TimelineRev`。
6. Record 默认只记录进入 Service 的 Command，不记录内部私有函数调用。
7. Replay 必须声明时间、随机数、外部 IO 的处理策略。
8. Record/Replay 属于 Debug 和测试能力，不进入 Core Runtime 最小接口。
