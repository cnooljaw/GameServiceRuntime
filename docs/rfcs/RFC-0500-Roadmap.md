# RFC-0500：开发路线图

> 状态：草案  
> 范围：Implementation Plan  
> 依据：`docs/learn/007-Game-Service-Runtime详细设计与实现.md`

## 目的

本文定义 GSR 从零实现的顺序。

## 总原则

先证明单节点 Runtime 模型正确，再做 Cluster。

实现顺序必须符合三层结构：

```text
Layer 1: Core Runtime
Layer 2: Runtime Tooling
Layer 3: Business Layer
```

内层先稳定，外层再加能力。外层不得反向污染内层。

不要一开始实现：

- 完整 Cluster。
- Discovery 高可用。
- Snapshot 迁移。
- Monitor Console。
- Runtime Tooling 全量能力。
- 业务层完整框架。

## 当前执行状态

截至 2026-07-17，**Core Runtime** 里程碑及 Cluster 前工程收口已经完成。它覆盖 `RFC-0100` 至 `RFC-0191`、本路线图的 Phase 1 至 Phase 6，包括错误模型、性能基线、生命周期可观测、本地和双节点端到端示例。

历史讨论中的“第一阶段”默认指 Cluster 之前的 Core Foundation，不等同于本路线图狭义的 Phase 1。Phase 5 Cluster Data Plane 后续独立实施，并已完成。

已实现里程碑的工程收口项统一记录在 [`docs/TODO.md`](../TODO.md)，后续新能力仍按本文顺序实施。

首份性能结果见 [`2026-07-17 Core Runtime 性能基线`](../benchmarks/2026-07-17-core-runtime.md)。Phase 5 Cluster Data Plane 和 Phase 6 Core Runtime 验证已于 2026-07-17 完成，下一实施阶段是 Phase 7 Runtime Tooling 基础。

## Phase 0：文档和术语冻结

输出：

- RFC-0000 术语表。
- RFC-0001 设计原则。
- API 规则。
- 命名规则。
- 冲突裁决记录。

## Phase 1：单节点 Service Runtime

实现：

- `ServiceRef`
- `Service`
- `ServiceSpec`
- `CreateService`
- `LocalRegistry`
- `Mailbox`
- `Send`

验收：

```text
Service A Send Command to Service B
Service B Handle Command
```

## Phase 2：Command 与 Call/Reply

实现：

- `CommandID`
- `CommandDeclarer` 与 Runtime 私有只读命令集
- `SessionID`
- `PendingCall`
- `Call`
- `CommandContext.Reply`

验收：

- Call 返回结果。
- Call 超时。
- Reply 两次失败。

## Phase 3：Scheduler

实现：

- ReadyQueue。
- 固定执行许可池。
- Batch。
- 慢 Command 指标。

## Phase 4：Timer

实现：

- `After`
- `Cancel`
- Timer 生成 Command。

## Phase 5：Cluster Data Plane

实现：

- `NodeID`
- `ClusterTransport`
- Handshake。
- WireEnvelope。
- Remote Send。
- Remote Call/Reply。

验收：

```text
Local Send/Call 与 Remote Send/Call 行为一致
```

## Phase 6：Core Runtime 验证

实现：

- 核心错误模型。
- 核心性能基准。
- 生命周期可观测。
- 最小端到端例子。

不实现具体游戏业务。

## Phase 7：Runtime Tooling 基础

实现：

- `DiscoveryService`
- `LoginService`
- `SessionRegistry`
- 最小 `Gateway Adapter`
- Snapshot。
- Supervisor。
- Monitor。
- Metrics。
- Benchmark。

## Phase 8：Cluster Control Plane

实现：

- `ClusterControlService`
- `NodeAgentService`
- 节点 Desired State / Observed State。
- 节点状态列表。
- 节点详情查询。
- 管理命令审计。

不实现：

- 远程任意代码执行。
- 生产环境默认开启危险运维命令。

## Phase 9：Runtime Tooling 扩展

实现：

- `ServiceGroup`
- `ServiceSetVersion`
- `WatchServiceGroup`
- `RoutingPolicy`
- `Hash`
- `RoundRobin`
- `Broadcast`

约束：

- 不修改 Core Runtime。
- 不让 Discovery 决定路由策略。

## Phase 10：Drain 与热更新切换

实现：

- `DrainService`
- `Visitor Tracking`
- `Weak Visitor`
- ServiceGroup 版本切换。
- 切换失败回滚。

不实现：

- 任意代码热替换。
- Go 进程内危险热补丁。

## Phase 11：Command Record 与 Replay

实现：

- Command Record。
- Battle Replay。
- Record 文件版本。
- 时间和随机数控制策略。

约束：

- Record/Replay 只作为 Debug 和测试能力。
- 不替代 Snapshot 和持久化。

## Phase 12：Business Layer 最小封装

实现：

- `game.CreateBattle`
- `BattleContext`
- `Timeline`
- `Broadcast`
- `PlayerService`
- `PlayerModule`

## Phase 13：业务示例

实现端到端：

```text
Room -> Battle -> Timeline -> Kick -> Settlement -> Stop
```

可增加玩家类示例：

```text
LoginService -> Gateway Adapter -> ProtocolMapper -> PlayerService -> PlayerModule -> Repository
```

约束：

- 具体业务只放在 Business Layer。
- 不让 Core Runtime 引用 Player/Battle/Room/Wallet。

## Commit 建议

提交信息尽量中文：

```text
feat(runtime): 创建 Service 生命周期
feat(runtime): 实现 Mailbox 和 Send
feat(runtime): 增加 Call Reply Session
feat(runtime): 增加 Scheduler
feat(runtime): 增加 Timer Command 管道
feat(cluster): 增加远程 Service Transport
feat(cluster): 增加集群控制面服务
feat(cluster): 增加 ServiceGroup 路由策略
feat(runtime): 增加 Drain 访问者追踪
feat(debug): 增加 Command 录制回放
feat(game): 增加 PlayerModule 业务组合
feat(game): 实现打地鼠 Battle 示例
```
