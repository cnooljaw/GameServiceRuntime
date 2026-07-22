# RFC-0001：设计原则

> 状态：已接受
> 范围：GSR 总体架构
> 依据：`docs/learn/005-Skynet设计思想与优雅实现.md`、`hanxi/skynet-demo`

## 目的

本文定义 GSR 的架构原则。所有后续 RFC 必须服从本文。

## 定位

GSR 不是：

- Actor Framework。
- RPC Framework。
- gRPC Framework。
- DDD/CQRS Framework。
- 微服务注册中心。
- Web 后端框架。
- Skynet PTYPE 复刻。

GSR 是：

```text
基于 Skynet 思想，用 Go 实现的游戏服务器 Service Runtime。
```

## 总体分层

```text
Layer 3: Business Layer
  ProtocolMapper / Battle / Room / Player / Wallet
  Timeline / Broadcast / Game Examples
        ↓
Layer 2: Runtime Tooling
  Discovery / Snapshot / Supervisor / Monitor / Control Plane
  LoginService / Gateway Adapter / ServiceGroup / RoutingPolicy / Drain / Record / Replay
        ↓
Layer 1: Core Runtime
  Service / ServiceRef / Command / Envelope / Mailbox / Scheduler
  Timer / Lifecycle / Send / Call / Cluster Data Plane
```

依赖方向只能从外向内。内层不能引用外层概念。

Core Runtime 保持通用。Runtime Tooling 解决工程化和运维问题。Business Layer 承载具体游戏业务。

### Layer 1：Core Runtime

Core Runtime 只提供最小运行模型：

```text
Service owns state
Command enters Service
Envelope carries Command
Mailbox queues Envelope
Scheduler executes handlers
Timer emits Command
Send / Call route Envelope
Cluster Data Plane extends routing to remote nodes
```

Core Runtime 不知道：

- Battle。
- Room。
- Player。
- Wallet。
- LoginService。
- Gateway Adapter。
- ProtocolMapper。
- ServiceGroup。
- Drain。
- Record/Replay。
- Admin API。
- ORM、Redis、MySQL。

### Layer 2：Runtime Tooling

Runtime Tooling 是领域无关的工具层。

它可以提供：

- Discovery。
- Snapshot。
- Supervisor。
- Monitor。
- Cluster Control Plane。
- LoginService。
- Gateway Adapter。
- ServiceGroup 与 RoutingPolicy。
- Drain 与访问者追踪。
- Command Record/Replay。

这些能力可以复用 Core Runtime，但不能改变 Core Runtime 的最小接口。

### Layer 3：Business Layer

Business Layer 是最外层。

它可以提供：

- ProtocolMapper。
- Battle。
- Timeline。
- Room。
- PlayerService。
- PlayerModule。
- WalletService。
- 打地鼠示例。
- 其他具体游戏业务。

业务层可以使用 Runtime Tooling 和 Core Runtime，但不能反向要求内层理解业务词汇。

## 核心原则

### Service 拥有状态

每份可变业务状态必须有唯一 owner。

例如：

```text
BattleService owns BattleState
PlayerService owns PlayerState
WalletService owns WalletState
```

禁止多个 Service 共享同一个可变对象。

### Command 是唯一入口

任何状态修改都必须通过 Command 进入 Service。

禁止：

```go
battle.Kick()
player.AddCoin()
```

使用：

```go
runtime.Call(ctx, battleRef, CmdKickShrew, req)
runtime.Call(ctx, walletRef, CmdCommitSettlement, req)
```

### 不暴露 PTYPE

Skynet 的 `PTYPE_LUA`、`PTYPE_RESPONSE`、`PTYPE_SOCKET` 等属于 Skynet Runtime 的协议入口。

GSR 不照搬这层概念。Go 版 Runtime 内部统一使用：

```text
Envelope
  ↓
Command Dispatcher
  ↓
Command Handler
  ↓
Service
```

业务只关心 `Command`。Transport 才关心 protobuf、TCP、WebSocket、QUIC 等编码和链路细节。

### Send 和 Call 共用消息管道

`Send` 和 `Call` 都投递 Command。

区别只在 Session：

```text
Send: session = 0
Call: session > 0
```

### Cluster Data Plane 是 Transport 的远程延伸

Cluster Data Plane 通过 `ClusterTransport` 把同一条消息管道延伸到远程节点。Cluster 整体还包含位于 Runtime Tooling 的 Control Plane，因此 Cluster 整体不等于 Transport。

