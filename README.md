# GSR - Game Service Runtime

GSR 是一个借鉴 Skynet 设计思想、使用 Go 实现的游戏 Service Runtime。它不是 Actor 框架、gRPC 微服务框架或通用 RPC 封装。

`docs/rfcs` 是设计和公开 API 的唯一事实来源，代码必须按 RFC 实现。

## 当前状态

当前最新发布标签为 `v0.3.0`，当前源码已经完成 Phase 7D Snapshot，待下一个 minor 版本发布；变更与限制见 [`CHANGELOG.md`](CHANGELOG.md)。已经实现 Core Runtime、Cluster Data Plane，以及可选的 Discovery、Monitor 和 Snapshot Tooling：

- Service、ServiceRef、Command 和私有 Registry。
- Mailbox、Scheduler 和固定执行许可池。
- Send、Call、Reply、Session 和 PendingCall。
- Timer 到 Command 的统一投递。
- Service 生命周期、超时、panic 隔离、任务追踪和基础指标。
- `Runtime.Inspect` 只读运行状态观测。
- WireEnvelope、远程 Send/Call/Reply 和跨节点调用环检测。
- `CommandContext.Source` 调用来源和按节点本地名字查询的 `Runtime.ResolveRemote`。
- TCP Transport、版本握手、受限帧、连接复用和断线通知。
- 带 AuthorityEpoch、Generation 和私有 owner 的节点租约、活动节点查询和长期 `ServiceName` 发现。
- 可组合 Discovery Codec 和类型化远程领域错误。
- 本地 Monitor Report、稳定 JSON 输出和完整 Metrics 快照枚举。
- 远程 Call 成功、失败结果指标。
- 通过 Capture Command 串行采集的版本化业务状态快照。
- 带 Revision 冲突保护的内存 Store、可组合 Cluster Codec 和组合根受限恢复。

Supervisor、远程 NodeAgent、Login/Gateway、ServiceGroup 和 Business Layer 仍在规划中，实施顺序见 [`RFC-0500`](docs/rfcs/RFC-0500-Roadmap.md)。当前工程欠账见 [`docs/TODO.md`](docs/TODO.md)。

## 本地 Runtime 示例

```bash
go test ./...
go run ./examples/local-runtime
```

预期输出：`hello`。

## 双节点 Cluster 示例

```bash
go run ./examples/cluster-runtime
```

预期输出：`hello cluster`。

当前 TCP Transport 不提供节点身份认证，只允许部署在可信内网，不能把 Cluster 端口直接暴露到公网。

基础 Cluster 默认只依赖静态节点配置和 `Runtime.ResolveRemote` 节点内名字解析，不需要 Discovery。

## 可选 Discovery 示例

```bash
go run ./examples/discovery-runtime
```

预期输出：`.config -> node-b/2`。

当前 Discovery 是单一内存权威。部署配置只提供权威节点的 `NodeID`、TCP 地址和稳定名字 `.discovery`，调用方通过 `Runtime.ResolveRemote` 获取当前动态 `ServiceRef`。Heartbeat 由部署编排调用；Discovery 不会动态修改 TCP peer。

只有调用方不应知道服务所在节点，或者需要动态迁移、节点目录和控制面时才启用 Discovery。普通业务 Service 不应仅为跨节点调用而依赖它。

GSR 信任可信内网中的集群节点，但不信任错误的程序状态。`Source`、租约 owner 和 AuthorityEpoch 用于状态约束，不是身份认证或安全令牌。

## 本地 Monitor 示例

```bash
go run ./examples/monitor-runtime
```

示例输出一行 JSON，包含本节点 Runtime 状态、Service、Mailbox、Runtime Task、PendingCall、Timer 和 Metrics。`tooling/monitor` 只消费 `Runtime.Inspect()` 的独立副本，不启动 HTTP、不创建后台任务，也不提供远程管理命令。

## Snapshot 示例

```bash
go run ./examples/snapshot-runtime
```

预期输出：`2`。

示例先通过 `snapshot.CaptureCommand` 在目标 Service 的 Mailbox 中生成 State，再由 `Manager` 保存到 `MemoryStore`。停止旧实例后，组合根加载 Snapshot、构造新 Service 并获得新的 `ServiceRef`。

Snapshot 不给运行中 Service 增加 `Snapshot()` 或 `Restore()` 旁路，也不替代数据库持久化、Wallet 账本或 Command Record。当前只提供内存 Store；数据库、对象存储、加密、压缩和自动恢复不在 Phase 7D 范围。
