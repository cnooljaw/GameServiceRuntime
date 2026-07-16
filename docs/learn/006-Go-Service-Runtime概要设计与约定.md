# Go Service Runtime 概要设计与约定

本文定义基于 Skynet 思想的 Go Runtime 内核。这里的 Runtime 是通用内核，不包含 Battle、Room、Wallet 等游戏上层封装。

## 设计目标

Go Service Runtime 要解决四个问题：

1. 让 Service 成为状态拥有者。
2. 让 Command 成为状态修改的唯一入口。
3. 让本地和远程调用共享同一条消息管道。
4. 让 Runtime 统一管理生命周期、调度、Timer、Cluster、Monitor。

## 非目标

第一版不要做：

- 完整 Cluster。
- 热迁移。
- 复杂 Discovery。
- 自动分片。
- 完整 Supervisor Tree。
- 复杂 Monitor Console。

第一版只需要跑通：

```text
CreateService -> Send -> Call -> Reply -> Timer
```

## 稳定命名

### 使用这些名称

```text
Service
ServiceRef
ServiceRuntime
ServiceContext
Command
CommandID
CommandContext
CreateService
Send
Call
Reply
After
TimerID
Mailbox
Scheduler
ClusterTransport
DiscoveryService
```

### 禁止这些名称

```text
Actor
ActorRef
Spawn
Tell
Ask
Request
Attack
RPC Stub
Manager
```

说明：

- `Actor` 只作为设计思想背景，不作为代码命名。
- `Request` 偏 HTTP/RPC，最终统一为 `Call`。
- `Attack` 只是动作，游戏对局统一叫 `Battle`。
- `Manager` 容易变成共享状态入口，优先使用具体 Service。

## 总体架构

```text
ServiceRuntime
  ├── Registry
  ├── Router
  ├── Mailbox
  ├── Scheduler
  ├── CommandDispatcher
  ├── SessionManager
  ├── TimerManager
  ├── ClusterTransport
  ├── DiscoveryService
  ├── Supervisor
  └── Monitor
```

第一版可以只实现：

```text
Registry
Router
Mailbox
CommandDispatcher
SessionManager
TimerManager
```

## 核心模型

### Service

Service 是运行时实体，不是普通对象。

它负责：

- 持有业务状态。
- 注册 Command。
- 串行处理进入自身 Mailbox 的消息。
- 在 Runtime 生命周期内启动和关闭。

建议接口：

```go
type Service interface {
    Init(ctx ServiceContext) error
    Handle(ctx CommandContext, cmd Command) error
    Stop(ctx context.Context) error
    Close() error
}
```

后续可扩展：

```go
type Stateful interface {
    Snapshot() ([]byte, error)
    Restore([]byte) error
}
```

### ServiceRef

`ServiceRef` 是地址，不是对象引用。

```go
type NodeID string
type ServiceID uint64

type ServiceRef struct {
    Node NodeID
    ID   ServiceID
}
```

规则：

1. 业务只能保存 `ServiceRef`。
2. 业务不能保存另一个 Service 的指针。
3. 本地和远程都使用同一个 `ServiceRef`。
4. 临时服务只使用 `ServiceRef`，不要注册全局名字。

### ServiceName

长期服务可以注册名字：

```go
type ServiceName string
```

例如：

```text
.db
.match
.config
```

临时 `BattleService` 不注册名字。

## Command 模型

Command 是 Service 的能力入口。

它不是：

- DDD Command。
- CQRS Command。
- RPC Method。

它是：

```text
Runtime Message Entry
```

建议类型：

```go
type CommandID uint32

type Command struct {
    ID      CommandID
    Payload any
}
```

第一版使用 `any`，后续通过泛型包装和代码生成增强类型安全。

### CommandRegistry

每个 Service 声明自己支持的 Command。

```go
type CommandRegistry interface {
    Register(id CommandID, handler CommandHandler) error
}

type CommandHandler func(ctx CommandContext, payload any) error
```

后续可加入泛型：

```go
type Handler[Req any, Resp any] func(
    ctx CommandContext,
    req Req,
) (Resp, error)
```

### CommandID 管理

不要团队手写散乱数字。

推荐：

```text
commands.yaml
  battle:
    KickShrew: 10001
    Reconnect: 10002
  player:
    AddCoin: 20001
```

生成：

```go
const (
    CmdKickShrew CommandID = 10001
    CmdReconnect CommandID = 10002
    CmdAddCoin   CommandID = 20001
)
```

## Send、Call、Reply

### 统一 Envelope

```go
type SessionID uint64

type Envelope struct {
    Source  ServiceRef
    Target  ServiceRef
    Session SessionID
    Command CommandID
    Payload any
}
```

### Send

```go
err := runtime.Send(ref, CmdShrewSpawn, event)
```

语义：

- 异步投递。
- 不等待业务结果。
- `Session = 0`。
- 仍然可能返回投递错误。

### Call

```go
result, err := runtime.Call(ctx, ref, CmdKickShrew, req)
```

语义：

- 投递 Command。
- 创建 session。
- 保存 pending。
- 等待 Reply 或超时。

