# RFC-0260：ServiceGroup 与路由策略

> 状态：草案
> 目标阶段：Phase 9
> 范围：Runtime Tooling
> 依赖：[RFC-0200](RFC-0200-Tooling-Discovery.md)、[RFC-0130](RFC-0130-Core-Send-Call-Reply.md)
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
    Get(ctx context.Context, group string) (ServiceSet, error)
}
```

该 API 是扩展层封装，底层仍然调用：

```go
runtime.Send(ref, cmd, payload)
runtime.Call(ctx, ref, cmd, payload)
```

## 与 Discovery 的关系

Phase 7B 的最小 Discovery 只保存节点租约和长期 `ServiceName`，不保存 ServiceGroup。Phase 9 必须在 `tooling/servicegroup` 内提供独立的 `DirectoryService`，由它通过 Command 保存：

```text
ServiceGroup -> ServiceSet{Version, Refs, Tags}
```

RoutingPolicy 决定如何用这些事实。

Discovery 只用于定位长期命名的 `DirectoryService`。禁止把 ServiceSet、负载均衡、取模或广播策略加入 `tooling/discovery`。

Watch 不返回由 Client 私自维护的 channel。订阅者使用 `ServiceRef + CommandID` 注册，DirectoryService 在版本变化时投递包含完整 `ServiceSet` 的 Command；订阅、取消和过期清理都必须通过 DirectoryService 的 Mailbox。

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
8. `Call` 只允许解析到一个目标；Broadcast Call 第一版返回稳定错误，不隐式选择“第一个 Reply”。
9. ServiceSet 的 Refs 必须去重并稳定排序，所有查询和通知返回独立副本。
10. DirectoryService 的状态只通过 Command 修改，不创建 goroutine；订阅过期清理由 Timer Command 驱动。
