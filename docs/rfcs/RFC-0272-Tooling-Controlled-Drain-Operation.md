# RFC-0272：受控 Drain 操作

> 状态：已接受
> 接受日期：2026-07-23
> 目标阶段：Phase 10C1
> 范围：Runtime Tooling、Cluster Control Plane
> 依赖：[RFC-0130](RFC-0130-Core-Send-Call-Reply.md)、[RFC-0180](RFC-0180-Core-Lifecycle.md)、[RFC-0250](RFC-0250-Tooling-Cluster-Control-Plane.md)、[RFC-0260](RFC-0260-Tooling-ServiceGroup-Routing.md)、[RFC-0270](RFC-0270-Tooling-Drain-Hot-Reload.md)、[RFC-0271](RFC-0271-Tooling-Drain-Guard.md)
> 依据：GSR 的 Mailbox 状态 owner、Directory compare-and-set 与 Guard 的不可逆 Begin 语义

## 目的

本文冻结 Phase 10C 的第一个可验证纵向切片：`DrainCoordinatorService`。它在一个 Control Plane Service 的 Mailbox 内保存经授权、带 `RequestID` 的 Drain 操作记录，按顺序消费 Directory、Drain Guard 和 Visitor Registry 的公开 API，并在旧实例的强访问者清零时到达 `ReadyToStop`。

它解决的是「切换成功但调用方没有拿到响应」「Guard Begin 的结果未知」和「谁授权了这次下线」不能散落在组合根或脚本中的问题。它不是通用 Desired State Controller，也不执行 `Runtime.Stop`。

## 目标

Phase 10C1 必须交付：

- `tooling/control` 的 `DrainCoordinatorService`、类型化 `DrainClient` 和可组合 Cluster Codec。
- 由精确 Gateway Service source、`Principal` 白名单和稳定 `RequestID` 共同约束的 Start、Resolve、Get、ListAudit Command。
- 单一 Mailbox owner 保存不可变输入、原 ServiceSet、发布结果、每个旧实例的 Guard 状态、强访问者计数、阶段和有界审计记录。
- Directory 发布成功后的新版本确认、发布未知结果的只读确认、Guard 幂等重试、Visitor 显式刷新和 `ReadyToStop` 结论。
- 本地与双节点 TCP 验收；所有返回的 ServiceSet、slice、map 与审计记录均为独立副本。

## 非目标

Phase 10C1 不实现：

- `Runtime.Stop`、NodeAgent 修改型 Command、创建新实例、健康检查、放置、扩缩容、Desired State、后台 Reconcile、Timer 轮询或自动重试。
- HTTP、CLI、Web Console、认证协议或用户 token 校验。Gateway 必须先完成认证，再以它自己的 ServiceRef 和 `Principal` 调用 Coordinator。
- 持久化操作记录、跨 Coordinator 恢复、选主、HA、操作超时自动终结、外部审计 sink 或二次确认。
- 在任何旧实例 Guard 已开始后，把原旧 `ServiceRef` 重新发布为回滚目标。原 Guard 不可 Resume；此后恢复只能准备可用的新实例并发布更高 ServiceSetVersion。

## 分层与依赖

```text
认证完成的 Gateway Service
  -> control.DrainClient.Start / Resolve / Get
  -> DrainCoordinatorService owns DrainOperation + Audit
     -> servicegroup.Client.Get / Publish
     -> drain.GuardClient.Begin / Status
     -> drain.Client.List

tooling/control -> tooling/servicegroup -> runtime
tooling/control -> tooling/drain        -> runtime
runtime         -X-> tooling/control / tooling/drain / tooling/servicegroup
```

`DrainCoordinatorService` 是操作事实的唯一 owner；Directory 仍是 ServiceSet 的唯一 owner，VisitorRegistryService 仍是 lease 的唯一 owner，Guard 仍是各目标入口状态的唯一 owner。Coordinator 不持有这些 Service 的指针，也不复制它们的内部 map。

部署必须使 Directory 的 `PublisherNode` 等于 Coordinator 所在 NodeID；Directory 的 NodeID 来源检查只是这条部署约束，不能替代 Gateway+Principal 授权。若配置不匹配，Directory 会确认拒绝 Publish，操作不会开始 Guard，运维者必须修正部署后创建新的 RequestID。

