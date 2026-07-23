# RFC-0250：Cluster Control Plane

> 状态：待实现
> 目标阶段：Phase 8
> 范围：Runtime Tooling、Cluster Control Plane
> 依赖：[RFC-0100](RFC-0100-Core-Service.md)、[RFC-0130](RFC-0130-Core-Send-Call-Reply.md)、[RFC-0180](RFC-0180-Core-Lifecycle.md)、[RFC-0190](RFC-0190-Core-Cluster-Data-Plane.md)、[RFC-0191](RFC-0191-Core-Cluster-Transport.md)、[RFC-0200](RFC-0200-Tooling-Discovery.md)、[RFC-0230](RFC-0230-Tooling-Monitor.md)
> 依据：Skynet debug_console、hanxi/skynet-admin 的观测分层

## 目的

本文冻结 Phase 8 的最小节点观测面：每个节点自动维护自己的 Discovery 租约，并由可信集群内的只读观察者读取节点事实。

它不引入新的 RPC、Transport 协议或 Core getter。所有跨节点动作继续通过 `ServiceRef`、`Command`、`Call` 和现有 Cluster Data Plane 完成。

## 目标

Phase 8 必须交付：

- 每个节点一个 `NodeAgentService`，只消费本地 `monitor.Monitor` 的独立 `Report`。
- NodeAgent 在进入 `Running` 后自动注册自己的 Discovery lease，并持续 Heartbeat。
- 一个 `ClusterObserverService`，保存静态 `NodeConfig`、刷新单节点观测并返回稳定的详情副本。
- 通过现有远程 Call 查询 NodeAgent 的 Client、Codec 和双节点 TCP 验收。

## 非目标

Phase 8 不实现：

- HTTP、CLI、Web Console、外部 Admin API、用户认证、授权 token 或人类操作者身份。
- Service Desired State、Controller、Reconcile、扩缩容、故障迁移、reload、启停节点、Drain、ServiceGroup 切换、回滚、远程代码执行或直接 `Runtime.Stop`。
- 动态 TCP peer 更新、配置持久化、选主、跨 Observer 同步或后台批量轮询。
- Transport 连接对象、进程内存、Service 指针、业务领域指标或业务状态。
- 通用审计日志接口。修改型命令尚未存在；后续引入高危命令前必须冻结认证、授权、二次确认和审计格式。

## 分层与依赖

```text
部署组合根
  -> DiscoveryService
  -> NodeAgentService --(自动 Register / Heartbeat)--> DiscoveryService
  -> ClusterObserverService --(只读 Call)--> NodeAgentService
  -> monitor.Monitor
  -> Runtime.Inspect()
```

依赖方向固定为：

```text
tooling/control -> tooling/discovery
tooling/control -> tooling/monitor -> runtime
runtime -X-> tooling/control
```

`NodeAgentService` 不读取 Runtime 私有字段，只调用注入的 `Reporter.Capture`。它只保存自身的 lease，不保存其它节点状态。`ClusterObserverService` 不持有 Agent 指针，只保存 `ServiceRef` 并以 `ServiceContext.Call` 查询。组合根负责提供 Discovery 和 Agent 的已知 `ServiceRef`；Handler 不调用 `Runtime.ResolveRemote`、不做配置 I/O，也不启动 goroutine。

## 可信集群边界

Phase 8 只运行在 RFC-0191 定义的可信集群网络内。Transport 握手 `NodeID`、Discovery lease owner 和 Agent 的来源检查都是错误状态 fencing，不是用户认证。

每个 NodeAgent 配置一个 `ObserverNode`。它仅接受 `CommandContext.Source` 同时满足下列条件的报告请求：

1. `Source.Node == ObserverNode`。
2. `Source.ID != 0`。

因此外部 `Runtime.Call` 的节点级 caller 不能直接读取 Agent；请求必须由 Observer Service 发起。可信 Observer 节点上的其它错误 Service 仍可能伪装为普通 Service，这符合当前 Cluster 信任模型；未来修改型命令必须添加更强的认证与授权 seam。

## 公开契约

包路径为 `tooling/control`。