### Reply

`Reply` 属于 `CommandContext`：

```go
type CommandContext interface {
    Self() ServiceRef
    Source() ServiceRef
    Reply(value any) error
}
```

规则：

1. `Send` 进入的 Command 调用 `Reply` 应失败。
2. `Call` 进入的 Command 最多只能 `Reply` 一次。
3. 超时后的 Reply 直接丢弃并记录指标。

## ServiceContext

Service 不应该直接依赖整个 Runtime。

推荐：

```go
type ServiceContext interface {
    Self() ServiceRef
    Send(target ServiceRef, cmd CommandID, payload any) error
    Call(ctx context.Context, target ServiceRef, cmd CommandID, payload any) (any, error)
    After(duration time.Duration, cmd CommandID, payload any) (TimerID, error)
    Now() time.Time
    Logger() Logger
    Metrics() Metrics
}
```

禁止暴露：

- Registry。
- Scheduler。
- Mailbox。
- 所有 Service 列表。
- Cluster 内部连接。

## Mailbox

Mailbox 是 Service 的消息队列。

第一版可用 channel：

```go
type Mailbox struct {
    queue chan Envelope
}
```

后续优化为 ring buffer：

```go
type Mailbox struct {
    queue []Envelope
    head  int
    tail  int
}
```

规则：

1. Service 状态只能在处理 Mailbox 消息时修改。
2. 外部 goroutine 不允许直接修改 Service 状态。
3. Timer、Cluster、Gateway 都只能投递 Envelope。

## Scheduler

不要一个 Service 一个 goroutine。

推荐模型：

```text
Mailbox receives Envelope
  ↓
ReadyQueue receives ServiceRef
  ↓
Worker takes ready Service
  ↓
Batch handle commands
  ↓
Requeue if mailbox not empty
```

调度规则：

- 每次最多处理 N 条 Command，避免单个 Service 独占 Worker。
- 记录 Command 执行耗时。
- 支持优先级，但第一版可不做。

## Timer

Timer 只生成 Command。

```go
timerID, err := ctx.After(5*time.Second, CmdTimeout, payload)
```

过期后：

```text
TimerManager -> Envelope -> Mailbox -> Service.Handle
```

第一版可用 Go timer，性能版使用 Timer Wheel。

## Registry 与 Name

分两层：

```text
LocalRegistry:
  ServiceID -> ServiceInstance

NameRegistry:
  ServiceName -> ServiceRef
```

规则：

- `BattleService`、`RoomInstance` 这类临时服务不注册名字。
- `.db`、`.match`、`.config` 这类长期服务可以注册名字。

## Cluster 概要

Cluster 不是 RPC。

```text
runtime.Call()
  ↓
Router
  ↓
Local or Remote
  ↓
Mailbox or ClusterTransport
```

远程也传 `Envelope`，网络层只是编码：

```protobuf
message WireEnvelope {
  string source_node = 1;
  uint64 source_service = 2;
  string target_node = 3;
  uint64 target_service = 4;
  uint64 session = 5;
  uint32 command = 6;
  bytes payload = 7;
}
```

protobuf 只用于跨节点边界，不强制内部业务全部 protobuf 化。

## Discovery 概要

Discovery 解决两个问题：

1. 哪些 Node 存活。
2. 长期 ServiceName 在哪里。

不要把它做成传统微服务注册中心。

第一版可以有一个简单 `DiscoveryService`：

```text
RegisterNode
Heartbeat
QueryNode
RegisterName
ResolveName
```

后续再考虑 Gossip。

## 错误语义

不要让业务看到底层 TCP 错误。

统一错误：

```go
var (
    ErrTimeout           error
    ErrServiceNotFound   error
    ErrServiceClosed     error
    ErrMailboxFull       error
    ErrRemoteUnavailable error
    ErrReplyTwice        error
)
```

## API 冻结草案

```go
// lifecycle
runtime.CreateService(spec ServiceSpec) (ServiceRef, error)
runtime.Stop(ref ServiceRef) error

// discovery
runtime.Lookup(id ServiceID) (ServiceRef, error)
runtime.Resolve(name ServiceName) (ServiceRef, error)
runtime.RegisterName(name ServiceName, ref ServiceRef) error

// messaging
runtime.Send(ref ServiceRef, cmd CommandID, payload any) error
runtime.Call(ctx context.Context, ref ServiceRef, cmd CommandID, payload any) (any, error)

// timer
runtime.After(ref ServiceRef, d time.Duration, cmd CommandID, payload any) (TimerID, error)
runtime.Cancel(timerID TimerID) error
```

## 开发顺序

1. `ServiceRef`、`Service`、`ServiceSpec`。
2. `CreateService` 和本地 Registry。
3. `Mailbox` 和 `Send`。
4. `CommandDispatcher`。
5. `SessionManager`、`Call`、`Reply`。
6. `Scheduler`。
7. `TimerManager`。
8. 示例：最小 Battle。
9. Cluster。
10. Discovery。
11. Snapshot。
12. Monitor。

核心原则：

```text
先证明单节点模型正确，再扩展分布式能力。
```

