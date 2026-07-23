# RFC-0500：开发路线图

> 状态：已接受
> 范围：Roadmap
> 依赖：[RFC-0001](RFC-0001-Foundation-Design-Principles.md)、[RFC-0003](RFC-0003-Foundation-RFC-Lifecycle.md)
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

截至 2026-07-23，**Core Runtime** 首版已经完成。它覆盖 `RFC-0100` 至 `RFC-0192`、本路线图的 Phase 1 至 Phase 7A，包括错误模型、性能基线、生命周期与任务观测、本地和双节点端到端示例。

历史讨论中的“第一阶段”默认指 Cluster 之前的 Core Foundation，不等同于本路线图狭义的 Phase 1。Phase 5 Cluster Data Plane 后续独立实施，并已完成。

已实现里程碑的工程收口项统一记录在 [`docs/TODO.md`](../TODO.md)，后续新能力仍按本文顺序实施。

首份性能结果见 [`2026-07-17 Core Runtime 性能基线`](../benchmarks/2026-07-17-core-runtime.md)。Phase 7A、最小 Discovery、本地 Monitor、Snapshot、Supervisor 和客户端入口已完成。当前进入 Phase 8：NodeAgent、自动租约 Heartbeat 与节点观测切片。

## 后续 RFC 审核结果

2026-07-22 根据已接受 Core RFC、`v0.3.0` 代码和测试完成首轮审核。结果如下：

| RFC | 阶段 | 状态 | 审核结论 |
|---|---|---|---|
| RFC-0210 Snapshot | 7D | 已接受 | 已实现 Capture Command、外部 Store、Revision 冲突保护、Cluster Codec 和组合根恢复。 |
| RFC-0220 Supervisor | 7E | 已接受 | 已实现 panic Decorator、Source/Generation fencing、有界 Runner、恢复预算、两阶段发布和 Snapshot 纵向切片。 |
| RFC-0290 客户端入口 | 7F | 已接受 | 已实现内存 SessionRegistry、SingleSession LoginService、固定 proof 线格式、TCP Login/Gateway Adapter、ProtocolMapper seam 与端到端验收。 |
| RFC-0250 Control Plane | 8 | 待实现 | 已冻结可信集群内 NodeAgent、自动 Heartbeat、节点配置与 Observed State；Service Desired State、Reconcile 和修改型运维明确后置。 |
| RFC-0260 ServiceGroup | 9 | 草案 | ServiceGroup 不进入现有 Discovery；需要独立 DirectoryService，Watch 使用 ServiceRef + Command。 |
| RFC-0270 Drain | 10 | 草案 | Visitor 状态改由 Service + Command 持有；需要租约、代际和失败回滚契约。 |
| RFC-0280 Record/Replay | 11 | 草案 | 第一版采用 Service decorator，不增加 Core Envelope 旁路；持久化背压仍需裁决。 |
| RFC-0300 至 RFC-0370 | 12 | 草案 | 已修正 Service 创建、Timeline 取消、Gateway 和跨 Service 状态推进边界；逐个模板仍需冻结公开 API。 |
| RFC-0400 示例 | 13 | 草案 | 结算改为 RequestID + 结果 Command，停止由外层生命周期 owner 发起。 |

审核只把没有开放接口问题的 RFC 提升为“待实现”。其它 RFC 保持“草案”，不能直接进入代码。

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

Phase 7 拆成六个可独立验收的子阶段，避免一次把所有外层能力压入 Core。

### Phase 7A：Runtime Inspection 与 Core 首版

状态：已完成（2026-07-17）。

实现：

- `Runtime.Inspect` 只读观测边界。
- Service、Mailbox、PendingCall、Timer 和 Runtime Task 视图。
- Core 与 Cluster 文档同步。
- API 冻结 Review。
- `v0.1.0` 标签。

### Phase 7B：最小 Discovery

状态：已完成并通过审查修正（2026-07-22）。

实现 Node Discovery、长期 ServiceName Discovery、AuthorityEpoch 与租约 owner 约束，并通过 Core 节点级名字查询完成稳定 bootstrap。ServiceGroup、Gossip、负载均衡和管理命令不进入本阶段。

### Phase 7C：本地 Monitor

状态：已完成（2026-07-22）。

实现本地 Monitor Report、JSON 输出、Metrics 枚举副本和远程 Call 结果指标。远程 NodeAgent、HTTP/CLI、Prometheus exporter 与管理面查询留到 Phase 8 或独立 adapter。

### Phase 7D：Snapshot

状态：已完成（2026-07-22）。

实现 Capture Command、版本化 State、稳定业务 Key、存储适配器、修订号冲突检查、可组合 Codec 和组合根受限恢复。业务持久化不下沉到 Core，不修改运行中实例。

### Phase 7E：Supervisor

已完成不可变失败通知、Source/Generation fencing、三种策略、尝试与窗口预算、有界 Runner、两阶段 Launcher 和 Snapshot 恢复纵向切片。Supervisor 只创建新实例，不复活旧 `ServiceRef`，不在 panic 后临时抓取状态；Publish 与 committed 之间再次失败也会消耗并隔离已经对外运行的 Generation。

### Phase 7F：客户端入口

状态：已完成（2026-07-22）。

已实现内存 `SessionRegistry`、`Login Adapter`、`LoginService`、最小 TCP `Gateway Adapter` 和 `ProtocolMapper` seam。会话 Generation、proof 线格式、原子绑定、SingleSession 的旧连接关闭，以及 Adapter 失败交接由 RFC-0290 冻结。生产 Handshake、TLS、跨节点或持久化会话不在本阶段。

## Phase 8：节点观测与 Heartbeat

实现：

- `ClusterObserverService`。
- `NodeAgentService`。
- 自动 Discovery Heartbeat、节点配置 / 缓存 Observed State。
- 节点状态列表、节点详情与单节点刷新。
- 只读 Agent 的 Cluster Codec 与双节点示例。

不实现：

- 远程任意代码执行。
- 外部 Admin API、认证、授权、审计和生产环境危险运维命令。
- Service Desired State、Reconcile、扩缩容、故障迁移、reload、Drain、ServiceGroup 切换和动态 peer 更新。

Phase 10 才能在独立 RFC 中冻结 Service Desired State、Controller、Reconcile 与修改型执行命令；不得把 NodeID 来源检查当作用户身份认证。

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

- Tooling 能力不得下沉为 Core 领域概念；只有多个上层共同需要的通用 Runtime 能力，才可先修改 RFC 后进入 Core。
- 不让 Discovery 决定路由策略。

## Phase 10：Drain 与热更新切换

实现：

- `DrainService`
- `Visitor Tracking`
- `Weak Visitor`
- ServiceGroup 版本切换。
- 切换失败回滚。
- Service Desired State、Controller、Reconcile 与 NodeAgent 执行动作。

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
