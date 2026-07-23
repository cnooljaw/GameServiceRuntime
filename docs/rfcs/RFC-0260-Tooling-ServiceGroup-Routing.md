# RFC-0260：ServiceGroup 与路由策略

> 状态：已接受
> 接受日期：2026-07-23
> 目标阶段：Phase 9
> 范围：Runtime Tooling
> 依赖：[RFC-0130](RFC-0130-Core-Send-Call-Reply.md)、[RFC-0190](RFC-0190-Core-Cluster-Data-Plane.md)、[RFC-0200](RFC-0200-Tooling-Discovery.md)
> 依据：Skynet 的名字定位与消息投递分层、`skynet_fly` 的服务组和版本化地址表

## 目的

本文冻结 GSR 的 ServiceGroup、版本化 ServiceSet、订阅通知和第一批路由策略。

ServiceGroup 是 Runtime Tooling 能力，不进入 Core Runtime。Core 仍然只理解：

```text
ServiceRef
Command
Envelope
Send / Call
```

Phase 9 增加一个独立的 `DirectoryService` 保存 ServiceGroup 事实，并提供纯路由组件把调用方持有的 ServiceSet 映射为一个或多个 `ServiceRef`。它不引入新的 RPC、Transport 协议、Core 路由入口或业务领域名字。

## 目标

Phase 9 必须交付：

- 独立的 `tooling/servicegroup` 包和单一权威 `DirectoryService`。
- 带 authority epoch 和单组 revision 的 `ServiceSetVersion`。
- 使用 compare-and-set 发布的完整 ServiceSet。
- 使用 `ServiceRef + Command` 的 Watch lease。
- `Hash`、`RoundRobin` 和 `Broadcast` 路由策略。
- 对调用方已持有 ServiceSet 执行 Send/Call 的 `Router`。
- 可组合 Codec、本地测试和双节点 TCP 验收。

## 非目标

Phase 9 不实现：

- 把 ServiceGroup、ServiceSet 或 RoutingPolicy 加入 Core Runtime。
- 让 Discovery 保存 ServiceSet 或决定路由。
- Service Desired State、Controller、Reconcile、自动扩缩容、故障迁移或放置策略。
- Service 自动注册进组、健康检查、选主、复制、持久化或 Gossip。
- 一致性哈希、权重、最少连接、随机路由或基于负载的路由。
- 外部 Admin API、用户认证、动作级授权或审计。
- Directory 高可用、跨 Directory 复制或持久化恢复。
- 在 Client 内启动 goroutine、维护隐式 channel 或后台刷新缓存。

## 分层与依赖

```text
部署配置 / 后续 Controller
  -> servicegroup.Client.Publish
  -> DirectoryService
       owns GroupName -> ServiceSet

订阅 Service
  -> servicegroup.Client.Watch
  <- ServiceSetChanged Command
  -> 在自己的 Mailbox 内替换缓存

业务 / Tooling caller
  -> Router.Send / Router.Call
  -> RoutingPolicy.Pick(caller-owned ServiceSet)
  -> Runtime Send / Call
```

依赖方向固定为：

```text
Business / Tooling -> tooling/servicegroup -> runtime
tooling/discovery  -X-> tooling/servicegroup
runtime            -X-> tooling/servicegroup
```

Discovery 只可用于定位长期命名的 `DirectoryService`。禁止把 ServiceSet、取模、轮询或广播加入 `tooling/discovery`。

## 公开类型

包路径为 `tooling/servicegroup`。

```go
const DefaultDirectoryName gsr.ServiceName = ".service-directory"

type GroupName string

type ServiceSetVersion struct {
    AuthorityEpoch uint64
    Revision       uint64
}

type ServiceSet struct {
    Name    GroupName
    Version ServiceSetVersion
    Refs    []gsr.ServiceRef
    Tags    map[string]string
}
```

ServiceGroup 表示一组承担同一职责的 Service。`Tags` 是整组元数据，不是成员级标签，不参与第一版路由。

一个有效 ServiceSet 满足：

