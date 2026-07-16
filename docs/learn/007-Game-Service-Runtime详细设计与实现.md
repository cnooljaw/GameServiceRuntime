# Game Service Runtime 详细设计与实现

本文定义 GSR 的最终分层：Core Runtime 保持通用，Game Layer 提供游戏友好封装。

后续实现时，不要把 Battle、Room、Wallet 直接塞进 Runtime 内核。

## 最终分层

```text
Core Runtime
  ├── Service
  ├── ServiceRef
  ├── Command
  ├── Send / Call
  ├── Mailbox
  ├── Scheduler
  ├── Timer
  └── Cluster

Game Layer
  ├── Battle
  ├── Room
  ├── Player
  ├── Wallet
  ├── Timeline
  ├── Broadcast
  ├── Reconnect
  └── Settlement
```

修正后的原则：

```text
Core Runtime 不知道 Battle。

Game Layer 知道 Battle，并基于 Core Runtime 封装更亲近游戏开发者的 API。
```

## 目录建议

```text
gsr/
  docs/
  runtime/
    service/
    command/
    mailbox/
    scheduler/
    timer/
    cluster/
    discovery/
    supervisor/
    monitor/
  game/
    battle/
    room/
    player/
    wallet/
    timeline/
    broadcast/
  examples/
    whack_mole/
  benchmark/
  tools/
  templates/
```

## Core Runtime 详细设计

### ServiceInstance

Runtime 内部不要直接暴露业务 Service。

```go
type ServiceInstance struct {
    Ref      ServiceRef
    Handler  Service
    Mailbox  *Mailbox
    Status   ServiceStatus
    Policy   ServicePolicy
    Commands *CommandRegistry
}
```

状态：

```go
type ServiceStatus int

const (
    ServiceCreated ServiceStatus = iota
    ServiceStarting
    ServiceRunning
    ServiceStopping
    ServiceClosed
    ServiceFailed
    ServiceRestarting
)
```

### CreateService 流程

```text
CreateService
  ↓
Validate ServiceSpec
  ↓
Allocate ServiceID
  ↓
Create Service instance
  ↓
Create Mailbox
  ↓
Register LocalRegistry
  ↓
Init(ServiceContext)
  ↓
Status = Running
  ↓
return ServiceRef
```

实现要求：

1. `CreateService` 返回 `ServiceRef`，不返回 Service 指针。
2. `Init` 失败必须清理 Registry 和 Mailbox。
3. Service 创建成功后才能接收消息。

### Stop 流程

```text
Stop
  ↓
Mark Stopping
  ↓
Reject new Call
  ↓
Drain or discard mailbox by policy
  ↓
Call Service.Stop
  ↓
Call Service.Close
  ↓
Remove registry
  ↓
Wake pending sessions with ErrServiceClosed
```

Battle 类临时服务通常可以快速关闭。

Player、Wallet 这类状态服务需要先保存或提交状态。

### Command 执行流程

```text
Envelope
  ↓
Mailbox
  ↓
Scheduler Worker
  ↓
CommandDispatcher
  ↓
Service.Handle
  ↓
Reply or no reply
```

`Service.Handle` 必须由 Runtime 包裹 `recover`：

```go
defer func() {
    if r := recover(); r != nil {
        supervisor.ReportPanic(instance.Ref, r)
    }
}()
```

### Call/Reply 实现

`Call` 需要：

```go
type PendingCall struct {
    Session SessionID
    Source  ServiceRef
    Target  ServiceRef
    Command CommandID
    Done    chan Result
    Deadline time.Time
}
```

流程：

```text
Call
  ↓
Allocate Session
  ↓
Save PendingCall
  ↓
Send Envelope(session > 0)
  ↓
Wait ctx.Done or Reply
  ↓
Delete PendingCall
```

`Reply` 流程：

```text
CommandContext.Reply
  ↓
Validate session
  ↓
Create Response envelope
  ↓
Route back to Source
  ↓
SessionManager wakes pending call
```

### Mailbox 设计

第一版：

```go
type Mailbox struct {
    queue chan Envelope
}
```

性能版：

