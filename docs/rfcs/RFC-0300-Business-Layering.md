# RFC-0300：Business Layer 分层

> 状态：草案  
> 范围：Business Layer  
> 依据：`docs/learn/007-Game-Service-Runtime详细设计与实现.md`、Skynet `examples/login` 的 LoginService / Gateway 分工

## 目的

本文定义 Business Layer、Runtime Tooling 与 Core Runtime 的边界。

## 核心结论

ProtocolMapper、Battle、Room、Player、Wallet、Timeline、Broadcast、PlayerModule 和具体示例都属于 Business Layer。

Core Runtime 不知道这些概念。

## 分层

```text
Business Layer
  ├── ProtocolMapper
  ├── Battle
  ├── Room
  ├── Player
  ├── PlayerModule
  ├── Wallet
  ├── Timeline
  ├── Broadcast
  ├── Reconnect
  ├── Settlement
  ├── Business Templates
  └── Game Examples

Runtime Tooling
  ├── LoginService
  ├── Gateway Adapter
  ├── SessionRegistry
  ├── Discovery
  ├── Snapshot
  ├── Supervisor
  ├── Monitor
  ├── Control Plane
  ├── ServiceGroup
  ├── Drain
  └── Record/Replay

Core Runtime
  ├── Service
  ├── ServiceRef
  ├── Command
  ├── Mailbox
  ├── Scheduler
  ├── Timer
  └── Cluster Data Plane
```

## 规则

Business Layer 可以提供：

```go
protocol.MapPacket(...)
game.CreateBattle(...)
battle.Broadcast(...)
battle.Timeline().After(...)
player.RegisterModule(...)
```

但内部必须使用：

```go
runtime.CreateService(...)
runtime.Send(...)
runtime.Call(...)
runtime.After(...)
```

Business Layer 不得绕过 Core Runtime。

Business Layer 可以使用 Runtime Tooling，例如：

```text
PlayerService 使用 Snapshot/Record。
BattleService 使用 Timeline 和 Record/Replay。
RoomAllocator 使用 ServiceGroup。
运维后台通过 Control Plane 触发 Drain。
```

但这些使用关系不能倒灌到 Core Runtime。

## 业务模板

业务模板是可复用的业务组织方式，不是 Runtime 内建框架。模板只约定状态 owner、Command 边界、生命周期和可验证流程；具体游戏可以选择、组合或不使用模板。

第一版提供以下模板方向：

```text
PlayerService：一个玩家的长期状态、上线/离线、持久化编排和玩家模块。
BattleService：一组参与者的一次游戏活动，包含准备、重连、回合、动作和计时器。
RoomService：房间入口、Battle 索引和分配策略。
MatchService：匹配队列、候选者选择和 Battle/Room 创建请求。
TaskService：任务进度、领取状态和周期刷新。
WalletService：余额、流水、幂等结算和持久化。
```

`BattleService` 是棋牌游戏和实时多人玩法的模板，不是所有业务的父模型。`TaskService`、`MatchService` 等简单长期服务应按自身状态直接建模，不能为了统一形式虚构 Room、Battle 或 PlayerModule。

模板之间的依赖方向为：

```text
Gateway Adapter -> ProtocolMapper -> PlayerService / MatchService / TaskService
MatchService or RoomService -> CreateBattle request -> BattleService
BattleService -> settlement request -> WalletService
```

箭头只表示 Command 或领域接口依赖，不表示持有对方对象指针。

## 从既有棋牌游戏框架吸收的边界

已稳定运行的棋牌游戏框架证明了以下业务能力有实际价值：

- 连接与玩家会话的隔离、连接池和断线清理。
- 房间进入、准备、开局、断线重连和结束结算形成完整流程。
- 对局输入、计时器和结果使用可追踪事件，支持录像和复盘。
- 基础逻辑与具体游戏规则分离，允许麻将等玩法在稳定流程上扩展。

GSR 吸收这些结果，但不复制其实现边界：认证和密钥交换由 `LoginService` 负责；协议编解码留在 Gateway/ProtocolMapper；业务状态不依赖全局表、Service 指针或跨对象直接调用；录像以 Command Record/Replay 和业务事件实现。

## Gateway 与 ProtocolMapper 边界

LoginService 和 Gateway Adapter 属于 Runtime Tooling 或外层 adapter。

LoginService 负责登录入口：

- 登录握手。
- `secret` 交换。
- token 校验编排。
- 单点登录、多端登录或顶号策略。
- 签发短期 `LoginTicket`。

Gateway Adapter 负责游戏连接入口：

- 连接管理。
- 登录证明校验。
- 协议解包。
- 限流和断线处理。
- 将响应写回客户端。

ProtocolMapper 属于 Business Layer。它负责把外部协议转换成内部 `Command`：

```text
WebSocket frame / HTTP request / TCP packet
  ↓
LoginService 完成登录认证
  ↓
Gateway Adapter
  ↓
验证 LoginTicket / proof
  ↓
ProtocolMapper
  ↓
CommandID + Payload
  ↓
runtime.Send / runtime.Call
```

Core Runtime 不接收 fd、socket、HTTP path、JSON 包名、客户端协议号、登录 token 或 `secret`。

Gateway 可以持有连接状态，但不能持有玩家权威状态。玩家权威状态必须属于 `PlayerService` 或其他明确的业务 Service。

Gateway 也不应该知道 `CmdJoinRoom`、`CmdSignIn` 这类业务命令的语义。业务协议如何映射到 Command，由 ProtocolMapper 决定。

明文 `secret` 不能作为普通业务 `Command` 的 payload。业务 Service 只接收 `SessionIdentity`、`PlayerID`、`AccountID` 等身份上下文。

## 为什么 Battle 不进 Core Runtime

Battle 是游戏领域概念。

Core Runtime 应保持通用和轻量。

如果把 Battle 内建进 Runtime，DBService、ConfigService、DiscoveryService 等非对局服务会被不必要的游戏语义污染。

## Business Layer 的价值

Business Layer 让游戏开发者少接触底层概念：

- 少写 `ServiceRef` 传递。
- 少写玩家循环广播。
- 少写 Timeline 与 Timer 转换。
- 少写重连快照流程。
- 少写玩家模块生命周期编排。

但所有底层一致性仍由 Core Runtime 保证。

## quix 的归属

`quix` 的一个玩家一个 agent、agent 内多模块、模块生命周期、玩家数据保存、网关重连等设计，属于 Business Layer。

GSR 可以学习它的业务组织方式：

```text
PlayerService owns PlayerState
PlayerService composes PlayerModule
Gate forwards client packets
ProtocolMapper maps packet to Command
PlayerStateRepository handles persistence
```

但不能把 `PlayerAgent`、`onPlayerOnline`、`onBackup` 等具体业务生命周期放进 Core Runtime。
