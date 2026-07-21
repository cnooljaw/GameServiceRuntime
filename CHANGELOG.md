# Changelog

本文记录 GSR 对外发布版本的行为变化。

## v0.2.0 - 2026-07-21

在不修改 Core Runtime API 的前提下增加最小 Runtime Tooling Discovery。

### Discovery

- 新增独立 `tooling/discovery` 包和类型化 `Client`。
- 节点注册返回带 Generation 的租约；Heartbeat 只能续期当前 Generation。
- 节点过期、注销或同 NodeID 重注册时，清理其拥有的长期 `ServiceName`。
- 节点列表按 `NodeID` 稳定排序，返回结果与内部状态隔离。
- 同一租约可以替换长期名字的 `ServiceRef`；其它活动租约不能抢占名字。
- 新增可组合 JSON Codec，非 Discovery Command 可以委托给 fallback。
- Discovery JSON 使用稳定 `snake_case` 线字段，并允许解码端忽略新增未知字段。
- Discovery 领域错误跨节点后仍可通过 `errors.Is` 判断。
- 新增双节点 TCP Discovery 示例和本地/远程验收测试。

### 限制

- 第一版是单一内存权威，不提供复制、选主、持久化或 Gossip。
- Discovery 的节点地址不会自动更新 `ClusterTransport` peer。
- Discovery `ServiceRef` 和所在节点地址必须由部署配置提供。
- Heartbeat 由部署编排调用；自动 NodeAgent 留到后续阶段。
- Discovery Command 不提供身份认证或授权，只允许部署在可信集群网络。
- Desired/Observed State、ServiceGroup、路由策略和管理命令尚未实现。

### 兼容性

Core Runtime 和 Cluster Data Plane 公开 API 相对 `v0.1.0` 保持不变。新 API 全部位于 `tooling/discovery`。

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