1. Name 非空且不允许首尾空白。
2. Version 的 AuthorityEpoch 和 Revision 均非零。
3. 每个 ServiceRef 的 Node 非空、ID 非零。
4. Refs 按 `NodeID`、`ServiceID` 升序排列且不重复。
5. Tags 的 key 非空且不允许首尾空白。
6. 查询、发布结果、Watch 结果和通知互不共享 Refs 或 Tags 的可变底层存储。

空 Refs 是有效事实，表示“该组当前存在，但没有可路由成员”。第一版不提供删除 Group 的命令；需要暂时切断新流量时发布空集合，从而保留连续版本历史并避免删除后重建的歧义。

## 版本身份

`uint64 Revision` 不能单独标识 ServiceSet。Directory 重启后如果 revision 从 1 重新开始，持有旧 revision 的客户端可能把新状态误判为过期状态。

因此版本由两部分组成：

```text
ServiceSetVersion = AuthorityEpoch + Revision
```

- Directory 每次构造时生成非零随机 AuthorityEpoch。
- 同一 AuthorityEpoch 内，每个 Group 的 Revision 从 1 开始单调递增。
- 同 epoch 的版本可以按 Revision 判断先后。
- AuthorityEpoch 变化表示权威实例已经变化，客户端必须用新权威的完整快照替换旧缓存，不能比较两个 epoch 的 Revision 大小。
- 零值版本只表示“预期该 Group 尚不存在”，不是可发布版本。

Revision 回绕必须返回稳定错误，不能覆盖当前版本。所谓回滚，是把旧 Refs 作为一个更高 Revision 的新 ServiceSet 再次发布，不允许倒退版本号。

## DirectoryService

```go
type DirectoryConfig struct {
    PublisherNode gsr.NodeID
    WatchTTL      time.Duration
    SweepInterval time.Duration
}

func NewDirectoryService(DirectoryConfig) (gsr.Service, error)
```

`DirectoryService` 是 ServiceGroup 事实的唯一状态 owner。它只在 Mailbox Handler 中修改 Group、revision 和 Watch lease，不创建 goroutine。

`PublisherNode` 必须非空。`Publish` 只接受 `CommandContext.Source().Node == PublisherNode` 的可信集群内请求。该检查只用于阻止配置错误和非发布节点误写，不是用户身份认证；Phase 9 不暴露外部修改型 Admin API。

`WatchTTL=0` 和 `SweepInterval=0` 使用实现定义的稳定默认值，负值无效。订阅过期由 Runtime Timer 投递私有 sweep Command；Timer 不执行清理回调。

## Directory Client

```go
type CommandCaller interface {
    Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error)
}

type Client struct { /* private */ }

func NewClient(CommandCaller, gsr.ServiceRef) (*Client, error)

func (*Client) Publish(
    context.Context,
    GroupName,
    ServiceSetVersion,
    []gsr.ServiceRef,
    map[string]string,
) (ServiceSet, error)

func (*Client) Get(context.Context, GroupName) (ServiceSet, error)
func (*Client) Watch(context.Context, GroupName, gsr.ServiceRef) (WatchResult, error)
func (*Client) RenewWatch(context.Context, WatchLease) (WatchLease, error)
func (*Client) Unwatch(context.Context, WatchLease) error
```

Client 只提供类型化 Call，不暴露 DirectoryService 指针、内部 map 或私有 Command payload。

### Publish

Publish 使用 compare-and-set：

- Group 尚不存在时，ExpectedVersion 必须为零值；Directory 分配当前 epoch、revision 1。
- Group 已存在时，ExpectedVersion 必须与当前版本完全相等；Directory 分配当前 revision 加 1。
- 预期版本不匹配返回 `ErrVersionConflict`，不修改状态、不发送通知。
- 即使 Refs 和 Tags 与当前值相同，成功 Publish 仍产生新 Revision。
- Directory 负责标准化 Refs 的顺序和去重；调用方不能指定新版本号。

Publish 成功表示新 ServiceSet 已经成为权威事实。后续 Watch 通知投递失败不能回滚 Publish，也不能把已经成功的发布伪装成失败。

### Get

Get 返回当前完整独立副本。尚未发布的 Group 返回 `ErrGroupNotFound`；已发布的空组正常返回。

## Watch lease

