# GSR Phase 7B 最小 Discovery 实施计划

> 状态：已完成（2026-07-21）

**目标：** 在不修改 Core Runtime 和 `ClusterTransport` 的前提下，实现一个可本地或跨节点调用的 `DiscoveryService`，提供带租约的活动节点发现和长期 `ServiceName` 解析；完成 API Review，并发布 `v0.2.0`。

**范围：** `RFC-0200` 和 `RFC-0500` 的 Phase 7B。实现位于 `tooling/discovery`，只依赖 Go 标准库和 `runtime` 包。

## 设计裁决

### 分层

```text
Business / Tooling caller
  -> discovery.Client
  -> Command + Call
  -> DiscoveryService
  -> private node/name maps
```

- `runtime` 不导入 `tooling/discovery`。
- Discovery 是普通系统 Service，状态只在 `Handle` 中通过 Command 修改。
- Service 不创建 goroutine；租约清理由 Runtime Timer 投递内部 Command。
- 调用方不能读取或持有 Discovery 内部 map。

### 启动与权威来源

第一版采用单一权威 `DiscoveryService`，不实现复制、选主、持久化或 Gossip。

Discovery Command 不提供节点身份认证或授权，只允许运行在 `RFC-0191` 定义的可信集群网络；管理面认证留到 Phase 8。

远程调用方必须从部署配置获得：

- Discovery 所在 `NodeID` 和 TCP 地址。
- Discovery 的 `ServiceRef`。

这是启动根，不允许 Discovery 反过来发现自己。`DefaultServiceName = ".discovery"` 只用于所在 Runtime 的本地 `Resolve`，不能替代远程启动配置。

### 节点租约

```go
type NodeLease struct {
    Node       gsr.NodeID
    Generation uint64
    ExpiresAt  time.Time
}

type NodeRecord struct {
    ID         gsr.NodeID
    Address    string
    Generation uint64
    LastSeen   time.Time
    ExpiresAt  time.Time
}
```

- `RegisterNode` 为该 `NodeID` 创建新 Generation。
- 同一 `NodeID` 再注册时，旧租约立即失效，其名下 ServiceName 一并删除。
- `Heartbeat` 只有在 NodeID 和 Generation 都匹配时才能续期。
- `NodeLease` 是防止旧进程覆盖新注册的版本凭据，不是认证或安全令牌。
- 查询只返回未过期节点；`ListNodes` 按 `NodeID` 排序并返回独立副本。
- Discovery 记录地址事实，但 Phase 7B 不据此修改 TCP peer；动态 reload 属于 Phase 8 Control Plane。
- Phase 7B 由部署编排代码调用 `RegisterNode` 和 `Heartbeat`；自动续租的 NodeAgent 留到 Phase 8。

### 长期 ServiceName

- 注册必须携带有效 `NodeLease`，且 `ServiceRef.Node == NodeLease.Node`。
- 同一租约可以把同名 Service 更新到新 `ServiceRef`，用于长期 Service 重建。
- 其它活动租约注册同名 Service 返回 `ErrNameConflict`。
- 注销必须同时匹配租约、名称和当前 `ServiceRef`，避免迟到注销删除新实例。
- 节点租约过期、注销或被新 Generation 替换时，删除它拥有的所有名字。
- Battle、单局 Room 等临时 Service 不注册长期名字。
- `Runtime.Resolve` 继续只解析本地 `ServiceSpec.Name`；全局解析由 `discovery.Client.ResolveName` 完成。

### 公开 API 基线

```go
const DefaultServiceName gsr.ServiceName = ".discovery"

type Config struct {
    LeaseTTL      time.Duration
    SweepInterval time.Duration
}

type CommandCaller interface {
    Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error)
}

func NewService(Config) (gsr.Service, error)
func NewClient(CommandCaller, gsr.ServiceRef) (*Client, error)
func NewCodec(fallback gsr.ClusterCodec) gsr.ClusterCodec

func (c *Client) RegisterNode(context.Context, gsr.NodeID, string) (NodeLease, error)
func (c *Client) Heartbeat(context.Context, NodeLease) (NodeLease, error)
func (c *Client) UnregisterNode(context.Context, NodeLease) error
func (c *Client) GetNode(context.Context, gsr.NodeID) (NodeRecord, error)
func (c *Client) ListNodes(context.Context) ([]NodeRecord, error)
func (c *Client) RegisterName(context.Context, NodeLease, gsr.ServiceName, gsr.ServiceRef) error
func (c *Client) UnregisterName(context.Context, NodeLease, gsr.ServiceName, gsr.ServiceRef) error
func (c *Client) ResolveName(context.Context, gsr.ServiceName) (gsr.ServiceRef, error)
```

