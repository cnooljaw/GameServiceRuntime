# RFC-0250：Cluster Control Plane

> 状态：待实现
> 目标阶段：Phase 8A
> 范围：Runtime Tooling、Cluster Control Plane
> 依赖：[RFC-0130](RFC-0130-Core-Send-Call-Reply.md)、[RFC-0190](RFC-0190-Core-Cluster-Data-Plane.md)、[RFC-0191](RFC-0191-Core-Cluster-Transport.md)、[RFC-0192](RFC-0192-Core-Runtime-Inspection.md)、[RFC-0230](RFC-0230-Tooling-Monitor.md)
> 依据：Skynet debug_console、hanxi/skynet-admin 的观测分层

## 目的

本文冻结 Phase 8A 的最小 Cluster Control Plane：可信集群内的只读节点观测与汇总。

它让一个 ClusterControlService 保存静态节点期望，并经由远端 NodeAgentService 取得本地 Monitor 报告。调用继续使用 ServiceRef、Command、Call 和 Cluster Data Plane；Control Plane 不新增 RPC、Transport 协议或 Core getter。

## 目标

Phase 8A 必须交付：

- 每个节点一个只读 NodeAgentService，只消费本地 monitor.Monitor 的独立 Report。
- 一个 ClusterControlService，保存配置期望状态、刷新单节点观测并返回稳定的节点详情副本。
- 通过现有远程 Call 查询 NodeAgent 的 Codec、Client 和双节点 TCP 验收。
- 节点期望状态和观测状态的明确区分，以及不可用、超时和坏响应的稳定观测结果。

## 非目标

Phase 8A 不实现：

- HTTP、CLI、Web Console、外部 Admin API、用户认证、授权 token 或人类操作者身份。
- reload、启停节点、Drain、ServiceGroup 切换、回滚、远程代码执行或直接 Runtime.Stop。
- 自动 Discovery 心跳、动态 TCP peer 更新、配置持久化、选主、跨 ControlService 同步或后台轮询。
- Transport 连接对象、进程内存、Service 指针、业务领域指标或业务状态。
- 通用审计日志接口。修改型命令尚未存在，不能伪造“已审计”的能力；后续引入高危命令时必须先冻结认证、授权、二次确认和审计记录格式。

## 分层与依赖

~~~text
外部部署组合根（静态 NodeTarget）
  -> ClusterControlService
  -> Command + Call
  -> NodeAgentService
  -> monitor.Monitor
  -> Runtime.Inspect()
~~~

依赖方向固定为：

~~~text
tooling/control -> tooling/monitor -> runtime
runtime -X-> tooling/control
~~~

NodeAgentService 不读取 Runtime 私有字段，只调用注入的 Reporter.Capture。ClusterControlService 不持有 NodeAgent 指针，只保存 ServiceRef 并以 ServiceContext.Call 发起请求。组合根负责创建 Service、解析或提供 Agent ServiceRef，并在其变化时重建本阶段的静态控制面；Service Handler 不调用 Runtime.ResolveRemote、不做配置 I/O，也不启动 goroutine。

## 可信集群边界

Phase 8A 只运行在 RFC-0191 定义的可信集群网络内。它没有外部 Admin adapter，因此不把 Transport 握手 NodeID 误称为用户认证。

每个 NodeAgent 配置一个 ControlNode。它只接受 CommandContext.Source 同时满足下列条件的请求：

1. Source.Node 等于 ControlNode。
2. Source.ID 非零。

因此外部 Runtime.Call 形成的节点级 caller 不能直接读取 NodeAgent；请求必须由 ControlService 发起。可信 Control 节点上的其它错误程序仍可能伪装为该节点的普通 Service，这正是当前 Cluster 的信任模型。它只影响只读信息；加入修改型命令前必须引入更强的认证与授权 seam。

## 公开契约

包路径为 tooling/control。

~~~go
const DefaultNodeAgentName gsr.ServiceName = ".node-agent"
const DefaultControlName gsr.ServiceName = ".cluster-control"

type Reporter interface {
    Capture() monitor.Report
}

type NodeAgentConfig struct {
    Reporter    Reporter
    ControlNode gsr.NodeID
}

