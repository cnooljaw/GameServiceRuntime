# RFC-0270：Drain、热更新与访问者追踪

> 状态：已接受
> 目标阶段：Phase 10A
> 接受日期：2026-07-23
> 范围：Runtime Tooling、Cluster
> 依赖：[RFC-0180](RFC-0180-Core-Lifecycle.md)、[RFC-0190](RFC-0190-Core-Cluster-Data-Plane.md)、[RFC-0260](RFC-0260-Tooling-ServiceGroup-Routing.md)
> 依据：`skynet_fly` 的访问者追踪和旧 Service 下线流程

## 目的

本文冻结 Phase 10 的第一个可验证纵向切片：`VisitorRegistryService`。它以有限 lease 保存“哪个 Service 正在使用哪个目标 Service”的 Tooling 事实，为后续 Drain 判断旧实例能否退出提供唯一状态 owner。

这里的热更新仍然只指进程外切换：启动新实例、发布更高版本的 `ServiceSet`、Drain 旧实例并最终 `Stop`。它不是 Go 进程内代码热补丁。

## 目标

Phase 10A 必须交付：

- 独立的 `tooling/drain` 包和 `VisitorRegistryService`。
- 带 AuthorityEpoch、Generation、owner 和过期时间的 `VisitorLease`。
- 显式 `Weak Visitor`、Acquire、Renew、Release 和稳定排序的 List。
- Runtime Timer 驱动的过期清理；Timer 只投递私有 Command。
- 类型化 Client、可组合 Cluster Codec、本地与双节点 TCP 验收。
- 返回副本、迟到 Release fencing、过期语义和基础指标。

## 非目标

Phase 10A 不实现：

- `DrainService`、新外部流量拒绝 decorator 或业务入口 guard。
- 创建新实例、健康检查、`ServiceSet` 发布、自动回滚或 `Runtime.Stop` 编排。
- Service Desired State、Controller、Reconcile、NodeAgent 修改型动作、扩缩容、放置和故障迁移。
- 外部 Admin API、认证 principal、动作级授权、二次确认和审计。
- 任意代码热替换、进程内热补丁、自动状态迁移或持久化 Visitor lease。
- 由非 Service 的连接对象直接作为 Visitor；Gateway 或业务会话必须在后续契约中映射为持有明确 `ServiceRef` 的 owner。

后续 Phase 10B 先冻结并交付独立的 Drain guard（见 [RFC-0271](RFC-0271-Tooling-Drain-Guard.md)）。ServiceSet 切换、失败回滚和超时后的人工处理必须随携带 principal、`RequestID`、动作授权和审计的控制面操作契约一起进入后续阶段；它们不得把责任回填给本 RFC 的 Registry。

## 分层与依赖

```text
DrainService / 业务 Service / 后续 Controller
  -> drain.Client
  -> VisitorRegistryService owns target -> visitor leases
  -> Runtime Command、Mailbox、Timer、Cluster Codec

tooling/drain      -> runtime
tooling/drain      -> tooling/servicegroup（只在后续 Drain 编排中消费公开快照）
runtime            -X-> tooling/drain
tooling/discovery  -X-> tooling/drain
```

`VisitorRegistryService` 不持有目标或访问者的 Service 指针，也不探测它们是否存活。它只保存经过 Command 写入的 lease 事实。一个调用方通过 `Client` 使用 Registry；Client 不返回内部 map、不创建 goroutine，也不维护后台缓存。

## 公开契约

包路径为 `tooling/drain`。

```go
const DefaultVisitorRegistryName gsr.ServiceName = ".visitor-registry"

type VisitorLease struct {
    Target         gsr.ServiceRef
    Visitor        gsr.ServiceRef
    AuthorityEpoch uint64
    Generation     uint64
    Weak           bool
    ExpiresAt      time.Time
}

type VisitorRef struct {
    Visitor    gsr.ServiceRef
    Generation uint64
    Weak       bool
    ExpiresAt  time.Time
}

type VisitorRegistryConfig struct {
    LeaseTTL      time.Duration
    SweepInterval time.Duration
}

type CommandCaller interface {
    Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error)
}

type Client struct { /* private */ }

func NewVisitorRegistryService(VisitorRegistryConfig) (gsr.Service, error)
func NewClient(CommandCaller, gsr.ServiceRef) (*Client, error)
func NewCodec(fallback gsr.ClusterCodec) gsr.ClusterCodec

func (*Client) Acquire(context.Context, gsr.ServiceRef, gsr.ServiceRef, bool) (VisitorLease, error)
func (*Client) Renew(context.Context, VisitorLease) (VisitorLease, error)
func (*Client) Release(context.Context, VisitorLease) error
func (*Client) List(context.Context, gsr.ServiceRef) ([]VisitorRef, error)
```

