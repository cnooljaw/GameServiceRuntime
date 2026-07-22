# RFC-0110：ServiceRef 与寻址

> 状态：已接受
> 范围：Core Runtime、Cluster
> 依赖：[RFC-0100](RFC-0100-Core-Service.md)
> 依据：`docs/learn/006-Go-Service-Runtime概要设计与约定.md`

## 目的

本文定义 ServiceRef、ServiceID、ServiceName 和 NodeID 的关系。

## 核心结论

`ServiceRef` 是地址，不是对象引用。

业务永远持有地址，不持有对象指针。

## 类型草案

```go
type NodeID string
type ServiceID uint64
type ServiceName string

type ServiceRef struct {
    Node NodeID
    ID   ServiceID
}
```

## ServiceID

`ServiceID` 表示当前节点内的运行实例。

特点：

- 生命周期短。
- 节点内唯一。
- Service 关闭后失效。
- 不适合当长期业务名字。

## ServiceName

`ServiceName` 表示长期逻辑服务。

适合：

```text
.db
.match
.config
.discovery
```

不适合：

```text
battle_10001
room_20002
```

## 临时服务和长期服务

### 临时服务

例如：

- `BattleService`
- 单局 `RoomService`

使用 `ServiceRef` 直接传递。

### 长期服务

例如：

- `DBService`
- `MatchService`
- `ConfigService`

可以注册 `ServiceName`。

## Registry 分层

```text
LocalRegistry
  ServiceID -> ServiceInstance

NameRegistry
  ServiceName -> ServiceRef
```

`LocalRegistry` 解决实例查找。

`NameRegistry` 解决长期服务解析。

第一版由 `ServiceSpec.Name` 完成注册，通过 `Runtime.Resolve(name)` 解析。重复名称返回 `ErrServiceNameConflict`；Service 退出时名称必须同步注销。

已知目标节点时，可使用节点级查询解析该节点的本地名字：

```go
ref, err := runtime.ResolveRemote(ctx, node, name)
```

`ResolveRemote` 是默认 Cluster bootstrap 能力，不是全局 Discovery。它只查询指定节点的 `NameRegistry`，用于从稳定名字取得动态 `ServiceRef`。调用方已知服务所在节点时，不需要启用 Discovery。只有调用方不应知道服务所在节点，或系统需要动态迁移、节点目录和控制面时，才使用 Runtime Tooling Discovery。

关闭后的 `ServiceRef` 可以在短期 tombstone 窗口内返回 `ErrServiceClosed`。tombstone 必须同时受 TTL 和数量上限约束，不能随短生命周期 Service 数量永久增长；窗口过期后返回 `ErrServiceNotFound`。

## Cluster 下的地址

`ServiceID` 只在节点内唯一。

完整地址必须包含：

```text
NodeID + ServiceID
```

Router 根据 `ServiceRef.Node` 决定本地投递还是远程投递。

`ServiceID(0)` 保留为节点级 Runtime endpoint 和 Runtime caller。业务 Service 不会获得 ID 0；Cluster 只允许该 endpoint 处理 Core 私有的名字查询 Call。

## 规则

1. `CreateService` 返回 `ServiceRef`。
2. 业务不得保存 Service 指针。
3. 临时服务不得注册全局名字。
4. 长期服务注册名字后，调用方仍然通过 `ServiceRef` 通信。
5. `ServiceRef` 失效时，Call 返回 `ErrServiceNotFound` 或 `ErrServiceClosed`。
6. ServiceName 在单个 Registry 中唯一，实例退出时自动注销。
7. 关闭地址记录必须有 TTL 和容量上限。
8. 远程启动配置只保存节点地址和稳定 `ServiceName`，不得依赖动态分配的 ServiceID。
9. 基础 Cluster 默认使用静态节点配置和节点内名字解析，不依赖 Discovery。

## 为什么不用对象指针

对象指针无法跨节点。

对象指针会绕过 Mailbox、Scheduler、Trace、Timeout 和 Cluster。

使用 `ServiceRef` 才能保持本地和远程调用一致。