```go
const DefaultNodeAgentName gsr.ServiceName = ".node-agent"
const DefaultObserverName gsr.ServiceName = ".cluster-observer"

type Reporter interface {
    Capture() monitor.Report
}

type NodeAgentConfig struct {
    Reporter          Reporter
    ObserverNode      gsr.NodeID
    Discovery         gsr.ServiceRef
    Address           string
    HeartbeatInterval time.Duration
    CallTimeout       time.Duration
}

type NodeConfig struct {
    ID      gsr.NodeID
    Address string
    Role    string
    Enabled bool
}

type NodeTarget struct {
    Config NodeConfig
    Agent  gsr.ServiceRef
}

type NodeStatus string

const (
    NodeUnknown     NodeStatus = "unknown"
    NodeHealthy     NodeStatus = "healthy"
    NodeUnavailable NodeStatus = "unavailable"
    NodeDisabled    NodeStatus = "disabled"
)

type NodeObservedState struct {
    ID         gsr.NodeID
    Status     NodeStatus
    CapturedAt time.Time
    Latency    time.Duration
    LastError  string
}

type NodeDetail struct {
    Config    NodeConfig
    Observed  NodeObservedState
    Report    monitor.Report
    HasReport bool
}

type ObserverConfig struct {
    Nodes       []NodeTarget
    CallTimeout time.Duration
    Now         func() time.Time
}

type CommandCaller interface {
    Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error)
}

func NewNodeAgentService(NodeAgentConfig) (gsr.Service, error)
func NewClusterObserverService(ObserverConfig) (gsr.Service, error)
func NewClient(CommandCaller, gsr.ServiceRef) (*Client, error)
func NewCodec(fallback gsr.ClusterCodec) gsr.ClusterCodec
```

`DefaultNodeAgentName` 和 `DefaultObserverName` 只是节点内稳定名字，不能被视为跨节点动态 `ServiceID`。组合根在已知节点调用 `Runtime.ResolveRemote` 后，把 Agent `ServiceRef` 放入 `NodeTarget`。

`NodeConfig` 是用于定位和观测的静态部署事实，不是可收敛的 Desired State。`ObserverConfig.Nodes` 在创建时深复制；NodeID 必须唯一，ID 与 Address 必须非空，Role 可以为空。Enabled 节点必须提供 Node 相同、ID 非零的 Agent ref；Disabled 节点可以省略 Agent，且永不发起远程 Call。

`NodeAgentConfig` 的 `Reporter`、`ObserverNode`、Discovery ref 和 Address 必须有效；Discovery ref 的 Node 与 ID 必须非零。`HeartbeatInterval=0` 默认 10 秒，`CallTimeout=0` 默认 3 秒；负值无效。部署配置必须使 Discovery `LeaseTTL` 大于 HeartbeatInterval，并为暂时失败预留至少一次重试窗口。NodeID 不从配置传入，始终使用 `ServiceContext.Self().Node`。

## Command 与 Codec

CommandID 固定为：

```text
0x02500101 GetNodeReport        NodeAgentService
0x025001fe RegisterNodeLease    NodeAgentService（私有）
0x025001ff HeartbeatNodeLease   NodeAgentService（私有）
0x02500201 ListNodes            ClusterObserverService
0x02500202 GetNodeDetail        ClusterObserverService
0x02500203 RefreshNode          ClusterObserverService
```

`RegisterNodeLease` 是 NodeAgent 的 `StartupCommandDeclarer` 返回的启动 Command。`HeartbeatNodeLease` 仅由该 Service 的 Runtime Timer 投递。它们不经过 `NewCodec`，远端 payload 必须被 Codec 拒绝或交给 fallback，不能被外部调用方构造。

所有公开查询都是 Call；调用方使用 Client，不直接构造请求。`NewCodec` 用标准库 JSON 编解码公开请求与响应，并将其它 Command 委托 fallback；无 fallback 时返回 `ErrUnsupportedCommand`。它必须拒绝类型不匹配、畸形 JSON、尾随 JSON 值和内部结构不变量不成立的成功响应。

Codec 只处理 payload，不处理 TCP、连接、认证、WireEnvelope 或 Core bootstrap。它可包裹 Discovery Codec，或被 Discovery Codec 包裹；两者按 CommandID 分派，顺序不改变命令语义。

## 状态与生命周期

NodeAgent 在 `Init` 只保存 `ServiceContext` 和冻结配置。它通过 `StartupCommandDeclarer` 声明 `RegisterNodeLease`；Runtime 在它进入 `Running` 后把该 Command 投入自己的 Mailbox。Handler 使用 `discovery.NewClient(ServiceContext, Discovery)`，以 Self.Node 和 Address 注册，成功后保存新 lease，并通过 `ServiceContext.After` 投递一次私有 `HeartbeatNodeLease`。

Heartbeat 成功后替换 lease 并安排下一次 Heartbeat。`ErrLeaseExpired` 表示 authority 或 generation 已变化，当前 Handler 必须重新 Register 并替换 lease。注册失败、普通 Heartbeat 失败、超时或远端不可用不清除仍可能有效的 lease；它们只安排下一次同类 Command 重试。每个远端 Call 使用 `context.WithTimeout(context.Background(), CallTimeout)`。Service 不做并行重试、指数退避或后台 poll。

