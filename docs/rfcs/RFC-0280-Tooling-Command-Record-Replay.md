# RFC-0280：Command Record 与 Replay

> 状态：草案  
> 范围：Runtime Tooling、Debug、Business Layer  
> 依据：`skynet_fly` 的服务录像与重放能力

## 目的

本文定义 GSR 的 Command 录制和重放。

Record/Replay 用于复现问题，不替代 Snapshot、数据库持久化或业务日志。

## Core Runtime 边界

Core Runtime 不直接依赖录制系统。

Record/Replay 通过观察 `Envelope` 和 `Command` 实现：

```text
Envelope
  ↓
Recorder
  ↓
Mailbox
  ↓
Handler
```

Recorder 是可插拔扩展，不改变 `Service` 接口。

## Record 内容

一条记录至少包含：

```text
RecordVersion
ServiceRef
CommandID
Payload
SessionMode
LogicalTime or Timestamp
RandomSeed if any
TraceID if any
```

不要记录：

- Service 指针。
- 私有函数调用。
- 未脱敏的敏感数据。
- 不可复现的外部 IO 原始句柄。

## Replay 模型

Replay 应创建隔离环境：

```text
Create replay Runtime
  ↓
Restore initial Snapshot if exists
  ↓
Feed recorded Command
  ↓
Compare output / state / metrics
```

Replay 可以有两种模式：

```text
BestEffort:
  尽量复现，用于排查。

Deterministic:
  控制时间、随机数和外部输入，用于测试。
```

## Battle 场景

BattleService 是第一优先级。

原因：

- Battle 输入天然是 Command。
- Battle 状态短生命周期。
- Bug 常依赖时间、顺序和随机数。

Battle Replay 需要记录：

- `BattleEpoch`
- `TimelineRev`
- 玩家输入 Command。
- Timer 触发 Command。
- 随机种子。
- 结算 Command。

## 与 Snapshot 的关系

Snapshot 保存某一刻状态。

Record 保存一段输入。

推荐组合：

```text
Initial Snapshot + Command Record -> Replay
```

没有 Snapshot 时，可以从 Service 初始状态开始重放，但成本更高。

## 存储和脱敏

Record 文件必须有版本。

Payload 可能包含用户数据，必须支持：

- 字段脱敏。
- 采样。
- 大小限制。
- 保留时间。
- 按 Battle、Player、TraceID 查询。

## 验收

必须能验证：

- 记录进入 Service 的 Command。
- 重放时 Command 顺序一致。
- Timer Command 可重放。
- 随机数可固定。
- Payload 版本不兼容时给出明确错误。
- Record 不影响正常 Runtime 性能。

## 规则

1. Record/Replay 不进入 Core Runtime 最小接口。
2. Record 默认记录 Command，不记录私有函数调用。
3. Replay 必须在隔离 Runtime 中执行。
4. Battle Replay 必须控制时间和随机数。
5. Record 文件必须有版本和脱敏策略。
6. 录制失败不能影响业务 Command 执行，除非测试模式显式要求 fail fast。