type NodeDesiredState struct {
    ID      gsr.NodeID
    Address string
    Role    string
    Enabled bool
}

type NodeTarget struct {
    Desired NodeDesiredState
    Agent   gsr.ServiceRef
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
    Desired   NodeDesiredState
    Observed  NodeObservedState
    Report    monitor.Report
    HasReport bool
}

type ControlConfig struct {
    Nodes       []NodeTarget
    CallTimeout time.Duration
    Now         func() time.Time
}

type CommandCaller interface {
    Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error)
}

func NewNodeAgentService(NodeAgentConfig) (gsr.Service, error)
func NewClusterControlService(ControlConfig) (gsr.Service, error)
func NewClient(CommandCaller, gsr.ServiceRef) (*Client, error)
func NewCodec(fallback gsr.ClusterCodec) gsr.ClusterCodec
~~~

DefaultNodeAgentName 与 DefaultControlName 只是节点内稳定名字，不能被当作跨节点动态 ServiceID。远端调用方应由组合根在已知节点上调用 Runtime.ResolveRemote，再把得到的 ServiceRef 作为 NodeTarget.Agent 创建 ControlService。

ControlConfig.Nodes 是本阶段唯一的 Desired State owner。NodeID 必须唯一；Address 和 ID 必须非空；Role 可以为空；Enabled 节点必须提供 Node 相同且 ID 非零的 Agent ref。Disabled 节点可以省略 Agent ref，且永不发起远程 Call。Config 在创建时深复制，调用方后续修改不影响 Service。

CallTimeout 零值默认 3 秒；负值、nil Now、nil Reporter、空 ControlNode、非法 Agent ref、重复 NodeID 和 nil CommandCaller 返回 ErrInvalidConfig 或 ErrInvalidCaller。Now 为 nil 时默认 time.Now。

## Command 与 Codec

CommandID 固定为：

~~~text
0x02500101 GetNodeReport        NodeAgentService
0x02500201 ListNodes            ClusterControlService
0x02500202 GetNodeDetail        ClusterControlService
0x02500203 RefreshNode          ClusterControlService
~~~

所有公开操作都是 Call；不提供 Send 版本。调用方使用 Client，不直接构造请求。NewCodec 用标准库 JSON 编解码上述请求与响应，并将其它 Command 委托 fallback；无 fallback 时返回 ErrUnsupportedCommand。它必须拒绝类型不匹配、畸形 JSON、尾随 JSON 值和内部结构不变量不成立的成功响应。

Codec 只处理 payload，不处理 TCP、连接、认证、WireEnvelope 或 Core bootstrap Command。它可包裹 Discovery Codec，或被 Discovery Codec 包裹；两者均以 CommandID 分派，顺序不改变各自命令语义。

## 状态与生命周期

NodeAgent 不保存可变业务状态。每次 GetNodeReport 在自己的 Mailbox Handler 中调用一次 Reporter.Capture，并 Reply 一个独立 monitor.Report。返回报告中的切片、元素和 map 必须与 Reporter 的后续报告隔离。

ClusterControlService 在自己的 Mailbox 内保存：

- 创建时冻结的 NodeTarget。
- 每个节点最近一次 NodeObservedState。
- 最近一次成功报告的独立副本。

初始状态：Enabled 节点为 unknown，Disabled 节点为 disabled。ListNodes 只返回按 NodeID 排序的当前详情副本。GetNodeDetail 不触发网络 I/O，只返回缓存副本。RefreshNode 只刷新一个 Enabled 节点：它用 ServiceContext 发起一次到 Agent 的 Call，在完成后以同一 Handler 写入新的 Observed State 和报告副本。

ControlService 的远程等待遵循 Core 的 Call 规则：等待期间归还 Scheduler 许可，但 ControlService 保持 busy，因此同一 ControlService 的刷新严格串行。第一版不做并行批量刷新、后台 poll 或跨节点合并；调用方需要刷新多个节点时逐一发 Command。

Service Stop 与 Close 只清理私有缓存和依赖引用；不取消或关闭其它 Service，不创建 Timer，也不创建 goroutine。

## 错误与失败语义

稳定错误：