`Stop(ctx)` 只对当前 lease 最多执行一次有界 `UnregisterNode`；`ErrLeaseExpired` 视为已经清理。调用失败不阻止 Runtime 的生命周期清理，也不得尝试旧 generation 的第二次注销。`Close` 清空私有 Context、Discovery client 和 lease。Runtime 关闭时禁止跨 Service Call 的既有规则优先，因此关闭阶段可以跳过注销并依赖 TTL 过期。

每次 `GetNodeReport` 在 Agent 自己的 Mailbox Handler 中调用一次 `Reporter.Capture`，并 Reply 独立的 `monitor.Report`。报告切片、元素与 map 必须和 Reporter 的后续报告隔离。

ClusterObserverService 在自己的 Mailbox 内保存：创建时冻结的 `NodeTarget`、每个节点最近一次 `NodeObservedState`，以及最近一次成功报告的独立副本。Enabled 节点初始为 unknown，Disabled 节点初始为 disabled。`ListNodes` 按 NodeID 排序；`GetNodeDetail` 不做网络 I/O；`RefreshNode` 只刷新一个 Enabled 节点并在同一 Handler 写缓存。第一版不做批量刷新、后台 poll 或跨节点合并。

## 错误与失败语义

稳定错误：

```text
ErrInvalidConfig
ErrInvalidCaller
ErrInvalidNode
ErrNodeNotFound
ErrNodeDisabled
ErrUnauthorized
ErrInvalidResponse
ErrUnsupportedCommand
```

NodeAgent 对错误来源、错误报告请求 payload 回复领域错误，且不得先 Capture 再拒绝。Discovery 的 `ErrLeaseExpired` 只用于 Agent 内部重注册决策；其它 Discovery/Runtime 基础设施错误直接保留原错误语义，不伪造为 `ErrInvalidResponse`。

RefreshNode 的超时、断线、远端不可用、payload 解码失败或无效成功响应只更新目标节点为 unavailable，清空报告，并写入稳定 `LastError`；不得泄漏底层错误字符串、连接细节或业务 payload。迟到 Reply 由 Core PendingCall 丢弃，不能覆盖已完成刷新。

## 并发、所有权与可观测性

NodeAgent 和 ClusterObserver 都是普通 Service。所有可变状态只在 Handler 中修改，均不得创建 goroutine。Timer 只投递 Command；Runtime 负责在 Stop/Close 时取消目标 Timer。`monitor.Report` 与 `NodeDetail` 的切片、元素和 map 必须深复制。

ClusterObserver 在刷新后记录 `control_refresh_succeeded_total`、`control_refresh_failed_total`、`control_node_status` 和 `control_refresh_latency`。NodeAgent 不增加 Core 专用 getter；其租约与报告事实分别来自 Discovery 和 Monitor。

部署组合根可以记录 Agent 配置与外层请求日志，但不得记录完整 Monitor Report、Transport 内部对象或未脱敏业务字段。本阶段没有修改型命令，因此没有审计事件。

## 与后续阶段的关系

Discovery 仍只保存活动 lease 和长期名字。Phase 8 的 Heartbeat 只续租 NodeAgent 自己的节点租约，不以 Observed State 回写 Discovery，也不据此动态修改 TCP peer。

Phase 9 处理 ServiceGroup、Directory 和路由事实。Phase 10 才能冻结 Service Desired State、Controller、Reconcile、NodeAgent 执行动作、Drain 和回滚；这些命令必须另外定义认证 principal、动作级授权、RequestID、审计记录、部分成功和失败回滚，不能复用本阶段 NodeID 来源检查作为用户授权。

## 验收

必须覆盖：

1. Startup Command 只在 Service 进入 Running 后经 Mailbox 投递，未声明 Command 被拒绝。
2. NodeAgent 自动 Register、定时 Heartbeat；lease 失效后重新 Register；普通暂时失败只在下一次间隔重试。
3. Stop 对当前 lease 最多注销一次；Runtime Closing 路径不发起新的跨 Service Call。
4. NodeAgent 只接受配置的 ObserverNode 和非零 Source.ID；拒绝路径不调用 Reporter。
5. NodeAgent 的报告副本独立于 Monitor 和调用方修改。
6. Observer 冻结并排序 NodeConfig；Enabled/Disabled 初始状态分别为 unknown/disabled。
7. GetNodeDetail 不发远程 Call；RefreshNode 正确更新一个节点的 healthy/unavailable 状态和独立报告。
8. 本地与双节点 TCP 查询通过可组合 Codec；私有 lease Command 不可远程编解码。
9. Service 不创建 goroutine；无 HTTP、无第二套 RPC；Core 不导入 tooling/control。
10. `go test ./...`、`go vet ./...`、`go test -race ./...` 通过。