```go
type Mailbox struct {
    buf   []Envelope
    head  uint64
    tail  uint64
    state MailboxState
}
```

规则：

- Mailbox 满时 `Send` 返回 `ErrMailboxFull`。
- `Call` 遇到 Mailbox 满直接失败，不允许无限等待。
- Scheduler 通过 ready 标记避免同一个 Service 重复入队。

### Scheduler 设计

不要一个 Service 一个 goroutine。

```text
ReadyQueue
  ↓
WorkerPool
  ↓
Service Mailbox
```

每个 Worker：

```go
for {
    ref := readyQueue.Pop()
    instance := registry.Get(ref)
    processBatch(instance, maxBatch)
    if instance.Mailbox.NotEmpty() {
        readyQueue.Push(ref)
    }
}
```

建议指标：

- Command 执行耗时。
- Mailbox 长度。
- Mailbox 等待时间。
- Service 批处理数量。
- Slow Command 次数。

### TimerManager 设计

第一版可用 Go timer 实现正确性。

生产版使用 Timer Wheel：

```text
TimerWheel
  ├── slot 0
  ├── slot 1
  ├── slot 2
  └── ...
```

Timer 到期：

```text
Timer expires
  ↓
Create Envelope
  ↓
Send to target Mailbox
```

禁止 Timer 回调直接改状态。

### ClusterTransport 设计

核心目标：

```text
runtime.Call(ctx, ref, cmd, req)
```

不管 `ref` 指向本地还是远程，业务调用方式相同。

Router：

```go
if ref.Node == localNode {
    localDelivery(envelope)
} else {
    clusterTransport.Send(envelope)
}
```

WireEnvelope：

```protobuf
message WireEnvelope {
  string source_node = 1;
  uint64 source_service = 2;
  string target_node = 3;
  uint64 target_service = 4;
  uint64 session = 5;
  uint32 command = 6;
  bytes payload = 7;
  bool response = 8;
}
```

Connection 状态：

```text
Connecting
Connected
Disconnected
Reconnecting
Closed
```

断线时：

- 未发送的消息按策略失败或重试。
- Pending Call 返回 `ErrRemoteUnavailable` 或 `ErrTimeout`。
- 不把 `EOF`、`connection reset` 暴露给业务。

### DiscoveryService 设计

Discovery 是系统服务，不是业务服务。

第一版可以单点：

```text
DiscoveryService
  ├── RegisterNode
  ├── Heartbeat
  ├── QueryNode
  ├── RegisterName
  └── ResolveName
```

节点启动：

```text
Cluster.Open
  ↓
Listen
  ↓
RegisterNode
  ↓
Heartbeat
  ↓
Sync Node List
```

不要让业务直接操作 Discovery 内部结构。

### Supervisor 设计

Panic 是生命周期事件，不是进程崩溃理由。

策略：

```go
type RestartStrategy int

const (
    RestartNever RestartStrategy = iota
    RestartOnFailure
    RestartAlways
    DestroyOnFailure
)
```

服务建议：

- `BattleService`：`DestroyOnFailure`。
- `PlayerService`：`RestartOnFailure`，配合 Snapshot。
- `WalletService`：优先保证持久化一致性，失败后进入保护状态。

### Monitor 设计

Monitor 应提供：

- Service 列表。
- Service 状态。
- Mailbox 长度。
- Pending Call 数量。
- Timer 数量。
- Slow Command。
- Cluster 连接状态。

第一版只做日志和内存指标，后续再做 HTTP/CLI 工具。

## Game Layer 详细设计

Game Layer 是对 Core Runtime 的包装，不改变内核。

### Battle

Battle 是游戏对局概念。

不要把 `Battle` 做成 Runtime 内建原语。

正确方式：

```go
battleRef, err := game.CreateBattle(ctx, CreateBattleOptions{
    GameID:  "whack_mole",
    Players: players,
})
```

内部仍然调用：

```go
runtime.CreateService(BattleServiceSpec{...})
```

Battle 封装提供：

- 玩家集合。
- Timeline。
- Broadcast。
- Snapshot。
- Metrics。
- Reconnect。
- Settlement。

### BattleContext

Battle 层可以在 `CommandContext` 上包装：

