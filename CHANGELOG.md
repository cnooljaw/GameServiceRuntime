# Changelog

本文记录 GSR 对外发布版本的行为变化。

## Unreleased

### Command Record and Replay

- 新增 `tooling/record`：透明 Decorator 在目标 Service 的 Handle 边界编码并发送不可变 `RecordEntry`；RecorderService 在自身 Mailbox 中按 StableKey 保存连续 sequence 的有界窗口，normal 录制失败只记录受限日志与指标，strict 模式仅供测试且不执行业务 Handler。
- 新增 typed Client、版本化 `RecordBundle`、调用方目录内原子替换的 JSONArchive，以及只创建并驱动隔离 TargetFactory 目标的 Replay；Replay 不复用或直接调用原 Runtime/ServiceRef。
- 新增可组合 Recorder Cluster Codec、Timer Command、Redactor/Codec/发送失败、环形淘汰、归档损坏和原 Runtime 隔离验收。Record 不替代 Snapshot、业务账本、生产保留或流量镜像；随机/时间确定性仍由业务 Command payload 提供。

### Manual Recovery and Compensation

- 新增 `RecoveryOperation`、`BeginRecovery`、`ResolveRecovery`、`ConfirmRecovery`、`GetRecovery` 与 `AbandonRecovery`：Recovery 必须复用一个已终态 StopOperation 的 RequestID 和完整旧 Target 集合；只有 Gateway 代表允许的 Principal 才能读取或推进它。
- 新增 NodeAgent Recovery receipt、组合根 `MapBlueprintRegistry` 与有界 `RecoveryRunner`。Runner 只按稳定 BlueprintID 创建新实例并经本地私有 Command 回报；它不发布 Directory、不重试创建，也不 Stop 已创建的实例，Close 会等待已开始创建返回。
- Confirm 前新 Ref 不进入 Directory；Confirm 后以冻结 Expected 的 CAS 保留当前成员并追加新 Ref。旧 Guard/Stopped Ref 永不重新发布；本地与双节点 TCP 测试覆盖 Gateway fencing、结果 Resolve、人工 Confirm、Runner Close 和 Race Detector。

### Controlled Node Stop Execution

- 新增 `StopOperation`、`BeginStop`、`ResolveStop` 与 `GetStop`：只有精确 Gateway ServiceRef 代表允许的 Principal 才能把同 RequestID 的 `ReadyToStop` Drain Operation 推进为受控 Stop；Coordinator 在创建和每次向 NodeAgent 投递前都强确认冻结的 Published ServiceSet。
- 新增可选成对配置的 NodeAgent Stop receipt，以及组合根持有的有界 `NodeStopRunner`。Runner 是唯一调用 `Runtime.Stop` 的 adapter；每次实际 Stop 前再次读取 Directory，结果只经本机私有 Command 回到 NodeAgent Mailbox，关闭时取消未开始工作并等待已开始 Stop 返回。
- 新增 Control Codec Stop payload、本地与双节点 TCP 验收。Directory 不匹配会使操作 Superseded；队列满、Directory 不可用和 Runner 关闭都保留可审计的 receipt/Operation 事实。此阶段不创建替代实例、不自动补偿或恢复，也不引入 Controller 或 Reconcile。

### Controlled Drain Operation

- 新增 `tooling/control` 的 `DrainCoordinatorService`、`DrainClient`、`Principal`、`RequestID`、`DrainOperation` 和有界 `DrainAudit`。只有精确 Gateway ServiceRef 代表白名单 Principal 才能 Start、Resolve、Get 或读取审计；同一 RequestID 的规范化输入稳定幂等，不同输入得到 `ErrRequestConflict`。
- Coordinator 在自身 Mailbox 串行执行 Directory CAS、被移除旧 Ref 的 Guard 与 Visitor 强 lease 刷新。Publish Reply 丢失不会自动重发，只能以显式 Resolve 的 Directory Get 确认；Guard Reply 丢失以 Status 确认；Directory 被更高版本替代时停止继续 Guard。
- 新增 Control Codec 的 Drain payload、独立快照、本地失败路径和双节点 TCP 验收。`ReadyToStop` 只记录事实，不调用 Runtime Stop、不创建实例、不做后台重试、NodeAgent 动作或自动恢复。

