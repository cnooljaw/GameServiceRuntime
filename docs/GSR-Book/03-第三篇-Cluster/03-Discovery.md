# Discovery

> 状态：已实现

## 为什么需要 Discovery

`ServiceRef` 是具体运行实例地址。节点或长期 Service 重启后，旧地址会失效，但这不意味着所有 Cluster 都需要 Discovery。

调用方已知服务所在节点时，直接使用静态节点配置和节点内名字解析：

```go
configRef, err := runtime.ResolveRemote(ctx, "node-b", ".config")
```

这就是默认 Cluster 模式，也对应 Skynet `cluster.config + cluster.query` 的思路。

只有调用方不应知道服务所在节点，或者长期 Service 需要动态迁移、节点目录和控制面时，才启用 Discovery。

GSR 的最小 Discovery 分开处理两类事实：

```text
Node Discovery:
  当前哪些节点租约有效，地址是什么。

ServiceName Discovery:
  长期名字当前映射到哪个 ServiceRef。
```

它不处理 ServiceGroup、负载均衡或管理状态，也不是普通 Send、Call 的前置组件。

## 所在层次

Discovery 位于 `tooling/discovery`，依赖 Core Runtime。Core 不反向理解 Discovery。

```text
discovery.Client
  -> Runtime.Call
  -> Command
  -> DiscoveryService
  -> private node/name maps
```

调用方只使用类型化 `Client`。CommandID、请求和 Reply 类型由 Discovery 包隐藏。

## 启用后的启动根

选择启用 Discovery 时，第一版只有一个权威 `DiscoveryService`。远程节点必须从部署配置获得：

- Discovery 节点的 `NodeID` 和 TCP 地址。
- Discovery 的稳定本地名字 `.discovery`。

建立到目标节点的 Transport 连接后，通过 Core 节点级查询获取当前动态地址：

```go
discoveryRef, err := runtime.ResolveRemote(ctx, "node-b", discovery.DefaultServiceName)
```

`ResolveRemote` 只查询指定节点的本地 Registry，对应 Skynet `cluster.query(node, name)` 的启动职责。它不是全局 Discovery，也不依赖业务 `ClusterCodec`。

基础 Cluster 不执行这一步。只有需要全局名字或节点目录的调用方才创建 `discovery.Client`。

## 节点租约

节点注册后获得：

```go
type NodeLease struct {
    Node           NodeID
    AuthorityEpoch uint64
    Generation     uint64
    ExpiresAt      time.Time
}
```

NodeID、AuthorityEpoch 和 Generation 共同确定租约身份。Heartbeat 只能续期当前身份。Discovery authority 每次创建都会生成新的非零 AuthorityEpoch，因此 authority 重启后，即使 Generation 又从 1 开始，旧租约也不会重新有效。

同一 NodeID 重新注册会生成新 Generation。旧进程即使迟到发送 Heartbeat，也不能延长新进程的租约。旧 Generation 拥有的长期名字同时删除。

Discovery 私下记录租约的精确 `CommandContext.Source()`：

- 注册请求的 `Source.Node` 必须等于登记的 NodeID。
- Heartbeat、注销和名字写操作必须来自原始 owner。
- 同一可信节点可以重新注册并建立新的租约 owner；旧租约随即失效。

`ExpiresAt` 只用于观测，不参与调用方构造身份。GSR 信任集群节点，但不信任错误的程序状态。Source、owner 和租约都不是认证凭据；当前 Discovery 只能部署在可信集群网络。

## 长期名字

长期 Service 创建后，由持有当前节点租约的编排代码注册：

```go
err := client.RegisterName(ctx, lease, ".config", configRef)
```

规则：

- `ServiceRef.Node` 必须等于租约节点。
- 同一租约可以将名字替换到新 ServiceID。
- 其它活动租约不能抢占名字。
- 注销必须匹配租约、名字和当前 ServiceRef。
- 节点过期、注销或重注册时自动删除其名字。

Battle、单局 Room 等临时 Service 直接传递 `ServiceRef`，不注册长期名字。

## 创建与调用

```go
service, err := discovery.NewService(discovery.Config{
    LeaseTTL:      time.Minute,
    SweepInterval: 10 * time.Second,
})
if err != nil {
    return err
}

discoveryRef, err := runtime.CreateService(gsr.ServiceSpec{
    Name:    discovery.DefaultServiceName,
    Service: service,
})
if err != nil {
    return err
}

client, err := discovery.NewClient(runtime, discoveryRef)
if err != nil {
    return err
}

lease, err := client.RegisterNode(ctx, "node-a", "127.0.0.1:9001")
if err != nil {
    return err
}

resolved, err := client.ResolveName(ctx, ".config")
```

`LeaseTTL` 零值默认 30 秒，`SweepInterval` 零值默认 5 秒。

## 跨节点 Codec

Discovery 使用可组合 Codec：

```go
codec := discovery.NewCodec(applicationCodec)
```

Discovery Command 使用类型明确的 JSON payload，线格式字段固定为 `snake_case`，不直接依赖 Go 结构体字段名。Decoder 忽略未知字段以兼容只增加字段的滚动升级，但拒绝格式错误和尾随第二个 JSON 值。

其它 Command 交给 `applicationCodec`。内部 `SweepExpired` 不能经过远程 Codec。

领域错误放在类型化 Reply 中，因此远程名字冲突仍可判断：

```go
errors.Is(err, discovery.ErrNameConflict)
```

Core Runtime 不需要注册 Tooling 错误。

只有明确的 Discovery 领域错误会放入类型化 Reply。Timer、Runtime 和生命周期错误直接从 Handler 返回，保持 Core 错误语义。Client 在领域错误为空后还会校验 Lease、NodeRecord、ServiceRef 和排序后的节点列表，类型正确但内容无效的响应返回 `ErrInvalidResponse`。

## 过期清理

第一个节点注册后，Discovery 使用 Runtime Timer 向自己投递 `SweepExpired` Command。Service 不创建 goroutine。

每个公开 Command 在处理前也会同步清理过期租约。即使后台 Timer 投递失败，查询也不会返回过期节点或名字。

Service 停止时，Runtime 会取消绑定到该 ServiceRef 的 Timer。

## 当前限制

- 单一内存权威，没有复制、选主或持久化。
- 部署编排仍可直接 Heartbeat；Phase 8 的 `NodeAgentService` 也会自动注册并续租自己的节点 lease。
- 节点地址不会自动修改 TCP peer。
- 不表达 Desired State、Observed State 或“配置存在但未连接”。
- 不提供 ServiceGroup、Hash、RoundRobin 或 Broadcast。
- 不提供身份认证和授权，Cluster 端口只能位于可信内网。

完整契约见 `RFC-0200`。可运行示例：

```bash
go run ./examples/discovery-runtime
```
