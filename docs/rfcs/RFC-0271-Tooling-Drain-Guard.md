# RFC-0271：Drain Guard

> 状态：已接受
> 目标阶段：Phase 10B
> 接受日期：2026-07-23
> 实现日期：2026-07-23
> 范围：Runtime Tooling
> 依赖：[RFC-0100](RFC-0100-Core-Service.md)、[RFC-0130](RFC-0130-Core-Send-Call-Reply.md)、[RFC-0190](RFC-0190-Core-Cluster-Data-Plane.md)、[RFC-0270](RFC-0270-Tooling-Drain-Hot-Reload.md)
> 依据：Skynet 的旧 Service 下线边界，以及 GSR 的 Service decorator 和 Mailbox 串行规则

## 目的

本文冻结 Phase 10B 的独立纵向切片：`Drain Guard`。它把一个业务 Service 包装为可由受信任协调 Service 进入 Drain 状态的入口；进入后，Guard 在目标 Service 自己的 Mailbox 内拒绝已显式声明的外部工作 Command，而内部清理 Command 仍可继续执行。

Guard 解决的是「已经拿到旧 `ServiceRef` 的调用方仍可直接投递」这一问题。它不是 Core 的新生命周期状态，不创建 `DrainService`，也不决定何时发布 `ServiceSet`、等待 Visitor 或执行 `Runtime.Stop`。

## 目标

Phase 10B 必须交付：

- 位于 `tooling/drain` 的 Service decorator、`GuardConfig`、`DrainStatus` 和类型化 `GuardClient`。
- 由 `BeginDrainCommand` 串行地切换到不可逆 Drain 状态，并以 `GetDrainStatusCommand` 查询独立状态快照。
- 精确到 `ServiceRef` 的协调者来源约束、显式外部 Command 清单、稳定 `ErrDraining` 和幂等 Begin。
- 对公开 Guard Command 的可组合 Cluster Codec、本地测试和双节点 TCP 验收。
- 对生命周期、Startup Command 和指标的透明转发；不在 Guard、Client 或 Codec 中创建 goroutine。

## 非目标

Phase 10B 不实现：

- 创建新实例、健康检查、`ServiceSet` 发布、切换、回滚、Visitor 查询、超时轮询或 `Runtime.Stop`。
- `DrainService`、Service Desired State、Controller、Reconcile、NodeAgent 修改型动作、扩缩容、放置和故障迁移。
- 外部 Admin API、认证 principal、动作级授权、`RequestID`、审计、二次确认或持久化操作记录。
- 恢复到「接受外部工作」的 Resume Command。Guard 一旦 Begin 成功即不可逆，旧实例的再启用只能通过后续控制面发布更高 `ServiceSetVersion` 并准备新的实例实现。
- 依据网络连接、调用来源、ServiceGroup 成员或业务 payload 自动推断什么是「外部工作」。

ServiceGroup 切换、失败回滚和超时后的人工恢复不能由无操作身份的 Guard 承担。它们会遇到「Directory 已提交、调用方却因网络超时未获响应」的未知结果；后续控制面契约必须将这些动作与 `RequestID`、授权、审计和可查询的操作记录一起冻结。

## 分层与依赖

```text
后续受控 Drain 操作 / 受信任协调 Service
  -> GuardClient.Begin(target)
  -> Drain Guard（目标 Service 的 Mailbox）
  -> 允许内部清理 Command / 拒绝 ExternalCommands

业务 Service -> tooling/drain -> runtime
runtime      -X-> tooling/drain
tooling/drain -X-> tooling/servicegroup（本切片）
```

`GuardConfig` 不接受 Service 指针、Runtime 指针或 Stop 回调。Guard 只持有被包装的 `Service` 和冻结后的值配置；跨 Service 协作仍只通过 `ServiceRef` 与 Command。

## 公开契约

包路径为 `tooling/drain`，在 [RFC-0270](RFC-0270-Tooling-Drain-Hot-Reload.md) 的 Visitor Registry API 之外增加：