### Drain Guard

- 新增 `tooling/drain` 的 `Decorate`、`GuardConfig`、`GuardClient` 和 `DrainStatus`：受信任 Controller 可在目标 Service 的 Mailbox 内开始不可逆 Drain，并只拒绝显式列出的外部 Command；内部清理 Command 继续转交业务 Service。
- 新增 `BeginDrainCommand`、`GetDrainStatusCommand`、可组合 JSON Codec、来源精确 fencing、重复 Begin 稳定状态和 Guard 指标。本地与双节点 TCP 验收覆盖了状态、Codec 和远程 Controller。
- Guard 不发布 ServiceSet、不等待 Visitor、不执行 Stop 或 Resume；直接远端业务 Call 的拒绝仍遵循 Core `RemoteError`，目标节点 adapter 负责映射业务重试响应。

### Visitor lease Registry

- 新增 `tooling/drain`：`VisitorRegistryService` 在自己的 Mailbox 中持有 Target/Visitor lease，并以随机 AuthorityEpoch、单调 Generation、owner 和 expiry fence 迟到 Renew/Release。
- 新增强弱访问者、类型化 Client、Runtime Timer 驱动的过期清理和只读稳定排序查询；Client、Service 与 Codec 不创建 goroutine。
- 新增可组合 JSON Codec、双节点 TCP Acquire/Renew/List/Release 验收和 `examples/drain-runtime`；Registry 只提供事实，不执行 Drain、ServiceGroup 切换或 Stop。

### ServiceGroup

- 新增 `tooling/servicegroup`：独立 `DirectoryService` 以 compare-and-set 发布完整 ServiceSet，版本由随机 AuthorityEpoch 和组内单调 Revision 组成；Directory 重建后旧版本不能修改新权威。
- 新增 typed Client、稳定错误和可组合 JSON Codec；Refs 会去重并稳定排序，查询、发布结果、Watch 结果和通知均返回独立 Refs/Tags。
- 新增带 owner、authority、generation 和 TTL 的 Watch lease。订阅 Service 通过公共 `ServiceSetChangedCommand` 在自己的 Mailbox 内接收完整快照；Timer 只投递私有过期清理 Command。
- 新增 FNV-1a Hash、并发安全 RoundRobin 和顺序 Broadcast，以及只对调用方显式 ServiceSet 路由的 Router；Broadcast 部分失败通过 `BroadcastError` 保留目标明细，Call 拒绝多目标。
- 新增双节点 TCP Publish/Get/Watch/Route 验收和 `examples/servicegroup-runtime`。Core Runtime 与 Discovery 均未增加 ServiceGroup 概念。

### Cluster Control Plane

- 新增 `tooling/control`：NodeAgent 在进入 `Running` 后通过 Startup Command 注册自己的 Discovery lease，再由 Timer Command 自动续租；租约失效会重新注册，Stop 最多注销当前 lease 一次。
- 新增 `ClusterObserverService` 与 typed Client，冻结静态 `NodeConfig`，缓存并独立返回 `Observed State`；节点刷新通过受限远程 Call 获取 NodeAgent 的 Monitor report。
- 新增可组合 Control Plane Codec、双节点 TCP 验收和 `examples/control-runtime`。控制面仍只读，不提供外部 Admin API、认证、授权、审计、Controller、Desired State 或修改型运维。

### Core Runtime

- 新增可选 `StartupCommandDeclarer`：Runtime 在 Service 进入 `Running` 后，以自身为 Source 投递声明的启动 Command；长期启动、注册和重试不再被迫放入 `Init` 或裸 goroutine。

### 客户端入口