~~~text
ErrInvalidConfig
ErrInvalidCaller
ErrInvalidNode
ErrNodeNotFound
ErrNodeDisabled
ErrUnauthorized
ErrInvalidResponse
ErrUnsupportedCommand
~~~

NodeAgent 对非 ControlService 来源 Reply ErrUnauthorized 的领域响应；它不得读取报告后再拒绝。Client 将领域响应还原为上述包内 sentinel，供本地与远端调用使用 errors.Is 判断。

RefreshNode 的目标是更新观测，而非把节点不可用变成 ControlService 故障：Agent 的超时、断线、远端不可用、payload 解码失败或无效成功响应均写入该节点的 unavailable 状态，清空 HasReport，设置稳定 LastError（timeout、remote_unavailable 或 invalid_response），并 Reply 更新后的 NodeDetail。它不得泄漏底层错误字符串、地址外的连接细节或业务 payload。

无效节点、Disabled 节点、错误来源或错误请求 payload 是调用错误，必须以领域错误 Reply，且不得修改其它节点缓存。NodeAgent 或 ControlService 因 Runtime 关闭、生命周期、Mailbox、Call cycle 等基础设施错误无法处理 Command 时，必须直接返回 Core 错误，不得伪装为 ErrInvalidResponse。

迟到 Agent Reply 由 Core PendingCall 丢弃；它不得覆盖一个已经完成的 RefreshNode。每次刷新只写自己对应的 NodeID，不能修改其它 Node 的观测。

## 并发、所有权与可观测性

NodeAgent 和 ControlService 都是普通 Service；所有状态修改只在 Handler 中发生，均不得创建 goroutine。monitor.Report 和 NodeDetail 的全部切片、元素和 map 必须深复制。调用方修改任意返回值不得改变 Service 缓存、Monitor 或下一次查询结果。

ControlService 在每次刷新后通过 ServiceContext.Metrics 记录：

~~~text
control_refresh_succeeded_total
control_refresh_failed_total
control_node_status
control_refresh_latency
~~~

前两个是计数器；control_node_status 以节点 NodeID 作为既有 Metrics 名称的一部分，不在 Core 引入标签模型；control_refresh_latency 记录一次 Call 结束的耗时。NodeAgent 不增加 Core 专用 getter；报告事实只来自 Monitor。

部署组合根可以记录 ControlService 构造、Agent 配置和外层请求日志，但不得记录完整 Monitor Report、token、Transport 内部对象或未脱敏业务字段。本阶段没有审计事件，因为不存在修改型管理命令。

## 与后续阶段的关系

Discovery 仍只保存活动租约和长期名字；Phase 8A 不自动续租，也不以 Observed State 回写 Discovery。配置 reload、动态 Agent ref 更新、启停、Drain 和 ServiceGroup 编排留待后续兼容扩展。

后续高危命令至少必须补充：外部 Admin adapter 的认证 principal、动作级授权、请求 ID、不可变审计记录、二次确认、超时/部分成功语义和失败回滚。它们不能复用 Phase 8A 的 NodeID 来源检查作为用户授权。

## 验收

必须覆盖：

1. NodeAgent 只接受配置的 ControlNode 且 Source.ID 非零的请求；拒绝路径不调用 Reporter。
2. NodeAgent 返回一次 Capture 的独立 Report 副本，修改返回值不影响下一次结果。
3. ControlService 冻结并排序 Desired State；Enabled/Disabled 初始状态分别为 unknown/disabled。
4. GetNodeDetail 不做远程 Call；RefreshNode 成功后更新同一节点的 healthy 状态、报告和 latency。
5. 超时、断线、无效 Agent response 和 payload 解码失败只把目标节点更新为 unavailable，返回稳定 LastError，不泄漏底层 cause。
6. Unknown、Disabled、重复配置和无权来源被稳定拒绝，且不污染其它节点状态。
7. 本地与双节点 TCP 调用都通过 NewCodec 完成；Codec fallback、未知字段、尾随 JSON 和错误类型保持受到测试保护。
8. Service 不创建 goroutine；无 Timer、无 HTTP、无第二套 RPC；Core 不导入 tooling/control。
9. go test ./...、go vet ./...、go test -race ./... 通过。