## 公开契约

包路径为 `tooling/control`。本 RFC 在既有只读 Observer API 外增加：

```go
const DefaultDrainCoordinatorName gsr.ServiceName = ".drain-coordinator"

type Principal string
type RequestID string

type DrainPhase string
const (
    DrainPreparing      DrainPhase = "preparing"
    DrainPublishUnknown DrainPhase = "publish_unknown"
    DrainGuarding       DrainPhase = "guarding"
    DrainWaitingVisitors DrainPhase = "waiting_visitors"
    DrainReadyToStop    DrainPhase = "ready_to_stop"
    DrainConflict       DrainPhase = "conflict"
    DrainSuperseded     DrainPhase = "superseded"
)

type DrainTarget struct {
    Ref                 gsr.ServiceRef
    Guarded             bool
    StrongVisitorCount  int
}

type DrainOperation struct {
    RequestID RequestID
    Principal Principal
    Group     servicegroup.GroupName
    Expected  servicegroup.ServiceSetVersion
    Original  servicegroup.ServiceSet
    Published servicegroup.ServiceSet
    Targets   []DrainTarget
    Phase     DrainPhase
    CreatedAt time.Time
    UpdatedAt time.Time
}

type DrainAudit struct {
    Sequence  uint64
    RequestID RequestID
    Principal Principal
    Action    string
    Outcome   string
    OccurredAt time.Time
}

type StartDrainRequest struct {
    RequestID RequestID
    Principal Principal
    Group     servicegroup.GroupName
    Expected  servicegroup.ServiceSetVersion
    NextRefs  []gsr.ServiceRef
    NextTags  map[string]string
}

type DrainCoordinatorConfig struct {
    Gateway           gsr.ServiceRef
    AllowedPrincipals []Principal
    Directory         gsr.ServiceRef
    VisitorRegistry   gsr.ServiceRef
    CallTimeout       time.Duration
    AuditLimit        int
}

type DrainClient struct { /* private */ }

func NewDrainCoordinatorService(DrainCoordinatorConfig) (gsr.Service, error)
func NewDrainClient(CommandCaller, gsr.ServiceRef) (*DrainClient, error)
func (*DrainClient) Start(context.Context, StartDrainRequest) (DrainOperation, error)
func (*DrainClient) Resolve(context.Context, RequestID, Principal) (DrainOperation, error)
func (*DrainClient) Get(context.Context, RequestID, Principal) (DrainOperation, error)
func (*DrainClient) ListAudit(context.Context, Principal) ([]DrainAudit, error)
```

`Principal` 与 `RequestID` 都必须是有效 UTF-8、非空且没有首尾空白的值。`Gateway`、Directory 和 VisitorRegistry 必须是完整 ServiceRef；Gateway 是认证 adapter 或上层控制 Service 的精确来源，而不是一个 NodeID。AllowedPrincipals 必须非空、无重复。Coordinator 只在 `CommandContext.Source() == Gateway` 且 Principal 位于 AllowedPrincipals 时接受 Start、Resolve、Get 和 ListAudit；查询也不绕过动作授权。Operation 只能由创建它的相同 Principal 读取或 Resolve。

`StartDrainRequest` 的 Expected 必须是非零 ServiceSetVersion；Coordinator 首先读取同一 Group，并且只有当前版本精确等于 Expected 才可取得 Original 和尝试 Publish。NextRefs/NextTags 依 Directory 的排序、去重与 tag 复制规则标准化，调用方不能指定 Published Version。`Targets` 只包含 Original 中**不再出现于** NextRefs 的 Ref；仍在 NextRefs 中的实例继续接流，不得被 Guard。相同 RequestID 的 Start 必须逐字段匹配标准化后的 Principal、Group、Expected、NextRefs 和 NextTags，匹配时返回独立的已有 Operation 而不再次 Call 下游；不匹配返回稳定 `ErrRequestConflict`。

Command ID 固定为：

```text
0x02500301 StartDrainOperation
0x02500302 ResolveDrainOperation
0x02500303 GetDrainOperation
0x02500304 ListDrainAudit
```