- 新增 `tooling/entry`：内存 `SessionRegistry` 在一个私有同步边界内保存 secret、ticket、严格递增 proof Sequence 和 Gateway 连接绑定。
- 新增 Mailbox 串行的 `LoginService` 和 typed `LoginClient`；首版 `SingleSession` 以 `AccountID + Server` 签发单调 Generation，新票据原子撤销旧 ticket 并返回旧连接 ID。
- Gateway 固定 `GSR-Gateway-Proof-v1` HMAC-SHA-256 proof 与带上限的 `AUTH` TCP 行格式；篡改、过期、重放或无效 proof 不会触发业务 mapper 或 Runtime Command。
- 新增由组合根持有的 TCP Login/Gateway Adapter。它们跟踪 listener 和连接任务，`Close(ctx)` 会等待任务真实返回；Adapter 通过窄 Handshake、Registry、Issuer、ConnectionCloser、ProtocolMapper 和 Runtime dispatch seam 组合。
- `ProtocolMapper` 仅接收 `SessionIdentity` 和业务包；Call 结果必须由 `CallResponseMapper` 编码，Gateway 不解释业务协议或持有业务状态。
- 新增 TCP 端到端示例，以及 proof 并发重放、迟到 Unbind、SingleSession 旧连接关闭、Call 响应和 Adapter 关闭竞争测试。
- LoginService 重建时从 SessionRegistry 按 `AccountID + Server` 恢复当前 ticket，继续递增 Generation 并撤销旧 ticket；不再因 Service 重建或切换 PlayerID 留下旧已认证连接。
- Login/Gateway Adapter 新增连接上限与握手、认证/业务包空闲超时；Gateway 另有每连接固定窗口包率限制。撤销提交后的旧连接在 mapper 前被 fencing，拒绝路径不会进入 `ProtocolMapper` 或 Runtime。

### Supervisor

- 新增 `tooling/supervisor`，Decorator 在 Handler panic 时发送不含 panic value、堆栈或业务状态的不可变失败通知，并重新 panic 交给 Core 隔离。
- Supervisor Service 通过 Command Source、稳定 `ServiceKey`、`ServiceRef` 和 Generation 校验通知；重复或迟到通知不会触发第二次决策。
- 新增 `RestartNever`、`DestroyOnFailure` 和 `RestartOnFailure`，分别限制单次故障尝试、窗口内成功重启和指数退避。
- 新增组合根持有的有界 Runner；固定 worker 执行退避、Snapshot/Store I/O、创建和结果重试，Service Handler 不阻塞也不创建 goroutine。
- 新增两阶段 RuntimeLauncher：Prepare 未发布实例，Supervisor 登记新 Ref/Generation 后 Commit 长期绑定；失败、迟到或部分 Prepare 结果统一 Abort。
- Publish 与 committed 之间再次 panic 时会 fencing 已对外运行的 Generation，迟到结果不能覆盖后续恢复。
- 新增 typed Client、八类 Core Metrics、Snapshot 端到端恢复示例，以及 Snapshot 缺失、连续创建失败、发布歧义、Abort 和关闭真实返回测试。

### Snapshot

- 新增 `tooling/snapshot`，通过稳定 `CaptureCommand` 在目标 Service 的串行 Handler 中生成版本化 State。
- `Manager` 使用窄 `CommandCaller` 和 `Store` 接口；Call 完成后才执行 Store IO，并核对目标 Service 返回的 owner Key。
- Snapshot 使用由状态 owner 持有的稳定业务 Key、Schema、Version 和单调 Revision；Key、Schema 必须是合法 UTF-8，默认 payload 上限为 1 MiB。
- `MemoryStore.Save` 返回原子操作后真正保留的独立 canonical Snapshot；它拒绝旧 Revision 和同 Revision 的不同内容，零值可直接使用。
- nil Payload 被稳定拒绝；空状态必须使用非 nil 空切片。
- 新增可组合 JSON Cluster Codec，请求和响应都携带稳定 Key，使用稳定 `snake_case` 字段、允许未知字段，并拒绝非法 UTF-8、缺失字段和尾随 JSON。
- 新增本地恢复和双节点 Capture 测试，以及组合根受限恢复示例。

### 限制