```go
type BattleContext interface {
    CommandContext
    BattleID() BattleID
    Epoch() BattleEpoch
    Players() PlayerCollection
    Timeline() Timeline
    Broadcast(cmd CommandID, payload any) error
}
```

注意：

`BattleContext` 是 game 包能力，不属于 runtime 包。

### Timeline

Timeline 是游戏时间轴，不等于底层 Timer。

底层：

```text
TimerManager
```

上层：

```text
Timeline
```

Timeline 用于：

- 地鼠生成。
- 地鼠过期。
- 回合倒计时。
- 自动托管。
- 重连快照。

建议 API：

```go
type Timeline interface {
    Schedule(at time.Time, cmd CommandID, payload any) (TimelineID, error)
    After(d time.Duration, cmd CommandID, payload any) (TimelineID, error)
    Cancel(id TimelineID) error
    Rev() TimelineRev
}
```

### Broadcast

Battle 广播不应该让业务每次手写循环：

```go
for _, p := range players {
    runtime.Send(p.Ref, cmd, payload)
}
```

封装：

```go
battle.Broadcast(CmdShrewSpawned, event)
battle.Players().Except(playerID).Broadcast(cmd, event)
```

底层仍然是 `runtime.Send`。

### Room

Room 负责组织玩家和创建 Battle。

职责：

- 匹配成功后创建 Battle。
- 保存 `BattleRef`。
- 维护玩家到 Battle 的关系。
- 处理 Battle 结束通知。

Room 不负责 Battle 内部规则。

流程：

```text
Match success
  ↓
RoomService
  ↓
game.CreateBattle
  ↓
BattleService
  ↓
Room saves BattleRef
```

### Player

PlayerService 是玩家状态 owner。

职责：

- 玩家基础状态。
- 当前房间或 Battle 引用。
- 重连状态。
- 结算结果应用。
- 快照或持久化。

Battle 不应直接持有 `*PlayerService`，只持有 `ServiceRef` 或业务层 `PlayerRef`。

### Wallet

WalletService 是强一致边界。

规则：

1. Wallet 状态不要散落在 Battle。
2. 结算通过 Command 进入 Wallet。
3. Wallet 必须有幂等键。
4. Wallet 需要持久化和审计日志。

建议命令：

```text
CmdFreezeAmount
CmdCommitSettlement
CmdRollbackSettlement
CmdGetBalance
```

### Reconnect

重连不是简单重发当前状态。

需要：

- `BattleEpoch`：对局版本。
- `TimelineRev`：时间轴版本。
- `Snapshot`：可恢复视图。
- `PlayerState`：玩家个人状态。

流程：

```text
Player reconnect
  ↓
GateService
  ↓
PlayerService
  ↓
BattleRef
  ↓
Call CmdGetSnapshot
  ↓
Return snapshot + timeline rev
```

### Settlement

结算建议分三段：

```text
Battle calculates result
  ↓
Wallet applies settlement
  ↓
Player updates visible state
```

Battle 不直接改钱包。

## 打地鼠示例

### 服务划分

```text
GateService
RoomService
BattleService
PlayerService
WalletService
```

### Battle Command

```text
CmdStartBattle
CmdSpawnShrew
CmdKickShrew
CmdExpireShrew
CmdGetSnapshot
CmdPlayerReconnect
CmdFinishBattle
```

### 核心状态

```go
type BattleState struct {
    BattleID BattleID
    Epoch    BattleEpoch
    Timeline TimelineState
    Players  map[PlayerID]ServiceRef
    Shrews   map[ShrewID]ShrewState
    Score    map[PlayerID]int64
}
```

### 地鼠生成

```text
CmdStartBattle
  ↓
Timeline.After(spawnDelay, CmdSpawnShrew)
  ↓
CmdSpawnShrew
  ↓
Create ShrewState
  ↓
Broadcast CmdShrewSpawned
  ↓
Timeline.After(ttl, CmdExpireShrew)
```

### 玩家点击

```text
Gate receives client input
  ↓
runtime.Call(battleRef, CmdKickShrew, req)
  ↓
Battle validates epoch/timeline/shrew
  ↓
Update score
  ↓
Reply KickResult
  ↓
Broadcast CmdShrewHit
```

