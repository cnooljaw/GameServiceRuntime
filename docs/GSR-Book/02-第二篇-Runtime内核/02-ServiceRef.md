# ServiceRef：地址、名字与实例不是一回事

小林写下：

```go
const PlayerServiceID = 1001
```

老周问：“重启后还是它吗？另一台节点也能用 1001 吗？恢复出的新实例为什么要冒充旧实例？”

## 地址模型

`ServiceRef` 由节点和实例 ID 组成：

```go
type ServiceRef struct {
    Node NodeID
    ID   ServiceID
}
```

例如：

```text
node-a/17
```

它表示“node-a 当前 Runtime 创建的第 17 个实例”，不是业务主键，也不是长期名字。

## 三种身份

| 类型 | 回答的问题 | 示例 |
| --- | --- | --- |
| `ServiceRef` | 当前实例在哪里？ | `node-a/17` |
| `ServiceName` | 这个节点上的长期角色叫什么？ | `.discovery` |
| 业务 ID | 业务实体是谁？ | `battle-42` |

不要把它们互相代替。

Battle 恢复后：

```text
BattleID: battle-42       保持
old ServiceRef: node-a/17 失效
new ServiceRef: node-b/9  生效
```

## 本地名字

创建时可以指定 `ServiceName`：

```go
ref, err := runtime.CreateService(gsr.ServiceSpec{
    Name:    ".config",
    Service: configService{},
})
```

同一 Runtime 内：

```go
resolved, err := runtime.Resolve(".config")
```

名字是长期角色，Ref 是当前实例。Registry 保证同一时刻名字不会绑定两个本地实例。

## 远程名字

如果调用方已经知道节点：

```go
ref, err := nodeA.ResolveRemote(ctx, "node-b", ".config")
```

它只查询 node-b 的本地 Registry，不是全局目录。

如果连节点也不应该由调用方知道，应使用：

- Discovery：节点 lease 和长期名字；
- ServiceGroup：一组实例的版本化 ServiceSet；
- 业务目录：业务 ID 到 Ref 的权威映射。

## 为什么旧 Ref 不能复活

假设旧 Ref 在恢复后重新指向新对象：

```text
node-a/17 -> old Battle
node-a/17 -> new Battle
```

缓存旧地址的调用方会在不知情时跨越代际，迟到 Command 也可能进入新状态。GSR 选择更明确的失败：

```text
old Ref -> ErrServiceNotFound / ErrServiceClosed
new Ref -> 正常处理
```

调用方必须通过名字、Directory 或业务流程取得新 Ref。

## ServiceID(0)

`ServiceID(0)` 保留给 Core 节点端点，用于节点级协议。系统 Service 仍使用动态 ID 和稳定名字：

```text
.discovery       -> node-b/3
.node-agent      -> node-b/4
.cluster-observer -> node-a/2
```

GSR 不引入魔法 `SystemServiceID`。

## 复制地址，不持有对象

跨 Service 字段应长这样：

```go
type battleService struct {
    wallet gsr.ServiceRef
}
```

不应长这样：

```go
type battleService struct {
    wallet *walletService
}
```

第一种写法保留 Mailbox 和生命周期边界，第二种写法把本地对象调用偷偷带回来了。

## 对照源码

- 类型：`runtime/types.go`
- 本地 Registry：`runtime/registry.go`
- 远程解析：`runtime/core_endpoint.go`
- 名字与 tombstone 测试：`runtime/runtime_test.go`

## 本章小结

一句话记住：

```text
Ref 找实例，Name 找角色，业务 ID 找领域实体。
```

三者分开，恢复、迁移和迟到消息才有明确语义。
