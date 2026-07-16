# RFC-0002：冲突裁决记录

> 状态：草案  
> 范围：设计决策  
> 依据：`docs/learn/go-skynet-chatgpt-20260708-1207.md` 后半段结论

## 目的

原始聊天中有多轮探索和修正。本文记录已经裁决的冲突，避免后续文档和实现反复摇摆。

## 裁决原则

1. 用户最新明确裁决优先；已经明确决定不照搬 Skynet 的部分，以该裁决为准。
2. 对用户和 RFC 尚未说明、或 RFC 互相冲突的问题，先查 Skynet 官方设计文档和 Wiki。
3. Skynet 设计文档没有说明清楚时，以 Skynet 官方源码实现判定其真实约束。
4. 学习源码时提取设计约束，不机械复制 Lua、C 或 PTYPE 的具体机制；最终实现必须适配 Go 的 context、goroutine 和类型系统。
5. Skynet 没有对应概念时，才采用 Go 惯例和一般工程规则。
6. 已写入 RFC 的正式术语优先于未裁决的聊天原文；早期术语只作为背景，不进入代码。
7. 仍无法裁决时标记为开放问题，不写进实现。

## 已裁决冲突

### Actor vs Service

裁决：使用 `Service`。

原因：

- GSR 学习的是 Skynet 的 Service Runtime 思想。
- `Actor` 容易误导成 Actor Framework。
- `Service` 更符合生命周期、地址、状态 owner 的语义。

### ActorRef vs ServiceRef

裁决：使用 `ServiceRef`。

原因：

- `ServiceRef` 是地址，不是对象引用。
- 支持本地和远程统一路由。

### Spawn vs CreateService

裁决：使用 `CreateService`。

原因：

- `Spawn` 容易让人联想到 goroutine。
- `CreateService` 明确表示 Runtime 管生命周期。

### Request/Ask vs Call

裁决：使用 `Call`。

原因：

- `Request` 偏 HTTP/RPC。
- `Call` 更接近 Skynet `call` 的 Session 语义。

### Tell vs Send

裁决：使用 `Send`。

原因：

- 贴近 Skynet `send`。
- 与 `Call` 形成清晰投递语义。

### Command 是否等于 RPC

裁决：不等于。

Command 是 Service 能力入口。Send 和 Call 是投递方式。

### Cluster 是否是 RPC

裁决：不是。

Cluster 是远程 Service Message Dispatch。

### Cluster 是否只包含 Transport

裁决：不是。

Cluster 至少分为：

```text
Data Plane: 业务 Envelope 跨节点投递
Control Plane: 节点管理、健康检查、观测查询、受控运维命令
```

`ClusterTransport` 只属于数据面。管理面能力必须放在 `ClusterControlService`、`DiscoveryService`、`MonitorService`、`NodeAgentService` 等系统 Service 中。

### Skynet PTYPE 是否进入 GSR

裁决：不进入。

Skynet 的 `PTYPE_LUA`、`PTYPE_RESPONSE`、`PTYPE_SOCKET`、`PTYPE_HARBOR` 等属于 Skynet Runtime 的协议入口。GSR 不复刻这层模型。

GSR 的最终口径是：

```text
Envelope
  ↓
Command Dispatcher
  ↓
Command Handler
  ↓
Service
```

原因：

- Go 不需要通过 `PTYPE_*` 才能完成业务分发。
- `Protocol` 属于 Transport 编码和链路细节。
- `Command` 才是 Service 能力入口。
- `PTYPE_RESPONSE` 对应的能力由 `Reply` 和 `PendingCall` 自动完成。

### Battle 是否进入 Core Runtime

裁决：不进入。

Battle 属于 Game Layer。Core Runtime 不知道 Battle。

### BattleService vs TableService

裁决：第一版默认使用 `BattleService`，不新增默认 `TableService`。

原因：

- “桌子”是棋牌游戏表现形式，不是通用业务模型。
- 原项目中的 `roomserver.tablelist` 和 `attackd` 本质上共同描述同一组玩家的游戏活动。
- 如果把座位、准备、重连、当前局、动作和计时器拆到两个 Service，容易重新制造跨 owner 协调问题。

因此，棋牌游戏模板中：

```text
BattleService owns participants / seats / ready / reconnect / round / actions / timers
RoomService owns room entry / battle index / allocation policy
```

只有当长期场馆、桌子索引、跨局容器或分布式迁移成为明确需求时，才考虑拆出单独的 `TableService` 或 `RoomTableIndex`。

### 跨 Service 状态是否使用锁协调

裁决：不使用“用户锁 + 桌子锁”或其他嵌套锁，来维持跨 Service 的一致性。

原因：

