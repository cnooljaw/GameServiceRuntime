# RFC-0260：ServiceGroup 与路由策略

> 状态：草案  
> 范围：Runtime Tooling  
> 依据：`skynet_fly` 的服务组、版本化地址表和访问封装

## 目的

本文定义 GSR 的 ServiceGroup 和路由策略。

ServiceGroup 是 Runtime Tooling 能力，不进入 Core Runtime。

Core Runtime 仍然只理解：

```text
ServiceRef
Command
Envelope
Send / Call
```

## 定义

ServiceGroup 表示一组承担同一职责的 Service。

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

`Version` 用于 watch、切换、回滚和避免旧地址长期驻留。

## 路由策略

第一批策略：

```text
Direct:
  直接使用指定 ServiceRef。

Hash:
  根据业务 key 映射到固定 ServiceRef。

RoundRobin:
  在 ServiceGroup 内轮询。

Broadcast:
  向 ServiceGroup 内所有 Service 投递。
```

策略接口草案：

```go
type RouterPolicy interface {
    Pick(set ServiceSet, key RoutingKey) ([]ServiceRef, error)
}
```

`Broadcast` 返回多个 `ServiceRef`。其他策略通常返回一个。

## API 草案

```go
type ServiceGroupClient interface {
    Send(group string, policy RoutingPolicy, cmd CommandID, payload any) error
    Call(ctx context.Context, group string, policy RoutingPolicy, cmd CommandID, payload any) (any, error)
    Watch(ctx context.Context, group string) (<-chan ServiceSet, error)
}
```

该 API 是扩展层封装，底层仍然调用：

```go
runtime.Send(ref, cmd, payload)
runtime.Call(ctx, ref, cmd, payload)
```

## 与 Discovery 的关系

Discovery 保存事实：

```text
ServiceGroup -> ServiceSet{Version, Refs, Tags}
```

RoutingPolicy 决定如何用这些事实。

禁止 Discovery 内建负载均衡、取模、广播策略。

## 与 Cluster 的关系

ServiceGroup 可以跨节点。

```text
Resolve ServiceGroup
  ↓
RoutingPolicy.Pick
  ↓
ServiceRef
  ↓
Local or Remote Send/Call
```

本地和远程仍然共用 `Envelope`。

## 与 Game Layer 的关系

Game Layer 可以封装更明确的业务路由：

```text
MatchWorkerGroup
BattleShardGroup
RoomAllocatorGroup
```

这些封装可以使用 ServiceGroup，但不能把业务名字写入 Core Runtime。

## 规则

1. ServiceGroup 不进入 Core Runtime。
2. ServiceGroup 不改变 `ServiceRef` 语义。
3. Discovery 只保存 ServiceSet，不决定路由策略。
4. 路由策略必须可测试。
5. Broadcast 必须明确错误聚合策略。
6. Hash 策略必须声明 key、哈希函数和扩缩容行为。
7. ServiceSet 变化必须通过版本号发布。
