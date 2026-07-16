# ServiceRef

> 状态：已实现
>
> 规范：[RFC-0110](../../rfcs/RFC-0110-Core-ServiceRef.md)

## 地址模型

```go
type ServiceRef struct {
    Node NodeID
    ID   ServiceID
}
```

`ServiceRef` 是地址和值对象，不是 Service 指针。`NodeID + ServiceID` 唯一标识一个运行实例，因此本地和远程目标可以共用同一个 API。

```text
本地 Ref  -> Local Registry -> Mailbox
远程 Ref  -> Cluster Router -> Transport -> 远端 Mailbox
```

## ServiceName

`ServiceName` 只用于长期服务名解析。创建命名 Service 后可以通过 `Runtime.Resolve` 得到当前 `ServiceRef`。

Battle 等临时实例不依赖全局名字。Room 或其他 owner 应直接持有它们的 `ServiceRef`。

## 生命周期规则

- Service 停止后，旧 `ServiceRef` 不会指向新实例。
- Runtime 使用有界 tombstone 区分“从未存在”和“已经关闭”。
- `ServiceRef` 可以序列化和跨节点传递，但不能反查 Service 对象。
- Discovery 只负责长期名字和节点事实，不改变 `ServiceRef` 的含义。

跨节点地址和名字发现分别见 [RFC-0190](../../rfcs/RFC-0190-Core-Cluster-Data-Plane.md) 与 [RFC-0200](../../rfcs/RFC-0200-Tooling-Discovery.md)。