四个公开 Command 只通过 `DrainClient` 的 Call 使用。`NewCodec` 扩展标准库 JSON 编解码，并继续委托既有 Control Codec fallback；它拒绝类型错误、畸形或尾随 JSON、未知响应码和不满足 Operation 不变量的成功响应。

## 状态与生命周期

Coordinator 在自己的 Mailbox 内以 `RequestID -> DrainOperation` 和有界、单调 Sequence 的 `DrainAudit` slice 保存状态。每次接受的 Start 或 Resolve 都追加一条审计；超过 AuditLimit 时仅丢弃最旧审计并增加指标，绝不修改 Operation。`AuditLimit=0` 默认 1024，负值无效；CallTimeout=0 默认 3 秒，负值无效。

Start 首先记录不可变请求与 `DrainPreparing`，随后用有界 Call 读取 Directory。读取结果版本不等于 Expected 时，Operation 进入 `DrainConflict`，不会 Publish。读取成功后，Coordinator 用 Expected compare-and-set 发布 Next：

1. Publish 成功：保存 Directory 返回的 Published；从 Original.Refs 中计算不在 Published.Refs 的稳定排序 Targets，进入 `DrainGuarding`。
2. Publish 返回 `ErrVersionConflict`：进入 `DrainConflict`；不 Begin 任何 Guard。
3. 超时、断线、payload 或其它未确认错误：进入 `DrainPublishUnknown`。不得自动重发 Publish、不得假设失败、不得回滚 Revision。

Resolve 是唯一的显式推进动作，不启动 Timer 或 goroutine：

1. `DrainPreparing` 重新读取并尝试首次 Publish；它尚未发出 Publish，所以可安全继续。
2. `DrainPublishUnknown` 只 Get Directory。只有当前完整 ServiceSet 的 Version 与预期的下一个 Revision 相符且 Refs/Tags 与请求的 Next 内容相等时，才确认 Published 并进入 Guarding；当前 Version 仍等于 Expected 或 Get 不确定时保留 PublishUnknown，供人工稍后再次 Resolve。若当前 Version 已高于 Expected，则该 Expected 的迟到 CAS 已不可能再提交，进入 `DrainSuperseded`；即使本操作曾提交也已被更高版本取代。Get 不能在仍见 Expected 时证明超时的 Publish 永远不会迟到。
3. Guarding 先 Get 并确认 Directory 仍等于 Published；否则进入 `DrainSuperseded`，不再 Begin 新 Guard。它随后对每个尚未确认的 Original.Ref 调用 GuardClient.Begin。成功或 Status 已为 Draining 时标记 Guarded；超时、断线或无效响应保持 Guarding，供下一次 Resolve 重试。Begin 可幂等重试；它已经提交但 Reply 丢失时，Status 用于确认。
4. 全部 Target Guarded 后，Coordinator List 每个 Target 的 Visitor lease 并统计 `Weak=false`。每次 `DrainWaitingVisitors` 的 Resolve 也先确认 Directory 仍等于 Published；任一 List 不确定时保持 DrainWaitingVisitors；存在强访问者时也保持 DrainWaitingVisitors；全部为零时进入 DrainReadyToStop。
5. ReadyToStop、Conflict 与 Superseded 是终态；Resolve 只返回独立快照，不再调用下游。

`DrainReadyToStop` 只是可执行 Stop 的审计结论。它不授予任何 Runtime Stop 权限；后续 NodeAgent 动作必须以相同 RequestID 检查 Operation，并重新确认 Directory 仍等于 Operation.Published，且在独立契约中记录执行结果。

如果在任何 Begin 成功前必须取消切换，人工操作可以把 Original 内容作为更高 Revision 发布，且必须创建新的 RequestID 和操作记录。一旦任一 Target Guarded，Original Refs 不可重新发布；恢复必须准备新的可接流实例。Coordinator 不为两类恢复自动执行 Publish。

Stop 只清空操作和审计状态；Close 清空保存的 ServiceContext、客户端和 map。Coordinator 重建后丢失操作记录，调用方必须以 Directory、Guard Status 和 Visitor List 重新人工判断，不能凭失效 RequestID 继续 Stop。