```go
const ServiceSetChangedCommand gsr.CommandID = 0x02600201

type WatchLease struct {
    Group          GroupName
    Subscriber     gsr.ServiceRef
    AuthorityEpoch uint64
    Generation     uint64
    ExpiresAt      time.Time
}

type WatchResult struct {
    Lease   WatchLease
    Current ServiceSet
    Found   bool
}

type ServiceSetChanged struct {
    Set ServiceSet
}
```

Watch 不返回 channel。订阅 Service 必须声明并处理 `ServiceSetChangedCommand`，并在自己的 Mailbox 内保存最新完整 ServiceSet。

Watch 规则：

1. `CommandContext.Source()` 必须与 Subscriber 完全相等，禁止替其它 Service 注册回调。
2. Watch 允许在 Group 尚不存在时注册；此时 `Found=false`。
3. Watch 响应携带同一 Mailbox 时点的当前完整快照；存在时 `Found=true`。
4. 同一 Group、Subscriber 再次 Watch 会分配新 Generation 并替换旧 lease。
5. Renew 只延长完全匹配且未过期的当前 lease，不改变 Generation。
6. Unwatch 只删除完全匹配的当前 lease；迟到的 Renew 或 Unwatch 不能影响新 Generation。
7. Directory authority 变化后，旧 lease 一律失效。
8. 每次成功 Publish 至多向每个当前 lease Send 一次完整 `ServiceSetChanged`；通知按 Subscriber Service 的 Mailbox 顺序处理。
9. 通知是 best-effort 状态提示，不是可靠事件日志。发送失败只记录指标，lease 保留到过期；订阅方必须能通过 Get 重新读取完整事实。
10. Client 发现通知的 AuthorityEpoch 变化时直接替换缓存；同 epoch 出现 revision 跳跃时也可直接使用完整快照，或调用 Get 复核。

## 路由策略

```go
type RoutingKey string

type RoutingPolicy interface {
    Pick(ServiceSet, RoutingKey) ([]gsr.ServiceRef, error)
}
```

第一批策略为：

### Hash

- RoutingKey 必须非空。
- 使用标准 FNV-1a 64 位哈希处理 RoutingKey 的原始 UTF-8 字节。
- 目标索引为 `hash % len(Refs)`。
- Refs 使用 ServiceSet 的稳定排序。
- 第一版不是一致性哈希；成员数量或排序变化可能重新映射大量 key，调用方不得假设最小迁移。

### RoundRobin

- 每个 `RoundRobin` 实例持有一个并发安全计数器。
- 第一次选择 Refs[0]，之后按当前 Refs 长度轮询。
- 计数器的状态范围是该 policy 实例；需要彼此独立轮询的调用方应使用不同实例。
- 成员变化时只对当前集合长度取模，不保存旧 ServiceRef。

### Broadcast

- 返回 ServiceSet 中全部 Refs，保持稳定顺序。
- Router.Send 逐个投递且不会创建 goroutine。
- 某个目标失败不阻止尝试后续目标。
- 部分失败返回带目标明细的 `BroadcastError`；成功目标不会被回滚或重试。

`Direct` 不属于 ServiceGroup 路由策略。已经持有单个 `ServiceRef` 时直接使用 Runtime Send/Call；再包装一个 Direct policy 只会制造没有信息增益的抽象。

## Router

```go
type CommandDispatcher interface {
    Send(gsr.ServiceRef, gsr.CommandID, any) error
    Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error)
}

type Router struct { /* private */ }

func NewRouter(CommandDispatcher) (*Router, error)

func (*Router) Send(
    ServiceSet,
    RoutingPolicy,
    RoutingKey,
    gsr.CommandID,
    any,
) error

func (*Router) Call(
    context.Context,
    ServiceSet,
    RoutingPolicy,
    RoutingKey,
    gsr.CommandID,
    any,
) (any, error)
```

Router 接受调用方已经持有的 ServiceSet，不按 GroupName 隐式查询 Directory，也不维护后台缓存。这一边界避免一个看似异步的 Send 暗含同步远程 Call，并允许订阅 Service 在自己的 Mailbox 中控制缓存切换时点。

Router 必须验证 policy 结果非空、目标不重复且都属于输入 ServiceSet。Send 对单目标保留 Runtime 原错误；多目标按 Broadcast 语义顺序尝试并聚合失败。Call 只允许恰好一个目标，多目标稳定返回 `ErrMultipleTargets`，不隐式选择第一个 Reply。

