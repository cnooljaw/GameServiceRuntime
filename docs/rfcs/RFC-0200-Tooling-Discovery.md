# RFC-0200：DiscoveryService

> 状态：已接受
> 范围：Runtime Tooling
> 依据：`docs/learn/006-Go-Service-Runtime概要设计与约定.md`

## 目的

本文定义 GSR 的活动节点发现和长期 `ServiceName` 解析。

Discovery 是 Runtime Tooling，不进入 Core Runtime。Core 仍然只理解 `NodeID`、`ServiceRef`、`Command` 和本地 `Runtime.Resolve`。

## 第一版范围

Phase 7B 只实现两类事实：

```text
Node Discovery:
  当前哪些节点租约有效，节点地址是什么。

Service Name Discovery:
  长期 ServiceName 当前解析到哪个 ServiceRef。
```

第一版不实现：

- Desired State、Observed State、NodeAgent 或管理命令；见 [RFC-0250](RFC-0250-Tooling-Cluster-Control-Plane.md)。
- ServiceGroup、版本化 ServiceSet 和路由策略；见 [RFC-0260](RFC-0260-Tooling-ServiceGroup-Routing.md)。
- Gossip、复制、选主、持久化或外部注册中心。
- 根据节点地址动态修改 `ClusterTransport` peer。

Discovery 不是通用微服务注册中心。

## 分层

```text
Business / Tooling caller
  -> discovery.Client
  -> Command + Call
  -> DiscoveryService
  -> private node/name maps
```

`DiscoveryService` 是普通系统 Service：

- 状态修改只通过 Command 进入 `Handle`。
- Service 不创建 goroutine。
- 租约清理由 Runtime Timer 投递内部 Command。
- `Stop`、`Close` 只清理 Service 自有资源。
- 调用方不能读取内部 map 或持有 Service 指针。

## 启动根

第一版采用单一权威 `DiscoveryService`。

远程调用方必须从部署配置获得：

- Discovery 所在 `NodeID` 和 TCP 地址。
- Discovery 的 `ServiceRef`。

Discovery 不能反过来发现自己。`DefaultServiceName = ".discovery"` 只用于所在 Runtime 的本地 `Resolve`，不能替代远程启动配置。

Discovery Command 不提供节点身份认证或授权，只允许运行在 `RFC-0191` 定义的可信集群网络。管理面认证留到 Phase 8。

## 节点租约

```go
type NodeLease struct {
    Node       NodeID
    Generation uint64
    ExpiresAt  time.Time
}

type NodeRecord struct {
    ID         NodeID
    Address    string
    Generation uint64
    LastSeen   time.Time
    ExpiresAt  time.Time
}
```

租约规则：

1. `RegisterNode` 为该 `NodeID` 创建非零新 Generation。
2. 同一 `NodeID` 再注册时，旧 Generation 立即失效，其拥有的长期名字一并删除。
3. `Heartbeat` 只有在 NodeID 和 Generation 都匹配时才能续期。
4. `NodeLease` 的身份由 NodeID 和 Generation 决定；`ExpiresAt` 只用于调用方观测。
5. `NodeLease` 用于阻止旧进程覆盖新注册，不是认证或安全令牌。
6. 查询只返回未过期节点。
7. `ListNodes` 按 `NodeID` 排序并返回独立副本。
8. 节点注销、过期或被新 Generation 替换时，删除它拥有的全部名字。

Phase 7B 由部署编排代码调用 `RegisterNode` 和 `Heartbeat`。自动续租的 NodeAgent 留到 Phase 8；不能用 Service 裸 goroutine 维持租约。

Discovery 保存节点地址事实，但不据此修改 TCP peer。动态 peer reload 由后续 Control Plane 和 Transport adapter 负责。

## 长期 ServiceName

长期名字适合：

```text
.db
.match
.config
.discovery
```

Battle、单局 Room 等临时 Service 不进入 Discovery，仍然直接传递 `ServiceRef`。

名字规则：

1. 注册必须携带当前有效 `NodeLease`。
2. `ServiceRef.Node` 必须等于租约节点。
3. 同一租约可以把名字更新到同节点的新 `ServiceRef`。
4. 其它活动租约注册同名 Service 返回 `ErrNameConflict`。
5. 注销必须同时匹配租约、名称和当前 `ServiceRef`，避免迟到注销删除新实例。
6. 解析只返回仍由有效租约拥有的 `ServiceRef`。
7. `Runtime.Resolve` 继续只解析本地 `ServiceSpec.Name`；全局解析使用 `discovery.Client.ResolveName`。

## 公开 API

```go
const DefaultServiceName ServiceName = ".discovery"

type Config struct {
    LeaseTTL      time.Duration
    SweepInterval time.Duration
}

type CommandCaller interface {
    Call(context.Context, ServiceRef, CommandID, any) (any, error)
}

func NewService(Config) (Service, error)
func NewClient(CommandCaller, ServiceRef) (*Client, error)
func NewCodec(fallback ClusterCodec) ClusterCodec
```