`Target` 是正在被使用、可能需要 Drain 的 Service；`Visitor` 同时是 lease 的 owner。`Weak=false` 表示强访问者，后续 Drain 必须等待它释放或过期；`Weak=true` 明确表示该关系不阻止退出。第一版不从调用方式、组成员关系或业务类型推断 Weak。

`VisitorLease` 的零值无效。`Target` 与 `Visitor` 都必须具有有效 `NodeID` 和非零 `ServiceID`；AuthorityEpoch、Generation 和 `ExpiresAt` 也必须非零。`VisitorRef` 是 `List` 的只读副本，按 `Visitor.Node`、`Visitor.ID` 升序排列。

Command ID 固定为：

```text
0x02700101 AcquireVisitorLease
0x02700102 RenewVisitorLease
0x02700103 ReleaseVisitorLease
0x02700104 ListVisitors
0x027001fe SweepExpiredVisitors（私有）
```

Client 只处理上述类型化 Call。调用方不得直接构造 wire payload，也不得保存或修改 Registry 内部状态。`NewCodec` 使用标准库 JSON 编解码这四个公开 Command 及其响应；私有 sweep Command 不可远程编解码，并由 fallback 或 `ErrUnsupportedCommand` 拒绝。

## 状态与生命周期

Registry 每次创建生成非零随机 AuthorityEpoch，并在自己的 Mailbox 内保存：

```text
Target ServiceRef -> Visitor ServiceRef -> VisitorLease
```

Generation 在同一个 AuthorityEpoch 内全局单调分配，跳过零值；回绕返回 `ErrLeaseExhausted`，不得覆盖现有 lease。Registry 不持久化状态；重建后的 AuthorityEpoch 不同，来自旧实例的 lease 一律失效。

`LeaseTTL=0` 默认 30 秒，`SweepInterval=0` 默认 5 秒；负值无效。每个 Command 开始时先以 `ServiceContext.Now()` 清理 `now >= ExpiresAt` 的 lease。首个有效 Acquire 在提交前通过 `ServiceContext.After` 安排一个私有 sweep Command。只有仍存在 lease 时，sweep 成功后才安排下一次；Registry 为空时不得安排下一次 Timer。已经入队的 sweep Timer 由 Core 在触发或目标停止时清理。

Acquire、Renew、Release 的来源约束如下：

1. Acquire 的 `CommandContext.Source()` 必须与请求中的 `Visitor` 完全相同。
2. Renew 和 Release 的 Source 必须与 `VisitorLease.Visitor` 完全相同。
3. `Runtime.Call` 的节点级 source（`ServiceID(0)`）不能创建、续订或释放 lease。

Acquire 为同一 `(Target, Visitor)` 分配更高 Generation，并替换旧 lease；此前 lease 的 Renew 或 Release 不能影响新 lease。Renew 保持 AuthorityEpoch、Generation、Target、Visitor 和 Weak 不变，只延长 `ExpiresAt`。Release 只删除完全匹配当前 AuthorityEpoch、Generation、Weak 和 `ExpiresAt` 的 lease。因而迟到 Release 或 Renew 不能删除重 Acquire 或已续订的 lease。

List 只返回尚未过期的独立 `VisitorRef` 副本。目标没有有效 lease 时返回非 nil 空切片，不是错误。它是 Tooling 事实查询；第一版可信集群内的任意 caller 都可读取，但不获得变更能力。

`Stop` 只清空 Registry 状态；`Close` 清空保存的 `ServiceContext`。停止和 Runtime Closing 路径不发送 Release 或通知，由 Timer 与 lease 到期收敛。

## 错误与失败语义

稳定错误：