- Mailbox 已经保证同一个 Service 内的 Command 串行执行。
- 两把锁不能定义跨 Service 的状态 owner，只会引入锁顺序、重入和超时后的补偿问题。
- 原棋牌游戏框架中桌子状态和对局状态分散，才产生了 table/user lock 的协调压力；第一版将这些强一致状态收回 `BattleService`。

GSR 的规则是：

```text
一份可变权威状态 -> 一个 Service owner -> 一个 Mailbox 串行写入
跨 Service 协作 -> Command + 明确结果 + RequestID 幂等
```

一个 handler 不得在修改本地状态后，同步等待另一个 Service，再继续依赖旧前提修改本地状态。需要外部结果时，应拆为后续 Command，或以业务补偿流程收敛。

### Discovery 是否使用传统注册中心

裁决：第一版不使用 Consul/Nacos。

采用简单 `DiscoveryService`，解决 Node Discovery 和长期 ServiceName 解析。

### ServiceGroup 是否进入 Core Runtime

裁决：不进入。

ServiceGroup 是工程化服务治理能力。它依赖 `ServiceRef`，但不是 Core Runtime 实体。

Core Runtime 不知道 `Hash`、`RoundRobin`、`Broadcast` 等策略。路由策略属于 Runtime Tooling。

### 是否做 Lua 风格热修复

裁决：不做。

可以学习 `skynet_fly` 的平滑切换、访问者追踪和旧服务 Drain，但不照搬 Lua 热修复。

Go 版本优先采用：

```text
启动新实例
  ↓
切换 ServiceGroup 版本
  ↓
Drain 旧实例
  ↓
关闭旧实例
```

### Record/Replay 是否进入 Core Runtime

裁决：不进入 Core Runtime 最小接口。

Record/Replay 是 Debug、测试和 Battle 问题复现能力。它通过观察 `Envelope` 和 `Command` 实现，不改变 Service 的基本模型。

### 登录握手放在哪里

裁决：放在 `LoginService`。

Skynet `examples/login` 的标准分工是：登录服务完成 challenge、密钥协商、HMAC 校验和 token 认证；游戏网关只验证客户端持有同一个 `secret` 并绑定连接；已认证请求再交给 agent 或业务服务。

GSR 采用这个分工：

```text
LoginService -> Gateway Adapter -> ProtocolMapper -> Command -> Service
```

因此：

- `LoginService` 属于 Runtime Tooling。
- `Gateway Adapter` 不重新交换 `secret`。
- `ProtocolMapper` 不做登录握手。
- Core Runtime 不知道 token、fd、subid、secret。
- 明文 `secret` 不进入普通业务 `Command`、日志、Snapshot 或 Record。

### 是否支持远程任意代码注入

裁决：生产 Runtime 不支持。

可以学习 `skynet-admin` 的远程管理入口，但不能照搬任意代码注入。GSR 的远程运维只能通过白名单 Command，并且必须支持权限控制和审计。

### 一个 Service 一个 goroutine

裁决：不是默认模型。

默认模型是：

```text
Service = State + Mailbox + Handler
Scheduler + 固定执行许可池负责执行
```

### Service 是否可以直接创建 goroutine

裁决：不可以。

Service 业务代码不得用 `go func` 创建脱离 Runtime 管理的异步任务。异步行为优先表达为 Command、Timer 或独立 Service。Runtime 发起的 Init、dispatch、Stop 和 Close 执行任务必须登记 owner、任务类型、取消函数和完成句柄；任务超时后即使无法强制终止，也不能从 Runtime 的追踪表中消失。

第一版不提供公开 `Fork` 或 `Go` API。只有出现无法用 Command、Timer 或 Service 表达的明确需求时，才讨论受管 Task API。

### Service handler 内是否允许同步 Call

裁决：允许，但等待 Reply 时必须让出 Scheduler 执行许可；返回前重新获取许可。

Service 在挂起期间仍保持 busy，不消费自己的后续 Command。Runtime 使用 `Envelope.CallPath` 拒绝同步 self-call 和已检测到的调用环。普通阻塞 IO 不享受该机制，仍应拆到专用 Service 或异步边界。

## 仍需后续细化的问题

1. Command 泛型 API 的最终形态。
2. Timer 第一版是否直接上 Timer Wheel。
3. DiscoveryService 是否单点还是 Gossip。
4. Wallet 持久化接口是否进入 GSR 项目范围。
5. Transport 是否需要 Codec Registry；即使需要，也不暴露成业务可见的 `ProtocolID`。
6. 管理面认证授权的第一版实现方式。
7. ServiceGroup 路由策略的默认组合。
8. Record 文件格式和脱敏策略。
9. LoginService 第一版使用 TCP 还是 WebSocket 入口。
