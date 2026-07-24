# Discovery：当调用方连节点也不该知道

小林已经会写：

```go
runtime.ResolveRemote(ctx, "node-b", ".config")
```

“如果 `.config` 明天迁到 node-c 呢？”

这才是 Discovery 要回答的问题。

## 它保存两类事实

`tooling/discovery` 保存：

1. 节点 lease：NodeID、Address、Generation、ExpiresAt；
2. 长期名字：ServiceName 当前绑定的 `ServiceRef`。

它不保存 ServiceGroup，不选择路由，也不做 Gossip。

## 创建 DiscoveryService

```go
service, err := discovery.NewService(discovery.Config{
    LeaseTTL:      time.Minute,
    SweepInterval: 5 * time.Second,
})

ref, err := runtime.CreateService(gsr.ServiceSpec{
    Name:    discovery.DefaultServiceName,
    Service: service,
})

client, err := discovery.NewClient(runtime, ref)
```

Discovery 本身是 Service，因此注册、续租、解析和清理都经同一个 Mailbox。

## 节点租约

```go
lease, err := client.RegisterNode(ctx, "node-b", "127.0.0.1:9002")
err = client.Heartbeat(ctx, lease)
```

Generation 用于拒绝旧 owner 的迟到心跳。节点重注册后，旧 lease 不能续租新记录。

如果 Heartbeat 停止，Sweep Command 会清理过期节点及其名字。

## 注册长期名字

```go
err := client.RegisterName(
    ctx,
    lease,
    ".config",
    configRef,
)
```

Discovery 校验：

- lease 属于当前节点代际；
- Ref 的 Node 与 lease 一致；
- 名字合法；
- 绑定没有被其他有效 owner 占用。

解析：

```go
configRef, err := client.ResolveName(ctx, ".config")
```

## 启动顺序

一个常见 bootstrap：

```text
1. 通过静态配置连到 Discovery 节点
2. ResolveRemote(node, ".discovery")
3. 创建 discovery.Client
4. ResolveName(".config")
5. 直接向返回的 Ref Send/Call
```

静态配置没有消失，它缩小为“如何找到第一个权威入口”。

## 为什么不做 Gossip

第一版选择单一权威 Service，因为 lease、代际和名字冲突已经足够复杂。Gossip 还会引入：

- 冲突合并；
- 传播延迟；
- 删除墓碑；
- 多副本一致性；
- 网络分区。

在没有业务需求和验收模型时加入这些能力，只会让“查名字”变成分布式数据库项目。

## Codec

跨节点调用 Discovery 必须组合：

```go
codec := discovery.NewCodec(fallback)
```

Codec 显式编码 request/response，不依赖 Go `gob` 自动传递任意类型。

## 可运行例子

```bash
go run ./examples/discovery-runtime
```

例子创建 node-a 和 node-b，注册 `.config`，再从 node-a 解析远端 Ref。

## 当前边界

Discovery 不负责：

- 一组副本；
- Hash/RoundRobin；
- desired state；
- 自动迁移；
- 健康检查后的自动重建。

这些分别属于 ServiceGroup、Control Plane 和恢复流程。

## 对照源码

- `tooling/discovery/service.go`
- `tooling/discovery/client.go`
- `tooling/discovery/codec.go`
- `tooling/discovery/remote_test.go`

## 本章小结

`ResolveRemote` 回答“已知节点上的名字”，Discovery 回答“长期名字当前在哪个有效节点”。不要因为都叫 Resolve，就把它们合成一个万能目录。