```go
const BeginDrainCommand gsr.CommandID = 0x02700201
const GetDrainStatusCommand gsr.CommandID = 0x02700202

type GuardConfig struct {
    Controller       gsr.ServiceRef
    ExternalCommands []gsr.CommandID
}

type DrainStatus struct {
    Draining  bool
    StartedAt time.Time
}

type GuardClient struct { /* private */ }

func Decorate(gsr.Service, GuardConfig) (gsr.Service, error)
func NewGuardClient(CommandCaller, gsr.ServiceRef) (*GuardClient, error)
func (*GuardClient) Begin(context.Context) (DrainStatus, error)
func (*GuardClient) Status(context.Context) (DrainStatus, error)
```

`Controller` 必须是 Node 非空、ServiceID 非零的完整 `ServiceRef`，并且不得与被包装 Service 的实际 Self 相同。它代表可信集群内、可开始 Drain 的单一协调 Service；这只是实例级来源 fencing，不是外部用户认证。

`ExternalCommands` 必须非空、非零、无重复，并且不能使用 `BeginDrainCommand` 或 `GetDrainStatusCommand`。Core 不维护 Service Command 清单，因此 Guard 不尝试预检内层是否处理这些 Command；清单是组合根对外部入口的显式配置。Guard 的两个控制 Command 属于保留编号，内层 Service 不得赋予它们其它含义。清单外 Command 被视为内部 Command，Guard 原样转交内层；组合根必须审计并列出所有会接收新业务流量的入口。

`DrainStatus` 是值快照：未开始时 `Draining=false` 且 `StartedAt` 为零；开始后 `Draining=true` 且 `StartedAt` 为非零、首次成功 Begin 时由 `ServiceContext.Now()` 记录。状态不携带 Visitor、ServiceGroup 或 Stop 进度。

`GuardClient` 只封装对一个 Guard ServiceRef 的类型化 `Call`，不保存业务状态、不创建后台任务。它在发起 Call 前验证 caller 和 target。`Begin` 的调用方通常是配置为 `Controller` 的 Service，因而其 `ServiceContext.Call` 会携带精确 Service source；Runtime 节点级 caller 不能开始 Drain。`Status` 是可信集群内的只读查询，不改变状态。

Command ID 固定为：

```text
0x02700201 BeginDrain
0x02700202 GetDrainStatus
```

`NewCodec` 继续使用标准库 JSON，增加上述两个公开 Command 及其响应的编解码，并与 Visitor Registry Command 一样委托 fallback。它拒绝类型不匹配、畸形 JSON、尾随 JSON、无效成功响应和未知响应码；不处理 ServiceGroup、Timer 或 Stop Command。

## 状态、顺序与生命周期

Guard 只在自己的 `Handle` 中保存：

```text
draining bool
startedAt time.Time
```

这两个字段与被包装 Service 的 handler 共用同一个目标 Mailbox。`BeginDrain` 到达前已经排在该 Mailbox 中的外部 Command 仍依原顺序交给内层；它处理完成后，后续到达的、位于 `ExternalCommands` 的 Command 在调用内层之前返回 `ErrDraining`。因此 Guard 不承诺跨多个目标的全局线性顺序，只提供单个旧实例入口的串行边界。

`Begin` 要求 `CommandContext.Source()` 与 `GuardConfig.Controller` 完全相等。来源或 payload 无效时不得改变状态；首次有效 Begin 在 Reply 前记录状态，后续有效 Begin 不改变 `StartedAt`、仍返回同一成功快照。因而 Reply 投递失败、远端调用超时或受信任控制者使用 Send 时，状态是否已开始可能未知，控制者必须用 `Status` 重新判定；它不能把成功投递误当成成功开始。`GetDrainStatus` 不检查来源，只返回独立值快照。

当 Guard 正在 Drain 时，声明为外部的 Command 必须在转交内层前返回 `ErrDraining`，并且不得触碰内层业务状态。未列出的 Command 仍转交内层，用于完成已存在的会话、释放 Visitor lease、状态快照或后续受控退出。Guard 不尝试推断这些内部 Command 是否安全。

`Init` 先保存 `ServiceContext`，再调用内层 `Init`；`Stop` 与 `Close` 原样转交内层，`Close` 后清空 Guard 保存的 Context。若内层实现 `StartupCommandDeclarer`，Guard 必须透明转发其声明；Guard 不新增 Startup Command、Timer 或 goroutine。

## 错误、指标与失败语义

除 [RFC-0270](RFC-0270-Tooling-Drain-Hot-Reload.md) 已有错误外，本切片增加：

```text
ErrInvalidGuard
ErrUnauthorized
ErrDraining
```