业务代码不应该区分本地和远程。

### Timer 生成 Command

Timer 到期后只能向目标 Service 投递 Command。

禁止 Timer callback 直接修改状态。

### Runtime 管生命周期

Service 的创建、停止、重启、关闭由 Runtime 管理。

业务只拿 `ServiceRef`，不能拿 Service 指针。

### 客户端入口不进 Core Runtime

WebSocket、HTTP、TCP、自定义二进制协议都属于外层入口适配。

入口层采用类似 Skynet `examples/login` 的职责切分，并适配 GSR 的 Service goroutine 约束：Login Adapter 负责登录连接、握手和 `secret` 交换，LoginService 负责编排认证状态和票据，Gateway Adapter 负责游戏连接验证和绑定，ProtocolMapper 负责业务协议到 `Command` 的映射。

LoginService 和 Gateway Adapter 属于 Runtime Tooling 或外层 adapter；ProtocolMapper 属于 Business Layer。

进入 GSR Runtime 之前，外部输入必须先转换成 `Command`：

```text
Client Connection
  ↓
Login Adapter 完成握手和 secret 交换
  ↓
LoginService 完成认证状态与票据编排
  ↓
Gateway Adapter
  ↓
验证登录证明并绑定连接
  ↓
ProtocolMapper
  ↓
Command
  ↓
Runtime.Send / Runtime.Call
  ↓
Service
```

Core Runtime 不理解连接、fd、HTTP path、WebSocket frame、JSON 包名、客户端协议号、登录 token 或 `secret`。

登录握手和 `secret` 交换只能存在于 Login Adapter。认证状态和票据策略属于 LoginService。连接和链路细节只能存在于 Login/Gateway Adapter。业务协议到 `Command` 的映射只能存在于 ProtocolMapper。

### Business Layer 不污染 Core Runtime

Battle、Room、Player、Wallet、PlayerModule 都属于 Business Layer。

Core Runtime 不知道这些概念。

### 工程化能力不污染 Core Runtime

`skynet_fly` 的服务组、热更新、访问者追踪、远程订阅、录制回放都有参考价值。

但 GSR 的 Core Runtime 不直接内建这些概念。它只提供稳定底座：

```text
ServiceRef
Command
Envelope
Mailbox
Scheduler
Send / Call
```

服务组路由、Drain、Record/Replay 必须放在 Runtime Tooling 中。具体游戏业务封装必须放在 Business Layer 中。

## 设计取舍

| 问题 | 结论 |
|-|-|
| 一个 Service 一个 goroutine？ | 否。默认使用 Mailbox + Scheduler + 固定执行许可池。 |
| 内部消息是否都用 protobuf？ | 否。protobuf 只作为跨节点边界协议。 |
| 是否保留 Skynet PTYPE？ | 否。GSR 内部统一为 Envelope + Command。 |
| Battle 是否内建进 Runtime？ | 否。Battle 是 Business Layer 封装。 |
| ServiceGroup 是否进入 Core Runtime？ | 否。它是 Runtime Tooling 能力。 |
| Gateway 是否进入 Core Runtime？ | 否。Gateway 是外层入口适配，进入 Runtime 前必须映射为 Command。 |
| LoginService 是否进入 Core Runtime？ | 否。Login Adapter 和 LoginService 都是 Runtime Tooling；前者负责握手，后者负责认证状态和票据编排。 |
| secret 是否作为业务 Command 参数传递？ | 否。业务只接收已认证身份上下文。 |
| 热更新是否第一版实现？ | 否。第一版只保证生命周期清晰，后续做 Drain。 |
| 录制回放是否替代 Snapshot？ | 否。Snapshot 是状态，Record 是输入序列。 |
| quix 的 PlayerAgent 是否进入 Core Runtime？ | 否。它启发的是 Business Layer 的 PlayerService/PlayerModule。 |
| 是否引入 Consul/Nacos？ | 第一版不引入。用简单 DiscoveryService。 |
| 是否直接整理聊天原文？ | 否。正式 RFC 只保留最终结论。 |

## 实现顺序

实现顺序以 [RFC-0500](RFC-0500-Roadmap.md) 为准。总体约束保持不变：先稳定 Core Runtime，再按可独立验收的纵向切片实现 Runtime Tooling，最后实现 Business Layer 和端到端业务示例。
