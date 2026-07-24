# ServiceGroup：版本化成员事实与显式路由

“Discovery 已经能找到 Service，为什么还要 DirectoryService？”

“因为找到一个长期名字，和管理一组可替换副本，是两种事实。”

## ServiceSet

```go
type ServiceSet struct {
    Name    GroupName
    Version ServiceSetVersion
    Refs    []gsr.ServiceRef
    Tags    map[string]string
}

type ServiceSetVersion struct {
    AuthorityEpoch uint64
    Revision       uint64
}
```

ServiceSet 是完整快照，不是“增加一个 Ref”的事件。

`AuthorityEpoch` 区分 Directory 权威代际，`Revision` 区分同一权威内的修改。比较版本时先看 Epoch，再看 Revision。

## DirectoryService

```go
directory, err := servicegroup.NewDirectoryService(
    servicegroup.DirectoryConfig{
        PublisherNode: "control-node",
    },
)
```

只有可信 PublisherNode 可以发布。发布使用 compare-and-set：

```text
expected version == current version
  -> publish next version
否则
  -> ErrVersionConflict
```

这防止两个运维流程互相覆盖。

## Watch 是 lease

订阅者通过 Call 获得：

```go
type WatchResult struct {
    Lease   WatchLease
    Current ServiceSet
    Found   bool
}
```

后续变更以 `ServiceSetChangedCommand` 投递完整快照。订阅需要续租；过期后 Directory 清理关系。

没有后台魔法 cache，关系仍是显式 Command 和 lease。

## Router 接收显式快照

```go
router, err := servicegroup.NewRouter(runtime)

err = router.Send(
    set,
    servicegroup.Hash{},
    servicegroup.RoutingKey("player-42"),
    command,
    payload,
)
```

Router 不在 Send 内同步查询 Directory。调用方明确选择哪个版本的 ServiceSet。

## 三种策略

Hash：

```go
servicegroup.Hash{}
```

使用 RoutingKey 的 FNV-1a 结果稳定选择一个成员，适合玩家分片。

RoundRobin：

```go
policy := &servicegroup.RoundRobin{}
```

计数属于 policy 实例，适合无粘性的均衡请求。

Broadcast：

```go
err := router.Send(set, servicegroup.Broadcast{}, "", command, payload)
```

逐目标尝试。部分失败返回 `BroadcastError`，已接受的目标不会回滚。

Call 必须只选一个目标，Broadcast 不能用于 Call。

## 与 Drain 的关系

热更新时：

```text
version 7: [old]
version 8: [new]
```

先让新实例 ready，再 CAS 发布更高版本。旧调用方可能仍持有 version 7，因此还需要旧实例入口 Guard 和 Visitor lease，不能只改 Directory 就立即 Stop。

## 可运行例子

```bash
go run ./examples/servicegroup-runtime
```

## 对照源码

- `tooling/servicegroup/types.go`
- `tooling/servicegroup/service.go`
- `tooling/servicegroup/routing.go`
- `tooling/servicegroup/watch_test.go`

## 本章小结

Discovery 保存“长期名字在哪里”，Directory 保存“这个组当前有哪些成员和版本”，Router 只消费调用方显式持有的快照。三个职责不能互相吞并。