`ErrInvalidGuard` 表示无效 Guard 配置、外部 Command 与控制 Command 碰撞或无效 Begin payload；`ErrUnauthorized` 表示 Begin 的精确来源不匹配；`ErrDraining` 表示 Guard 已拒绝一个配置为外部入口的 Command。Client 发现无效响应或 Codec 发现无效 wire 值时继续返回既有 `ErrInvalidResponse`。Runtime、Transport 和 context 取消/超时错误保持原语义。

Core 只稳定编码自己的错误；因此目标所在节点的业务入口或同节点调用方可以用 `errors.Is(err, ErrDraining)` 进行类型化处理，而对任意远端外部 Command 的直接 Call 会按现有 Cluster 规则收到 `*gsr.RemoteError`。业务协议 adapter 应在目标节点把 `ErrDraining` 映射为其自身的可重试响应，不能把 Guard 错误加入 Core 的稳定远端错误表。`GuardClient.Begin` 和 `Status` 则通过自身的类型化响应，在跨节点时保持 `ErrUnauthorized`、`ErrInvalidGuard` 与 `ErrInvalidResponse` 的语义。

Guard 使用 `ServiceContext.Metrics()` 记录：

```text
drain_guard_begun_total
drain_guard_begin_duplicate_total
drain_guard_rejected_total
```

指标只能从 `Runtime.Inspect().Metrics` 读取。Guard 不增加 Core getter、日志 adapter、HTTP 或外部控制协议。

## 与后续 Drain 操作的关系

后续控制面的一次 Drain 操作至少需要记录以下不可伪造的阶段事实：新实例就绪、预期的旧 `ServiceSetVersion`、Directory 提交后的新版本、每一个旧实例 Begin 的结果、强 Visitor 清零或超时，以及 Stop 的结果。因为 Directory Publish 成功后响应可能丢失，调用方必须以 `Get` 和操作记录重新判定，而不是盲目回滚或把 Revision 倒退。

在一个成功的操作中，控制面先准备并验证新实例，再以 compare-and-set 发布更高版本的新 `ServiceSet`，然后让旧实例 Begin，最后在强 Visitor 释放后由拥有 Runtime 生命周期能力的节点执行 Stop。回滚也只能发布更高 Revision 的完整旧成员集合；它不能调用 Resume 重开已经 Begin 的旧实例。自动化、超时和人工恢复全部留给后续具备 `RequestID`、principal、授权和审计的 RFC。

## 验收

必须覆盖：

1. `Decorate` 拒绝 nil Service、无效 Controller、空/零值/重复 ExternalCommands、控制 Command 碰撞和 Controller 等于实际 Self。
2. Guard 透明转发生命周期和 Startup Command。
3. Begin 之前外部 Command 正常到达内层；Begin 之后同类 Command 返回 `ErrDraining` 且不改变内层状态；清单外内部 Command 仍被转交。
4. Begin 只接受精确 Controller `ServiceRef`；错误 source、节点级 caller 和错误 payload 不改变状态。
5. Begin 幂等地保留首次 `StartedAt`；Status 在前后返回满足不变量的独立快照；开始和拒绝指标正确。
6. Guard Command 的 Codec 正确组合 Visitor/fallback Codec，拒绝私有或错误 payload、畸形 JSON、尾随 JSON、类型错误、未知响应码和无效成功响应。
7. 双节点 TCP 下，Controller Service 可通过 `GuardClient.Begin` 使远端旧实例进入 Drain；Guard Client 的领域错误可类型化识别。外部 Command 的拒绝由目标节点业务入口映射，跨节点直接 Call 继续遵循 Core `RemoteError` 语义。
8. Core、Discovery 和 ServiceGroup 不导入 `tooling/drain`；Guard、Client、Codec 和被包装 Service 不创建 goroutine；`go test ./...`、`go vet ./...`、`go test -race ./...` 通过。

## 实现结论

Phase 10B 使业务组合根能够把会直接接收新请求的旧 Service 包装为显式入口闸门：协调 Service Begin 成功后，即使调用方缓存了旧 `ServiceRef`，也不能进入已列出的外部 Command。它明确不执行 ServiceGroup 切换、不等待 Visitor、不恢复旧实例，也不停止 Service；这些跨节点操作仍必须由下一阶段具有操作记录、授权和人工恢复边界的控制面完成。
