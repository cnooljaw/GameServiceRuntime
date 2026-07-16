# GSR - Game Service Runtime

GSR 是一个借鉴 Skynet 设计思想、使用 Go 实现的游戏 Service Runtime。它不是 Actor 框架、gRPC 微服务框架或通用 RPC 封装。

`docs/rfcs` 是设计和公开 API 的唯一事实来源，代码必须按 RFC 实现。

## 当前状态

已经实现首个单节点 Core Foundation 里程碑：

- Service、ServiceRef、Command 和私有 Registry。
- Mailbox、Scheduler 和固定执行许可池。
- Send、Call、Reply、Session 和 PendingCall。
- Timer 到 Command 的统一投递。
- Service 生命周期、超时、panic 隔离、任务追踪和基础指标。

Cluster Transport、Discovery、Snapshot、Supervisor、Monitor 和 Business Layer 仍在规划中，实施顺序见 [`RFC-0500`](docs/rfcs/RFC-0500-Roadmap.md)。当前工程欠账见 [`docs/TODO.md`](docs/TODO.md)。

## 本地 Runtime 示例

```bash
go test ./...
go run ./examples/local-runtime
```

预期输出：`hello`。
