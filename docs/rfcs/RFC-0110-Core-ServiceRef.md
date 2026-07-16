# RFC-0110：ServiceRef 与寻址

> 状态：草案  
> 范围：Core Runtime、Cluster  
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

## Cluster 下的地址

`ServiceID` 只在节点内唯一。

完整地址必须包含：

```text
NodeID + ServiceID
```

Router 根据 `ServiceRef.Node` 决定本地投递还是远程投递。

## 规则

1. `CreateService` 返回 `ServiceRef`。
2. 业务不得保存 Service 指针。
3. 临时服务不得注册全局名字。
4. 长期服务注册名字后，调用方仍然通过 `ServiceRef` 通信。
5. `ServiceRef` 失效时，Call 返回 `ErrServiceNotFound` 或 `ErrServiceClosed`。

## 为什么不用对象指针

对象指针无法跨节点。

对象指针会绕过 Mailbox、Scheduler、Trace、Timeout 和 Cluster。

使用 `ServiceRef` 才能保持本地和远程调用一致。