## 错误与失败语义

稳定错误：

```text
ErrInvalidConfig
ErrInvalidCaller
ErrInvalidPrincipal
ErrInvalidRequestID
ErrInvalidDrainRequest
ErrRequestConflict
ErrDrainOperationNotFound
ErrOperationOwnerMismatch
ErrUnauthorized
ErrInvalidResponse
ErrUnsupportedCommand
```

输入、来源或授权失败不得调用 Directory、Guard 或 Visitor Registry，也不得创建 Operation；可信 Gateway 携带但不在 AllowedPrincipals 的 Principal 必须产生一条 `denied` 审计。下游 Runtime、Transport 和 context 错误不会伪装成成功：Coordinator 将它们归入相应非终态 Phase 和审计 outcome，再向调用方返回当前 Operation，使同 RequestID 的人工 Resolve 可见。

Directory VersionConflict 是可确认的 Conflict；Directory Get 在仍见 Expected 时不能否定一次超时 Publish，但更高 Version 已排除迟到 CAS 并使 Operation Superseded。此类未确认后直接发现更高 Version 的 Operation 没有可确认的 Published/Targets 快照。Guard Begin 的 ErrUnauthorized 表示部署配置与 Guard Controller 不匹配，仍保持 Guarding，不能跳过该 Target。Visitor lease 的 Weak=true 永不阻止 ReadyToStop；过期 lease 已由 Registry 的 List 语义排除。

## 并发、所有权与可观测性

Coordinator 的 Operation、Audit、Sequence、目标状态和客户端只在 Handler 中修改。它不创建 goroutine；每个下游 Call 使用 `context.WithTimeout(context.Background(), CallTimeout)`，并让 Runtime 在 Call 等待时归还执行许可。无后台 poll、无隐式重试、无 channel 和无 Service 指针。

Coordinator 记录：

```text
drain_operations_started_total
drain_operations_duplicate_total
drain_operations_conflict_total
drain_operations_publish_unknown_total
drain_operations_guard_unknown_total
drain_operations_ready_total
drain_operations_denied_total
drain_audit_evicted_total
```

读取方只能经 `Runtime.Inspect().Metrics` 取得指标。Coordinator 不向 Core 增加权限、审计或 Stop API。

## 验收

必须覆盖：

1. 配置拒绝无效 Gateway/Directory/VisitorRegistry、空或重复 Principal、负超时/审计上限；所有导出快照独立。
2. Start 精确校验 Gateway Source 与 Principal；拒绝不触碰下游，可信 Gateway 的 denied 请求留下有界审计。
3. 相同 RequestID 的相同 Start 不重复 Publish/Begin；任一输入差异返回 ErrRequestConflict；不同 Principal 不能读取或 Resolve 操作。Next 保留的 Original Ref 不得被 Guard，只有被移除的 Ref 进入 Targets。
4. 成功 Start 以 Expected CAS 发布更高 Version、记录 Original/Published、依次 Begin 全部旧 Target，并在强 Visitor 清零后 ReadyToStop。
5. 版本冲突不 Begin Guard；Publish 超时后不自动重发，Resolve 只能通过完全匹配的 Get 确认，仍见 Expected 或 Get 不确定时保持 PublishUnknown，更高 Version 时进入 Superseded。
6. Guard Reply 丢失、未授权或暂时不可达时不遗漏 Target；Resolve 用 Status/幂等 Begin 收敛。Directory 被其它发布者改变时进入 Superseded，不再 Begin。
7. 强 Visitor 阻止 ReadyToStop，Weak/过期 Visitor 不阻止；Resolve 不轮询、不创建 Timer 或 goroutine。
8. Codec 正确组合现有 Control、Drain 与 ServiceGroup Codec，拒绝类型、JSON、响应不变量错误；双节点 TCP 中 Gateway Service 可 Start/Resolve，节点级 caller 被拒绝。
9. Core、Discovery、Directory、Visitor Registry 和 Guard 不导入 `tooling/control`；`go test ./...`、`go vet ./...`、`go test -race ./...` 通过。