```text
ErrInvalidConfig
ErrInvalidCaller
ErrInvalidLease
ErrInvalidTarget
ErrInvalidVisitor
ErrLeaseExpired
ErrLeaseOwnerMismatch
ErrLeaseExhausted
ErrInvalidResponse
ErrUnsupportedCommand
```

Client 在发出 Call 前验证自己能够判断的参数。Registry 也必须验证 wire payload、Command 来源和所有 lease 不变量；非法、过期或越权请求不得改变状态。领域错误以稳定响应码跨节点返回；Runtime、Transport、context 取消或超时错误保持原语义。

创建首个 Timer 失败时，Acquire 不得提交 lease，也不得 Reply 成功。Timer 投递失败仍由 Core Timer 指标观测；Registry 不在 Handler 外补发 goroutine 或回调。返回 payload 类型不匹配、畸形 JSON、尾随 JSON、未知响应码或不变量不成立时，Client/Codec 返回 `ErrInvalidResponse`。

不存在或已过期 lease 的 Renew/Release 返回 `ErrLeaseExpired`。调用方应在其自己的 Mailbox 中决定重试、重 Acquire 或业务降级；Registry 不自动重试，也不尝试探测 visitor 或 target。

## 并发、所有权与可观测性

Registry 的 map、Generation 和 sweep 状态只在 Handler 中修改。Service、Client 和 Codec 都不得创建 goroutine；Timer 只投递 `SweepExpiredVisitors` Command。所有 List 结果和成功响应都与内部状态及调用方后续修改隔离。

Registry 记录以下 Metrics：

```text
visitor_leases
visitor_strong_leases
visitor_acquire_total
visitor_renew_total
visitor_release_total
visitor_expired_total
```

指标从 `ServiceContext.Metrics()` 写入；读取方继续通过 `Runtime.Inspect().Metrics` 获取快照。Registry 不增加 Core getter、HTTP、日志 adapter 或外部管理协议。

## 与后续 Drain 的关系

后续受控 Drain 操作必须先发布新的 `ServiceSetVersion`，再使旧实例拒绝新外部工作，最后以 `List(target)` 判断强访问者是否清零。ServiceGroup 切换只能影响通过组解析的新请求；缓存旧 `ServiceRef` 的调用方仍可能直接投递，因此 [Drain guard](RFC-0271-Tooling-Drain-Guard.md) 必须位于 Tooling decorator 或业务入口 Command，不能声称 Core 原生存在 `Draining` 状态。

已经发布后的回滚不得倒退 Directory Revision。尚未开始任何旧实例 Guard 时，后续控制面可以把旧 Refs 作为更高 Revision 的新 ServiceSet 发布；任一 Guard 开始后，原 Ref 已不可重新接流，恢复必须准备新实例并发布更高 Revision。控制面必须记录不确定结果以支持人工恢复。Directory AuthorityEpoch 改变时，调用方必须完整替换快照，不能跨 epoch 比较 Revision。

## 验收

必须覆盖：

1. Acquire 生成有效 epoch、Generation 和过期时间；List 按稳定顺序返回独立副本。
2. 同一 `(Target, Visitor)` 重 Acquire 生成新 Generation，旧 lease 的 Release 或 Renew 被 fencing。
3. Renew 只延长当前 lease；迟到 Release 不能删除已续订 lease。
4. 强、弱访问者同时存在时都能列出；后续 Drain 只需等待强访问者。
5. Acquire、Renew、Release 精确校验 Command Source；节点级和其它 Service source 不能修改 lease。
6. lease 过期、Timer sweep、空 registry 不再安排下一次 Timer、停止取消目标 Timer 均正确。
7. AuthorityEpoch 变化、Generation 回绕、非法输入、错误响应和 Timer 安排失败不破坏已有状态。
8. Codec 拒绝私有 Command、畸形 JSON、尾随 JSON、类型错误和无效成功响应，并正确委托 fallback。
9. 本地与双节点 TCP 的 Service caller 可完成 Acquire、Renew、List 和 Release；远程领域错误可类型化识别。
10. Core、Discovery 和 ServiceGroup 不导入 `tooling/drain`；Service 不创建 goroutine；`go test ./...`、`go vet ./...`、`go test -race ./...` 通过。
