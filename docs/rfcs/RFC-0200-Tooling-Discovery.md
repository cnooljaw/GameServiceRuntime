# RFC-0200：DiscoveryService

> 状态：草案  
> 范围：Cluster、系统服务  
> 依据：`docs/learn/006-Go-Service-Runtime概要设计与约定.md`

## 目的

本文定义 GSR 的节点发现和长期服务名解析。

## 两类发现

必须区分：

```text
Node Discovery:
  哪些节点存活，节点地址是什么。

Service Name Discovery:
  长期服务名解析到哪个 ServiceRef。

Service Group Discovery:
  一组同职责 Service 的地址列表和版本。
```

不要混成传统微服务注册中心。

## 第一版方案

第一版使用简单 `DiscoveryService`。

它是系统服务，不是业务服务。

命令：

```text
CmdRegisterNode
CmdUpdateNodeDesiredState
CmdHeartbeat
CmdQueryNode
CmdListNodes
CmdRegisterName
CmdResolveName
CmdRegisterServiceGroup
CmdUpdateServiceGroup
CmdWatchServiceGroup
CmdResolveServiceGroup
```

第一版可以只实现 Node Discovery 和 Service Name Discovery。

Service Group Discovery 是后续扩展，不阻塞 Core Runtime。

## Desired State 与 Observed State

节点信息必须分为两类。

Desired State 来自配置、控制台或部署系统：

```go
type NodeDesiredState struct {
    ID      NodeID
    Address string
    Role    string
    Enabled bool
}
```

Observed State 来自心跳和运行时观测：

```go
type NodeObservedState struct {
    ID        NodeID
    Status    NodeStatus
    LastSeen  time.Time
    Latency   time.Duration
    Version   string
    LastError string
}
```

不要把配置期望和运行状态写进同一个不可区分的结构。

## Node 注册

节点启动：

```text
Cluster.Open
  ↓
Listen TCP
  ↓
RegisterNode
  ↓
Heartbeat
  ↓
Sync Node List
```

NodeInfo：

```go
type NodeInfo struct {
    Desired NodeDesiredState
    Observed NodeObservedState
}
```

## ServiceName 注册

长期服务启动后：

```go
runtime.RegisterName(".match", matchRef)
```

Discovery 保存：

```text
.match -> ServiceRef{Node: "match-1", ID: 100}
```

## ServiceGroup 发现

`ServiceGroup` 用于表达同一职责的多实例服务。

示例：

```text
match-worker -> [
  ServiceRef{Node: "match-1", ID: 101},
  ServiceRef{Node: "match-1", ID: 102},
  ServiceRef{Node: "match-2", ID: 201},
]
```

ServiceGroup 必须带版本：

```go
type ServiceSet struct {
    Name    string
    Version uint64
    Refs    []ServiceRef
    Tags    map[string]string
}
```

客户端可以通过 `CmdWatchServiceGroup` 等待版本变化，再由路由层切换引用。

Discovery 只保存事实，不决定业务路由策略。

## 临时服务不注册

Battle、单局 Room 等临时实例不进入 Discovery。

Room 持有 Battle 的 `ServiceRef`。

## 后续方案

后续可考虑 Gossip。

但第一版不引入复杂共识系统。

## 规则

1. Discovery 不负责业务状态。
2. Discovery 不负责 Battle 实例分配。
3. Discovery 只存节点和长期服务名。
4. 业务不直接访问 Discovery 内部结构。
5. Discovery 不执行远程管理命令，只提供节点和名字的事实来源。
6. 节点状态必须能表达配置存在但当前未连接的情况。
7. ServiceGroup 是扩展层能力，不改变 `ServiceRef` 的含义。
8. Discovery 不内建负载均衡、取模或广播；这些属于 RoutingPolicy。
