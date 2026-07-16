# Skynet 设计思想与优雅实现

本文整理 Skynet 值得 Go 游戏服务器学习的核心思想。这里不把 Skynet 当成 Lua 框架学习，而是把它当成一个成熟的游戏服务器运行时范本。

## 一句话结论

Skynet 不是普通的 Actor Framework，也不是 RPC Framework。

更准确的理解是：

```text
Skynet 是一个以 Service 为运行单位、以消息为唯一入口、以轻量调度为核心的 Message Runtime。
```

## 最值得学习的设计

### Service 是运行时实体

Skynet 的核心不是类，也不是对象方法，而是 `skynet_context` 代表的运行时实体。

一个 Service 同时拥有：

- 地址。
- 生命周期。
- 状态。
- 消息队列。
- 调度入口。
- 协议分发能力。

Go 版 Runtime 应继承这个思想：

```text
Service = addressable runtime entity
```

不要把 Service 理解成普通 Go struct，也不要让业务代码拿到 Service 指针互相调用。

### Handle 是地址，不是对象引用

Skynet 使用 handle 定位 Service。handle 不是指针，也不是对象引用。

它的价值是：

- 跨线程安全。
- 跨 Lua VM 安全。
- 跨 Service 不共享对象。
- 允许 Cluster 扩展到远程节点。

Go 版应使用：

```go
type ServiceID uint64

type ServiceRef struct {
    Node NodeID
    ID   ServiceID
}
```

业务持有 `ServiceRef`，不持有 `*BattleService`、`*PlayerService`。

### Service 拥有状态

Skynet 的一个重要启发是：状态有明确 owner。

不要设计：

```text
RoomManager
PlayerManager
BattleManager
```

再让所有逻辑到 Manager 里拿共享状态。

更好的方式是：

```text
RoomService owns RoomState
PlayerService owns PlayerState
BattleService owns BattleState
```

状态只允许通过 Command 修改。

### 消息是唯一入口

Skynet 的调用不是对象方法调用，而是消息投递。

错误方向：

```go
battle.Kick(playerID)
```

正确方向：

```go
runtime.Call(ctx, battleRef, CmdKickShrew, req)
```

这样所有访问都经过：

- Mailbox。
- Scheduler。
- Trace。
- Timeout。
- Cluster。
- Monitor。

### Send 和 Call 共用同一套模型

Skynet 的 `send` 和 `call` 不是两套框架。

它们共享消息管道，区别只是是否带 `session`：

```text
Send: session = 0
Call: session > 0
```

Go 版应统一成：

```go
type Envelope struct {
    Source  ServiceRef
    Target  ServiceRef
    Session SessionID
    Command CommandID
    Payload any
}
```

`Send` 和 `Call` 都生成 `Envelope`，再交给 Router 投递。

### Session 是 Call/Reply 的生命线

Skynet 的同步调用依赖 session。

调用方：

```text
生成 session
保存 pending
发送消息
等待 Reply
```

被调用方：

```text
收到 session
执行业务
按 session 回复
```

Go 版不应把 `Call` 做成传统 RPC Stub，而应保留 session 模型。

### Timer 产生消息

Skynet 的 timer 不直接修改业务状态。

正确模式：

```text
Timer
  ↓
Command
  ↓
Mailbox
  ↓
Service.Handle
```

禁止：

```go
time.AfterFunc(d, func() {
    battle.state = ...
})
```

Timer 只能投递 Command，让 Service 在自己的消息序列中处理状态变化。

### Cluster 是远程消息管道

Skynet Cluster 的优雅之处在于：它没有重新发明一套 RPC。

本地：

```text
ServiceRef -> Mailbox -> Command Handler
```

远程：

```text
ServiceRef -> Cluster Transport -> Remote Mailbox -> Command Handler
```

两者差异只在 Transport。

Go 版不要设计成：

```text
本地一套 API
远程一套 gRPC API
```

应保持：

```go
runtime.Call(ctx, ref, cmd, req)
runtime.Send(ref, cmd, msg)
```

业务不关心 `ref` 指向本地还是远程。

## Skynet 为什么优雅

### API 小

Skynet 的常用能力很少：

- 创建服务。
- 发送消息。
- 同步调用。
- 回复。
- 定时。
- 退出。

复杂业务由基础原语组合出来。

Go 版也应坚持 Primitive First：

```text
先提供稳定原语，再提供上层封装。
```

### 没有到处都是 Manager

Skynet 不鼓励通过全局 Manager 共享状态。

Go 游戏服务器常见问题是：

```text
一个 GameServer 里挂满 Manager
Manager 之间互相调用
最后边界全部消失
```

应改成：

```text
Service owns state
ServiceRef routes command
Command mutates state
```

### Service 不是线程

Skynet 的轻量来自：

```text
Service != Thread
Service = State + Queue + Dispatch
```

Go 版不要简单做：

```go
go service.loop()
```

尤其不要一个 Service 一个 goroutine 固定常驻。

更好的方式是：

```text
Service Mailbox
  ↓
Ready Queue
  ↓
Worker Pool
```

### 配置、名称、实例分离

Skynet 里 handle、name、cluster node 是不同层次。

Go 版也要分清：

- `ServiceID`：运行实例。
- `ServiceName`：长期逻辑服务。
- `NodeID`：节点。
- `ServiceRef`：可路由地址。

临时 `BattleService` 使用 `ServiceRef`。

长期 `.db`、`.match`、`.config` 可以注册名字。

## Go 版应继承什么

必须继承：

- Service 作为运行时实体。
- ServiceRef 作为地址。
- Command 作为唯一入口。
- Send/Call 共用 Pipeline。
- Timer 产生 Command。
- Cluster 只是远程 Transport。
- Service 拥有状态。
- Mailbox + Scheduler 控制执行。

可以增强：

- Go 泛型类型安全。
- protobuf 作为跨节点 Wire Envelope。
- Snapshot/Restore。
- Supervisor。
- Metrics/Trace/Monitor。
- Game Layer 友好封装。

不要继承：

- Lua 动态协议字符串。
- 字符串 command。
- 早期 `Actor` 命名。
- 传统 RPC Stub。
- 微服务注册中心思维。

## 最终评价

Skynet 的价值不在 Lua，不在某个源码技巧，而在它把游戏服务器问题压缩成了几个稳定原语：

```text
Service
Address
Message
Queue
Session
Timer
Cluster
```

Go 版 GSR 应该保留这些原语，再用 Go 的类型系统、测试、工具链和工程约束把它变成更适合现代团队维护的 Runtime。