公开错误至少包括：

```go
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

Discovery 领域错误通过 Command 的类型化 Reply 返回，由 `Client` 还原为稳定错误；不能要求 Core Runtime 认识 Tooling 错误。

### CommandID

Discovery 的稳定命令编号集中在包内 `commands.go`：

```text
0x02000101 RegisterNode
0x02000102 Heartbeat
0x02000103 UnregisterNode
0x02000104 GetNode
0x02000105 ListNodes
0x02000106 RegisterName
0x02000107 UnregisterName
0x02000108 ResolveName
0x020001ff SweepExpired（仅 Service 内部 Timer 使用）
```

调用方不直接拼装这些 Command，而是使用 `Client`。编号稳定是为了 Cluster Codec 和录制诊断，不新增动态 Command Registry。

### Codec

`NewCodec(fallback)` 使用标准库 `encoding/json` 编解码 Discovery 的请求和 Reply：

- Discovery Command 由本 Codec 处理。
- 其它 Command 委托给 `fallback`。
- 无 fallback 时遇到其它 Command 返回 `ErrUnsupportedCommand`。
- 内部 `SweepExpired` 不允许经过远程 Codec。
- 线格式使用固定 `snake_case` 字段和私有线类型，不依赖 Core Go 结构体的字段名。
- Decoder 忽略未知字段以支持只增加字段的滚动升级，但拒绝格式错误和尾随第二个 JSON 值。
- Codec 只处理 payload，不接触 TCP、连接或 `WireEnvelope`。

## 非目标

- 不修改 Core `Runtime`、Registry、`ServiceRef` 或 `ClusterTransport` API。
- 不实现 Desired State、Observed State、NodeAgent、管理命令或 TCP peer reload。
- 不实现自动 Heartbeat Service，也不在 Service 或示例中使用裸 goroutine 维持租约。
- 不实现 ServiceGroup、版本化 ServiceSet、负载均衡、Hash、RoundRobin 或 Broadcast。
- 不实现 Gossip、共识、高可用、多副本同步或持久化。
- 不自动扫描 `Runtime.Inspect` 并注册全部 Service。
- 不把临时 Battle/Room 注册到 Discovery。
- 不提供 Consul、Nacos、etcd 或 DNS 适配器。

## Task 1：冻结 RFC 与阶段边界

**文件：**

- 修改 `docs/rfcs/RFC-0200-Tooling-Discovery.md`
- 修改 `docs/rfcs/RFC-0250-Tooling-Cluster-Control-Plane.md`
- 修改 `docs/rfcs/RFC-0500-Roadmap.md`
- 修改 `docs/TODO.md`
- 修改本计划

**步骤：**

1. 将 `RFC-0200` 范围改为 Runtime Tooling，写入本计划的租约、Client、名字所有权、启动根和静态 Transport 边界。
2. 从 `RFC-0200` 第一版移除 Desired/Observed State、ServiceGroup 和管理命令；分别链接 `RFC-0250`、`RFC-0260`。
3. 明确 Phase 7B 只保存活动节点事实，不表达“配置存在但未连接”；该状态留给 Phase 8 Control Plane。
4. `RFC-0200` 保持“草案”直到实现和 API Review 完成。
5. 将路线图 Phase 7B 标记为执行中，更新 `docs/TODO.md` 当前阶段和日期。
6. 运行：

   ```bash
   rg -n 'CmdUpdateNodeDesiredState|CmdRegisterServiceGroup|CmdWatchServiceGroup|Configured but disconnected' docs/rfcs/RFC-0200-Tooling-Discovery.md
   git diff --check
   ```

   预期：第一条无输出；Markdown diff 无空白错误。

7. 提交：`docs(discovery): 冻结最小节点与名字发现边界`。

## Task 2：实现节点租约

**文件：**

- 新增 `tooling/discovery/types.go`
- 新增 `tooling/discovery/errors.go`
- 新增 `tooling/discovery/commands.go`
- 新增 `tooling/discovery/client.go`
- 新增 `tooling/discovery/service.go`
- 新增 `tooling/discovery/node_test.go`

**测试接缝：** 通过真实 `Runtime` 创建 `DiscoveryService`，再通过公开 `Client` 调用；测试不读取 Service 私有 map。

**先写失败测试：**

1. `TestRegisterNodeReturnsLeaseAndRecord`：注册返回非零 Generation，记录包含地址、LastSeen 和 ExpiresAt。
2. `TestHeartbeatRenewsMatchingLease`：匹配租约续期，旧快照不变化。
3. `TestRegisterNodeInvalidatesPreviousGeneration`：同 NodeID 再注册后，旧租约 Heartbeat 返回 `ErrLeaseExpired`。
4. `TestListNodesReturnsSortedCopies`：乱序注册后按 NodeID 返回；修改结果不影响后续查询。
5. `TestExpiredNodeIsNotDiscoverable`：推进 Runtime 时间源后，查询返回 `ErrNodeNotFound`。
6. `TestUnregisterNodeRequiresCurrentLease`：只有当前 Generation 能注销。
7. `TestDiscoverySweepUsesTimerCommand`：注册后存在一个 Timer；注销最后节点后已排定 Timer 到期但不再重排，停止 Service 时 Runtime 取消目标 Timer。
8. `TestDiscoveryRejectsInvalidNodeInput`：空 NodeID、空地址和零 Generation 被拒绝。

运行并确认测试因包/API 尚不存在而失败：

```bash
go test ./tooling/discovery -run '^Test(RegisterNode|Heartbeat|ListNodes|ExpiredNode|UnregisterNode|DiscoverySweep|DiscoveryRejects)' -count=1
```

**最小实现：**

1. `NewService` 校验配置并应用明确默认值；Service 私有持有节点 map、Generation 计数和 Timer 状态。
2. `Init` 只保存 `ServiceContext`，不 Call、不创建 goroutine，也不直接启动 Timer。
3. 第一个注册 Command 在 Handler 内通过 `ServiceContext.After` 安排 `SweepExpired`；Sweep Command 清理后按需重新安排。
4. 每个公开 Command 处理前按 `ServiceContext.Now()` 清理过期租约，避免依赖 Timer 调度精度保证查询正确性。
5. `Client` 做输入校验、类型化 Reply 检查和领域错误还原。
6. `ServiceContext` 不增加 `Cancel`。最后节点注销后，已排定 Sweep 到期执行一次并停止重排；目标 Service 关闭时由 Runtime 取消绑定 Timer。
7. `Stop`、`Close` 只清理私有容器。
8. 运行：

   ```bash
   go test ./tooling/discovery -run '^Test(RegisterNode|Heartbeat|ListNodes|ExpiredNode|UnregisterNode|DiscoverySweep|DiscoveryRejects)' -count=50
   go test -race ./tooling/discovery -run '^Test(RegisterNode|Heartbeat|ListNodes|ExpiredNode|UnregisterNode|DiscoverySweep|DiscoveryRejects)' -count=20
   go test ./runtime -run '^TestProjectServicesDoNotStartGoroutines$' -count=1
   ```

9. 提交：`feat(discovery): 实现节点租约与活动节点查询`。

## Task 3：实现长期 ServiceName 发现

**文件：**

- 修改 `tooling/discovery/types.go`
- 修改 `tooling/discovery/errors.go`
- 修改 `tooling/discovery/commands.go`
- 修改 `tooling/discovery/client.go`
- 修改 `tooling/discovery/service.go`
- 新增 `tooling/discovery/name_test.go`

**先写失败测试：**

1. `TestRegisterAndResolveName`：有效租约注册后解析到完整 `ServiceRef`。
2. `TestSameLeaseCanReplaceNameRef`：同租约可把长期名字切换到同节点新 ServiceID。
3. `TestOtherLeaseCannotReplaceName`：其它活动租约注册同名返回 `ErrNameConflict`。
4. `TestUnregisterNameRequiresExactOwnerAndRef`：迟到注销不能删除新绑定。
5. `TestNodeExpiryRemovesOwnedNames`：租约过期后名字返回 `ErrNameNotFound`。
6. `TestNodeReregisterRemovesPreviousGenerationNames`：同 NodeID 新 Generation 不继承旧名字。
7. `TestRegisterNameRequiresMatchingNode`：租约节点和 `ServiceRef.Node` 不一致时返回 `ErrInvalidName`。
8. `TestResolveNameDoesNotExposeMutableState`：所有返回值都是值副本。

运行并确认测试失败：

```bash
go test ./tooling/discovery -run '^Test(RegisterAndResolveName|SameLease|OtherLease|UnregisterName|NodeExpiryRemoves|NodeReregisterRemoves|RegisterNameRequires|ResolveNameDoes)' -count=1
```

**最小实现：**

1. 名字表只保存 `ServiceName -> {ServiceRef, NodeID, Generation}`。
2. 增加按租约反向索引，使节点失效时一次删除其拥有的名字。
3. 注册、替换、注销和解析全部在 Discovery Service 串行 Handler 中执行，不增加锁。
4. 领域冲突通过类型化 Reply 返回；程序错误或未知 payload 才从 Handler 返回 error。
5. 运行：

   ```bash
   go test ./tooling/discovery -run '^Test.*Name' -count=50
   go test -race ./tooling/discovery -run '^Test.*Name' -count=20
   ```

6. 提交：`feat(discovery): 实现长期服务名注册与解析`。

## Task 4：增加可组合 Codec 与远程验收

**文件：**

- 新增 `tooling/discovery/codec.go`
- 新增 `tooling/discovery/codec_test.go`
- 新增 `tooling/discovery/remote_test.go`
- 新增 `examples/discovery-runtime/main.go`

**先写失败测试：**

1. `TestCodecRoundTripsDiscoveryRequestsAndReplies`：每个公开 Discovery Command 的请求和成功 Reply 保持具体 Go 类型。
2. `TestCodecDelegatesUnknownCommand`：未知 Command 委托 fallback。
3. `TestCodecRejectsUnknownCommandWithoutFallback`：无 fallback 返回 `ErrUnsupportedCommand`。
4. `TestCodecRejectsInternalSweepCommand`：内部 Timer Command 不允许远程编码。
5. `TestRemoteDiscoveryRegisterHeartbeatAndResolveName`：两个 TCP Runtime 完成远程节点注册、续租、名字注册和解析。
6. `TestRemoteDiscoveryPreservesDomainErrors`：远程名字冲突仍可 `errors.Is(err, ErrNameConflict)`，不退化为 Core `RemoteError`。

运行并确认测试失败：

```bash
go test ./tooling/discovery -run '^Test(Codec|RemoteDiscovery)' -count=1
```

**最小实现：**

1. 使用 `encoding/json`，按稳定 CommandID 和 response 标记选择具体请求/Reply 类型。
2. Discovery Reply 内携带领域错误码；Client 映射为包内稳定错误。
3. fallback 只处理非 Discovery Command，不形成第二套 Transport。
4. 示例启动两个 TCP Runtime：Discovery 节点注册长期 Service，另一节点远程解析并打印 `ServiceRef`。
5. 运行：

   ```bash
   go test ./tooling/discovery -run '^Test(Codec|RemoteDiscovery)' -count=30
   go test -race ./tooling/discovery -run '^Test(Codec|RemoteDiscovery)' -count=10
   go run ./examples/discovery-runtime
   ```

6. 提交：`feat(discovery): 增加跨节点编解码与端到端示例`。

## Task 5：文档、基准与 API Review

**文件：**

- 修改 `README.md`
- 修改 `docs/GSR-Book/03-第三篇-Cluster/03-Discovery.md`
- 修改 `docs/rfcs/RFC-0200-Tooling-Discovery.md`
- 修改 `docs/rfcs/RFC-0500-Roadmap.md`
- 修改 `docs/TODO.md`
- 修改 `CHANGELOG.md`
- 修改本计划
- 新增 `tooling/discovery/benchmark_test.go`
- 根据 Review 结论修改 `tooling/discovery` 代码和测试

**步骤：**

1. 增加本地 `ResolveName` 和 10,000 名字清理基准，只建立基线，不预先优化 map 或引入缓存。
2. README 增加 Discovery 示例和当前限制；GSR Book 说明启动根、租约、名字所有权和静态 Transport 边界。
3. 执行双轴 Review：

   - RFC 轴：API、错误、租约、名字清理、稳定排序、远程语义是否符合 `RFC-0200`。
   - 分层轴：Core 是否零修改，Tooling 是否只依赖 Core。
   - 状态轴：所有 Discovery 状态是否只在 Handler 中修改，Stop/Close 是否只清理。
   - 并发轴：Service 是否无 goroutine，Timer 是否只投递 Command。
   - 失效轴：过期、重复注册、迟到心跳、迟到注销和节点重启是否安全。
   - Codec 轴：类型、fallback、领域错误和未知 Command 是否明确。
   - API 轴：Go doc、零值、返回副本和未来兼容性是否清晰。

4. P1/P2 清零后，将 `RFC-0200` 状态改为“已接受”，路线图将 Phase 7B 标记完成并把当前阶段切到 Phase 7C。
5. `CHANGELOG.md` 增加 `v0.2.0` 的 Discovery 能力和限制。
6. 运行完整门禁：

   ```bash
   go test ./... -count=1
   go vet ./...
   go test -race ./... -count=1
   go test ./tooling/discovery -count=100
   go test ./tooling/discovery -run '^$' -bench . -benchmem -benchtime=1000x -count=5
   go run ./examples/local-runtime
   go run ./examples/cluster-runtime
   go run ./examples/discovery-runtime
   git diff --check
   ```

7. 提交：`docs(discovery): 完成最小 Discovery 验收`。

### Task 5 验收结果

- 双轴 Review：0 个 P1；发现并修复 2 个 P2 Codec 兼容性问题。
- P2-1：JSON 曾隐式依赖 Go 字段名，包含嵌套 `ServiceRef`；已改为稳定 `snake_case` 字段和私有线类型，并增加精确线格式测试。
- P2-2：Decoder 曾拒绝未知字段，不利于滚动升级；已改为忽略未知字段，同时保留格式错误和尾随 JSON 拒绝测试。
- 接受的剩余风险：Timer 投递失败时后台 Sweep 不保证自动重启，但每个后续 Command 都会同步清理；查询正确性不依赖 Timer，失败由 Runtime Metrics 记录。
- 分层检查：Core Runtime 无 Discovery 依赖或 API 修改；Discovery 只依赖标准库和 `runtime`。
- 并发检查：AST 门禁通过，Discovery Service 无裸 goroutine，Sweep 只由 Runtime Timer 投递 Command。
- 完整门禁：`go test ./...`、`go vet ./...`、`go test -race ./...`、Discovery 100 次重复测试全部通过。
- 示例输出：`hello`、`hello cluster`、`.config -> node-b/2`。
- Apple M2 基准（1000 次，5 轮）：`ResolveName` 为 3.69-5.60 微秒、744 B、11 alloc；清理 10,000 个名字为 0.498-0.511 毫秒，计时区间 0 B、0 alloc。
- Benchmark 只作为 `v0.2.0` 回归基线，不构成性能承诺。

## Task 6：发布 v0.2.0

1. 确认全量测试、vet、race、示例和 Benchmark 结果已写回本计划。
2. 确认工作区干净，Review 无未处理 P1/P2。
3. 创建提交：`docs(release): 发布 Discovery v0.2.0`。
4. 创建本地附注标签：

   ```bash
   git tag -a v0.2.0 -m 'GSR Runtime Tooling Discovery v0.2.0'
   git show --stat --oneline v0.2.0
   ```

5. 本计划不执行 `git push`。

### Task 6 发布结果

- Task 1 至 Task 5 均已按独立垂直切片提交。
- 发布前工作区干净，Review 无未处理 P1/P2，完整门禁结果见 Task 5。
- `v0.2.0` 创建为指向发布提交的本地附注标签。
- 未执行 `git push`。

## 完成标准

- `tooling/discovery` 是独立 Tooling 包，Core Runtime 代码和公开 API 不变。
- 节点注册、Heartbeat、过期、注销和同 NodeID 重启有稳定租约语义。
- 长期 ServiceName 绑定受租约所有权保护，并随租约失效清理。
- 本地与远程调用只经过 `Client -> Command -> DiscoveryService`。
- Discovery 领域错误在远程调用后仍可使用 `errors.Is` 判断。
- Service 不创建 goroutine；过期清理由 Timer Command 驱动。
- ServiceGroup、Control Plane、Gossip 和动态 TCP peer 更新没有进入 Phase 7B。
- RFC、教程、示例、测试、Race、Benchmark 和发布记录一致。
- `v0.2.0` 指向干净、可复验且未推送的提交。