## Command 与 Codec

CommandID 固定为：

```text
0x02600101 PublishServiceSet
0x02600102 GetServiceSet
0x02600103 WatchServiceGroup
0x02600104 RenewServiceGroupWatch
0x02600105 UnwatchServiceGroup
0x026001fe SweepExpiredWatches       DirectoryService 私有
0x02600201 ServiceSetChanged         订阅 Service 公共通知
```

`NewCodec(fallback)` 使用标准库 JSON 编解码公开请求、响应和通知，并将其它 Command 委托 fallback。私有 sweep Command 不可远程编解码。

Codec 必须拒绝类型不匹配、畸形 JSON、尾随 JSON 值、未知错误码和违反 ServiceSet、WatchLease 不变量的成功响应。Codec 只处理 payload，不处理连接、认证、WireEnvelope 或路由。

## 错误与失败语义

稳定领域错误至少包括：

```text
ErrInvalidConfig
ErrInvalidCaller
ErrInvalidGroup
ErrInvalidServiceSet
ErrGroupNotFound
ErrVersionConflict
ErrVersionExhausted
ErrUnauthorized
ErrInvalidWatch
ErrWatchExpired
ErrWatchOwnerMismatch
ErrInvalidResponse
ErrUnsupportedCommand
ErrInvalidRoutingKey
ErrNoRoute
ErrInvalidRoutingResult
ErrMultipleTargets
```

Directory 的领域错误通过稳定响应码返回。Transport、Runtime、context 取消或超时错误保留原语义，不伪装成领域错误。

Broadcast 多目标投递可能部分成功。`BroadcastError` 必须按目标稳定排序保存失败项，并支持调用方检查底层错误；错误文本不能声称投递已经回滚。

## 生命周期、并发与可观测性

- Directory 的 Group、revision、watcher 和 generation 只在 Handler 中修改。
- Directory、Client、Router 和 policy 都不创建 goroutine。
- RoundRobin 只用并发安全计数器保护自己的选择状态。
- Watch sweep 只由 Runtime Timer Command 驱动。
- Stop 清空 Group 和 watcher；Close 清空 ServiceContext。关闭阶段不发送取消或通知。
- Directory 至少记录 publish 成功/冲突、Watch 当前数量和通知成功/失败指标。
- Router 不修改 ServiceSet，不重试 Runtime Send/Call，也不吞掉底层错误。

## 与后续阶段的关系

Phase 9 只提供事实目录、订阅和路由能力。它不决定应该有多少 Service、Service 应放在哪个节点，也不自动把 Runtime 中的 Service 注册进组。

Phase 10 的 Controller 可以成为 PublisherNode 上的发布者：

```text
Desired State + Observed State
  -> Controller
  -> Create / Drain / Stop
  -> Publish higher ServiceSetVersion
```

回滚必须发布更高 Revision 的旧成员集合。Directory 的 NodeID 来源检查不能替代 Phase 10 修改型控制命令所需的 principal、动作级授权、RequestID 和审计。

## 验收

必须覆盖：

1. Directory 为新组分配 revision 1，并以 compare-and-set 拒绝陈旧发布。
2. Directory 重建后 AuthorityEpoch 改变；旧版本不能修改新权威。
3. Refs 去重、稳定排序，所有 Refs 和 Tags 返回独立副本。
4. 未发布组与已发布空组语义不同。
5. 未授权发布在修改状态和通知前被拒绝。
6. Watch 可先于 Group 创建；重订阅、续订、取消、过期和旧 generation fencing 正确。
7. Publish 通知完整快照；通知失败不回滚事实。
8. Hash 的算法和扩缩容行为固定，RoundRobin 并发安全，Broadcast 顺序与部分失败可检查。
9. Router 不隐式查询 Directory；Call 拒绝多目标，策略不能返回组外或重复目标。
10. 本地与双节点 TCP 场景通过可组合 Codec 完成 Publish、Get、Watch 通知和路由。
11. Service 不创建 goroutine；无第二套 RPC；Core 与 Discovery 不导入 `tooling/servicegroup`。
12. `go test ./...`、`go vet ./...`、`go test -race ./...` 通过。
