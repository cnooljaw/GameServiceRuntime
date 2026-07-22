# RFC-0280：Command Record 与 Replay

> 状态：草案
> 目标阶段：Phase 11
> 范围：Runtime Tooling、Debug、Business Layer
> 依赖：[RFC-0100](RFC-0100-Core-Service.md)、[RFC-0210](RFC-0210-Tooling-Snapshot.md)
> 依据：`skynet_fly` 的服务录像与重放能力

## 目的

本文定义 GSR 的 Command 录制和重放。

Record/Replay 用于复现问题，不替代 Snapshot、数据库持久化或业务日志。

## Core Runtime 边界

Core Runtime 不直接依赖录制系统，也不增加返回可变 Envelope 的事件钩子。

第一版使用 Service decorator 包装目标 Service，在同一个 `Handle` 边界编码并记录进入该 Service 的 Command：

```text
Mailbox
  ↓
Recording decorator Handle
  ├── encode immutable record
  ├── Send record to RecorderService
  └── delegate to business Handle
```

Decorator 是可插拔组合对象，不改变 `Service` 接口，也不暴露被包装 Service。Timer、Service 间 Send/Call 和外部入口最终都进入同一 Handle，因此可以在这一边界得到每个目标 Service 的串行输入顺序。

## Record 内容

一条记录至少包含：

```text
RecordVersion
Source ServiceRef
Target stable business key and current ServiceRef
CommandID
Payload
Per-target Sequence
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
7. 第一版不承诺记录 Call/Send 的 Session 模式；Core `CommandContext` 不公开 Session。需要验证 Reply 时，应记录业务输出或最终状态，而不是推断内部 PendingCall。
8. RecorderService 的存储状态只能通过 Command 修改；持久化 IO 的 owner 和背压策略必须在进入“待实现”前明确。