- Directory 当前是单一内存权威，不提供复制、选主、持久化或自动 Service 注册；通知是 best-effort 完整快照，不是可靠事件日志。
- Hash 第一版使用普通取模，不是一致性哈希；成员变化可能重新映射大量 key。Router 不维护后台缓存，订阅 Service 自己决定快照切换时点。
- ServiceGroup 不包含 Desired State、Controller、Reconcile、健康检查、自动扩缩容、Drain 或回滚编排。
- Visitor lease Registry 不拒绝新流量、不等待访问者、不迁移状态，也不编排 Service 停止；这些能力仍需后续 Drain 契约。
- 不修改 Core `Service` 接口，不支持对运行中实例原地 Restore。
- 当前只提供内存 Snapshot Store，不提供数据库、对象存储、压缩、加密或增量快照。
- Supervisor 只处理同节点 Handler panic，不提供跨进程恢复、持久故障队列、完整 Supervisor Tree 或远程 Codec。
- 失败通知投递是单次非阻塞 Send；投递失败有指标和日志，但不保证自动恢复。
- 长期名字通过调用方提供的 `BindingPublisher` 发布；Supervisor 不强制依赖 Discovery。
- 客户端入口当前只提供内存 Registry、单进程 TCP 行协议和测试 Handshake；不提供生产账号体系、TLS/密钥协商、WebSocket/HTTP、持久化或跨节点会话。

## v0.3.0 - 2026-07-22

增加本地 Monitor Tooling，把 `Runtime.Inspect()` 转换为稳定报告和 JSON。Monitor 不进入 Core Runtime，不启动网络监听或后台任务。

### Core Runtime

- `MetricsSnapshot` 新增 `Counters()`、`Gauges()` 和 `Durations()`，每次返回可独立修改的完整指标 map。
- 远程 Call 在统一返回路径记录 `remote_calls_succeeded_total` 或 `remote_calls_failed_total`；本地 Call 不计入。

### Monitor

- 新增独立 `tooling/monitor` 包，通过窄 `Inspector` 接口读取本地 Runtime Inspection。
- `Capture()` 输出 Runtime、Service、Mailbox、Task、PendingCall、Timer 和 Metrics 报告。
- Runtime、Service 和 Task 状态转换为稳定字符串，未知枚举输出 `unknown`。
- `WriteJSON(io.Writer)` 使用稳定 `snake_case` 字段，空切片和 map 分别输出 `[]` 和 `{}`。
- JSON writer 错误原样返回，Monitor 不关闭调用方 writer。
- 新增本地 Monitor JSON 示例。

### 限制

- 当前只提供本地即时快照，不提供历史存储、聚合、告警或采样。
- 不提供 HTTP、CLI、Prometheus/OpenMetrics exporter、远程 NodeAgent 或管理命令。
- Inspection 保持最终一致语义，不承诺各子系统来自同一原子时刻。
- Cluster 连接状态仍由后续 Transport 观测 adapter 提供。

### 兼容性

相对 `v0.2.0` 仅增加 Core 只读指标枚举方法和独立 Tooling API，没有修改既有 Service、Command、Cluster 或 Discovery 调用语义。

## v0.2.0 - 2026-07-22

增加最小 Runtime Tooling Discovery，并补齐它依赖的通用 Core 调用来源和节点级启动入口。

基础 Cluster 仍默认使用静态节点配置和 `Runtime.ResolveRemote` 节点内名字解析，不依赖 Discovery。Discovery 只作为全局位置解耦、动态迁移和控制面的可选 Tooling。

### Core Runtime

- `CommandContext` 新增 `Source()`，Runtime 自身发起的 Command 使用 `{Node: localNode, ID: 0}`，Service 调用保留真实 `ServiceRef`。
- 新增 `Runtime.ResolveRemote(ctx, node, name)`，对应 Skynet `cluster.query` 的启动职责。
- `ServiceID(0)` 仅处理 Core 私有名字查询 Call；名字请求和 ServiceID 响应不经过业务 `ClusterCodec`。

### Discovery