`LeaseTTL=0` 默认使用 30 秒，`SweepInterval=0` 默认使用 5 秒；负值返回 `ErrInvalidConfig`。

Client：

```go
func (c *Client) RegisterNode(context.Context, NodeID, string) (NodeLease, error)
func (c *Client) Heartbeat(context.Context, NodeLease) (NodeLease, error)
func (c *Client) UnregisterNode(context.Context, NodeLease) error
func (c *Client) GetNode(context.Context, NodeID) (NodeRecord, error)
func (c *Client) ListNodes(context.Context) ([]NodeRecord, error)
func (c *Client) RegisterName(context.Context, NodeLease, ServiceName, ServiceRef) error
func (c *Client) UnregisterName(context.Context, NodeLease, ServiceName, ServiceRef) error
func (c *Client) ResolveName(context.Context, ServiceName) (ServiceRef, error)
```

稳定错误：

```text
ErrInvalidConfig
ErrInvalidNode
ErrNodeNotFound
ErrLeaseExpired
ErrInvalidName
ErrNameNotFound
ErrNameConflict
ErrInvalidResponse
ErrUnsupportedCommand
```

Discovery 领域错误通过类型化 Reply 返回，由 `Client` 还原为包内稳定错误。Core Runtime 不得导入或注册 Tooling 错误。

## CommandID

Discovery CommandID 固定为：

```text
0x02000101 RegisterNode
0x02000102 Heartbeat
0x02000103 UnregisterNode
0x02000104 GetNode
0x02000105 ListNodes
0x02000106 RegisterName
0x02000107 UnregisterName
0x02000108 ResolveName
0x020001ff SweepExpired
```

`SweepExpired` 只供 Discovery Service 自身的 Timer 使用，不经过远程 Codec。

调用方不直接拼装 Command，而是使用 `Client`。固定编号用于 Cluster Codec 和诊断，不新增动态 Command Registry。

## Codec

`NewCodec(fallback)` 使用标准库 `encoding/json` 编解码 Discovery 请求和 Reply：

- Discovery Command 由本 Codec 处理。
- 其它 Command 委托给 fallback。
- 无 fallback 时遇到其它 Command 返回 `ErrUnsupportedCommand`。
- 内部 `SweepExpired` 不允许远程编解码。
- 线格式字段使用固定的 `snake_case` 名称，不直接依赖 Go 结构体字段名。
- 解码忽略未知字段，以允许只增加字段的滚动升级；格式错误或尾随第二个 JSON 值仍返回 `ErrInvalidResponse`。
- Codec 只处理 payload，不接触 TCP、连接或 `WireEnvelope`。

## 过期清理

Discovery 在每个公开 Command 处理前根据 `ServiceContext.Now()` 清理过期租约，查询正确性不依赖 Timer 是否准时运行。

第一个节点注册后，Service 使用 `ServiceContext.After` 为自身安排 `SweepExpired`。Sweep 完成后：

- 仍有节点时重新安排 Timer。
- 没有节点时停止重排。

`ServiceContext` 不为此增加 `Cancel`。最后节点注销后，已经排定的 Sweep 可以到期执行一次；Service 关闭时 Runtime 负责取消绑定到目标的 Timer。

后台 Sweep 是资源及时回收机制，不是查询正确性的唯一保障。若 Timer 因 Mailbox 满等原因投递失败，下一次公开 Command 仍会先同步删除过期租约和名字，因此不会返回过期事实。

## 与后续阶段的关系

Desired State、Observed State 和“配置存在但未连接”属于 Cluster Control Plane，不由 Phase 7B 表达。

ServiceGroup 保存多个同职责 Service 的版本化集合，属于独立扩展。Discovery 第一版只保存单个长期名字到单个 `ServiceRef` 的绑定，不决定 Hash、RoundRobin、Broadcast 或其它路由策略。

## 规则

1. Discovery 不进入 Core Runtime。
2. Discovery 不负责业务状态或 Battle 实例分配。
3. Discovery 第一版只保存活动节点和长期 ServiceName。
4. Discovery 不执行远程管理命令。
5. Discovery 不修改 `ClusterTransport` peer。
6. Discovery 不内建负载均衡、取模或广播。
7. 所有状态修改必须通过 Command 串行处理。
8. Timer 只能投递 `SweepExpired` Command。
9. 返回的节点列表必须排序并与内部状态隔离。
10. 节点失效时必须清理其拥有的所有名字。

## 验收

必须覆盖：

- 节点注册、续租、过期、注销和同 NodeID 新 Generation 替换。
- 节点列表稳定排序和副本语义。
- 长期名字注册、同租约替换、冲突、精确注销和随租约清理。
- 本地与双节点 TCP 调用。
- Discovery Codec 的类型保持、fallback 和内部 Command 拒绝。
- 领域错误经过远程调用后仍可用 `errors.Is` 判断。
- Service 无 goroutine，Sweep 只通过 Timer Command 运行。
- 全量测试、`go vet` 和 Race Detector。
