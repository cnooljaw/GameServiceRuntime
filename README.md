# GSR - Game Service Runtime

GSR 是一个借鉴 Skynet 设计思想、使用 Go 实现的游戏 Service Runtime。它不是 Actor 框架、gRPC 微服务框架或通用 RPC 封装。

`docs/rfcs` 是设计和公开 API 的唯一事实来源，代码必须按 RFC 实现。

## 当前状态

已经实现 Core Runtime 和 Cluster Data Plane：

- Service、ServiceRef、Command 和私有 Registry。
- Mailbox、Scheduler 和固定执行许可池。
- Send、Call、Reply、Session 和 PendingCall。
- Timer 到 Command 的统一投递。
- Service 生命周期、超时、panic 隔离、任务追踪和基础指标。
- `Runtime.Inspect` 只读运行状态观测。
- WireEnvelope、远程 Send/Call/Reply 和跨节点调用环检测。
- TCP Transport、版本握手、受限帧、连接复用和断线通知。

Discovery、Snapshot、Supervisor、Monitor 适配器、Login/Gateway 和 Business Layer 仍在规划中，实施顺序见 [`RFC-0500`](docs/rfcs/RFC-0500-Roadmap.md)。当前工程欠账见 [`docs/TODO.md`](docs/TODO.md)。

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