### 地鼠过期

```text
Timeline emits CmdExpireShrew
  ↓
Battle checks shrew status
  ↓
Mark expired
  ↓
Broadcast CmdShrewExpired
```

### 对局结束

```text
CmdFinishBattle
  ↓
Calculate settlement
  ↓
Call WalletService CmdCommitSettlement
  ↓
Notify PlayerService
  ↓
Snapshot final state
  ↓
Stop BattleService
```

## 实现阶段

### Phase 0：文档与术语冻结

输出：

- `glossary.md`
- `api-rules.md`
- `naming-rules.md`
- ADR：为什么不用 Actor。
- ADR：为什么 Cluster 不是 RPC。

### Phase 1：单节点 Core Runtime

实现：

- `ServiceRef`
- `Service`
- `CreateService`
- `Mailbox`
- `Send`
- `CommandDispatcher`

验收：

```text
Service A Send Command to Service B
Service B Handle Command
```

### Phase 2：Call/Reply

实现：

- `SessionID`
- `PendingCall`
- `Call`
- `CommandContext.Reply`
- Timeout。

验收：

```text
Call returns result
Call timeout returns ErrTimeout
Reply twice returns ErrReplyTwice
```

### Phase 3：Scheduler

实现：

- ReadyQueue。
- WorkerPool。
- Batch。
- Fair scheduling。

验收：

```text
大量 Service 不需要大量 goroutine
单个繁忙 Service 不会饿死其它 Service
```

### Phase 4：Timer

实现：

- `After`
- `Cancel`
- Timer 生成 Command。

验收：

```text
Timer 到期后通过 Mailbox 进入 Service
Timer callback 不直接修改状态
```

### Phase 5：Game Layer 最小封装

实现：

- `game.CreateBattle`
- `BattleContext`
- `Timeline`
- `Broadcast`

验收：

```text
打地鼠 Battle 能启动、生成、点击、过期、结束
```

### Phase 6：Cluster

实现：

- `NodeID`
- `ClusterTransport`
- Handshake。
- WireEnvelope。
- Remote Send。
- Remote Call/Reply。

验收：

```text
Gate node Call battle node
业务代码不区分本地远程
```

### Phase 7：Discovery

实现：

- `DiscoveryService`
- RegisterNode。
- Heartbeat。
- ResolveName。

验收：

```text
节点启动后自动注册
长期服务可通过名字解析
临时 Battle 不进入 Discovery
```

### Phase 8：生产能力

实现：

- Snapshot。
- Supervisor。
- Monitor。
- Metrics。
- Benchmark。

## 测试建议

Core Runtime：

- `CreateService` 生命周期测试。
- `Send` 顺序测试。
- `Call/Reply` 超时测试。
- `Reply` 重复调用测试。
- Mailbox 满测试。
- Timer 投递测试。
- Panic recover 测试。

Cluster：

- Remote Send。
- Remote Call。
- 断线时 pending call 失败。
- 节点重连。
- Name resolve。

Game Layer：

- Battle 创建。
- Broadcast。
- Timeline 顺序。
- Reconnect snapshot。
- Settlement 幂等。

## Benchmark 建议

必须关注：

- 每秒 Command 数。
- Mailbox 入队/出队成本。
- Scheduler batch 大小。
- Timer 数量。
- Pending Call 数量。
- Service 数量。
- GC 分配。

不要只测单个函数。

应测完整路径：

```text
Call -> Router -> Mailbox -> Scheduler -> Handler -> Reply
```

## 最终实现准则

1. Core Runtime 保持小而稳定。
2. Game Layer 可以亲近游戏业务，但不能污染 Runtime。
3. Service 只能通过 Command 修改状态。
4. Timer、Cluster、Gateway 都只能投递 Command。
5. 本地和远程共用 Envelope。
6. Battle 是 Game Layer 封装，不是 Runtime 内建类型。
7. Wallet 是强一致服务，不能被 Battle 直接修改。
8. 先完成单节点正确性，再做 Cluster。
9. 文档、RFC、测试必须先于大规模实现。