- 新增独立 `tooling/discovery` 包和类型化 `Client`。
- 节点注册返回带 AuthorityEpoch 和 Generation 的租约；authority 重启后旧租约不会重新有效。
- 注册来源必须匹配节点，Heartbeat、注销和名字写操作必须匹配私有租约 owner。
- 节点过期、注销或同 NodeID 重注册时，清理其拥有的长期 `ServiceName`。
- 节点列表按 `NodeID` 稳定排序，返回结果与内部状态隔离。
- 同一租约可以替换长期名字的 `ServiceRef`；其它活动租约不能抢占名字。
- 新增可组合 JSON Codec，非 Discovery Command 可以委托给 fallback。
- Discovery JSON 使用稳定 `snake_case` 线字段，并允许解码端忽略新增未知字段。
- Discovery 领域错误跨节点后仍可通过 `errors.Is` 判断。
- Timer、Runtime 等基础设施错误直接由 Handler 返回，不降级为 Discovery 响应错误。
- Client 校验成功 Lease、NodeRecord、ServiceRef 和节点列表的结构不变量。
- 新增双节点 TCP Discovery 示例和本地/远程验收测试。

### 限制

- 第一版是单一内存权威，不提供复制、选主、持久化或 Gossip。
- Discovery 的节点地址不会自动更新 `ClusterTransport` peer。
- Discovery 所在节点的 `NodeID` 和地址仍由部署配置提供；动态 `ServiceRef` 通过 `.discovery` 查询。
- Heartbeat 由部署编排调用；自动 NodeAgent 留到后续阶段。
- Discovery Command 不提供身份认证或授权，只允许部署在可信集群网络；Source、owner 和租约只约束程序状态。
- Desired/Observed State、ServiceGroup、路由策略和管理命令尚未实现。

### 兼容性

相对 `v0.1.0`，Core 新增 `CommandContext.Source` 和 `Runtime.ResolveRemote`；Discovery API 新增 `AuthorityEpoch` 和 `ErrLeaseOwnerMismatch`。本次修正直接纳入 `v0.2.0` 基线。

## v0.1.0 - 2026-07-17

首个可复验的 Core Runtime 版本。公开语义以已接受的 `RFC-0100` 至 `RFC-0192` 为准。

### Core Runtime

- Service、ServiceRef、ServiceName 和私有只读 Command 集。
- 有界 Mailbox、ReadyQueue、固定执行许可池和单 Service 串行 Handler。
- Send、Call、Reply、Session、PendingCall 和同步调用环检测。
- Timer 到 Command 的统一投递和投递失败指标。
- Init、Handle、Stop、Close 的 panic 隔离、超时、任务追踪和资源收敛。
- `Runtime.Inspect` 只读运行状态、Service、Mailbox、PendingCall、Timer、Task 和 Metrics 视图。

### Cluster Data Plane

- 本地和远程目标共用 Send/Call/Reply API。
- WireEnvelope、CallPath、Reply 身份校验和稳定 Runtime 错误传播。
- TCP 版本握手、受限长度帧、连接复用、按节点并行建连和断线通知。
- 本地与双节点端到端示例。

### 工程门禁

- `go test ./...`、`go vet ./...` 和 `go test -race ./...` 持续集成检查。
- Service 禁止直接创建 goroutine 的 AST 门禁。
- Send、Call/Reply、多 Service 和 Timer 完整路径 Benchmark。
- Runtime 重复创建关闭后的 PendingCall、Timer、Task 和 goroutine 收敛测试。

### 限制

- 当前 TCP Transport 不提供节点身份认证、完整性保护或加密，只允许部署在可信内网。
- Peer 地址为静态配置，不包含 Discovery、自动重连或动态地址更新。
- Runtime 只定义 `ClusterCodec` 边界；应用需要提供与 CommandID 匹配的稳定 Payload Codec。
- Snapshot、Supervisor、Monitor 适配器、Login/Gateway、ServiceGroup、Drain、Record/Replay 和 Business Layer 尚未实现。

### 兼容性

`v0.1.0` 是首个 major 0 基线。补丁版本应保持已接受 RFC 的兼容行为；后续不兼容调整必须先更新 RFC、提供迁移说明，并在新的 minor 版本发布。
