# Changelog

本文记录 GSR 对外发布版本的行为变化。

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
